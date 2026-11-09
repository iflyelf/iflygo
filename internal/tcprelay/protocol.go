// Package tcprelay 在 UDP 被完全封锁（仅允许 TCP）的网络中，为 Nebula overlay
// 提供「UDP over TCP」中继：客户端把发往灯塔/中继的 UDP 数据报封装进 TCP 连接，
// 服务端解封装后以 UDP 转发到目标 endpoint。
//
// 设计要点：
//   - 不改动 Nebula（其为纯 UDP），而是在客户端侧把 static_host_map / relay.relays
//     中的真实 endpoint 改写为本地桥接的 UDP 端口；本地桥接通过 TCP 连到 tcprelay
//     服务端，服务端再以 UDP 发往真实 endpoint。
//   - 帧协议：握手 [1 字节版本][2 字节目标长度][目标 "host:port"][2 字节令牌长度][令牌]；
//     数据帧 [4 字节大端长度][载荷]，双向复用同一 TCP 连接。
//   - 每个（本地 UDP 监听 ⇄ TCP 隧道 ⇄ 真实上游）为一对一的独立隧道，
//     多个上游（lighthouse/relay）各自占用一个本地端口，互不干扰。
package tcprelay

import (
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// 协议常量。
const (
	protocolVersion = 2
	maxFrameSize    = 1 << 16 // 64KiB，UDP 数据报上限（实际远小于此）
	maxTargetLen    = 1024
	maxTokenLen     = 512
)

// handshake 是客户端建立隧道时发送的握手：版本 + 目标 UDP 地址 + 可选共享令牌。
type handshake struct {
	Version int
	Target  string // "host:port"
	Token   string
}

func writeHandshake(w io.Writer, target, token string) error {
	if len(target) == 0 || len(target) > maxTargetLen {
		return fmt.Errorf("目标地址长度非法: %d", len(target))
	}
	if len(token) > maxTokenLen {
		return fmt.Errorf("令牌过长: %d", len(token))
	}
	buf := make([]byte, 0, 5+len(target)+len(token))
	buf = append(buf, protocolVersion, byte(len(target)>>8), byte(len(target)))
	buf = append(buf, target...)
	buf = append(buf, byte(len(token)>>8), byte(len(token)))
	buf = append(buf, token...)
	_, err := w.Write(buf)
	return err
}

func readHandshake(r io.Reader) (handshake, error) {
	head := make([]byte, 3)
	if _, err := io.ReadFull(r, head); err != nil {
		return handshake{}, err
	}
	ver := int(head[0])
	n := int(head[1])<<8 | int(head[2])
	if n == 0 || n > maxTargetLen {
		return handshake{}, fmt.Errorf("握手目标长度非法: %d", n)
	}
	target := make([]byte, n)
	if _, err := io.ReadFull(r, target); err != nil {
		return handshake{}, err
	}
	var tl [2]byte
	if _, err := io.ReadFull(r, tl[:]); err != nil {
		return handshake{}, err
	}
	tn := int(tl[0])<<8 | int(tl[1])
	if tn > maxTokenLen {
		return handshake{}, fmt.Errorf("握手令牌长度非法: %d", tn)
	}
	var token []byte
	if tn > 0 {
		token = make([]byte, tn)
		if _, err := io.ReadFull(r, token); err != nil {
			return handshake{}, err
		}
	}
	return handshake{Version: ver, Target: string(target), Token: string(token)}, nil
}

// tokenMatches 以常量时间比较令牌，避免时序侧信道。
func tokenMatches(want, got string) bool {
	if want == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// writeFrame 写一帧（4 字节大端长度 + 载荷）。
//
// 使用 net.Buffers：对 TCP 连接会走 writev，一次系统调用写出头部+载荷，
// 减少高吞吐中继场景的 syscall 次数。
func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFrameSize {
		return fmt.Errorf("帧过大: %d", len(payload))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if len(payload) == 0 {
		_, err := w.Write(hdr[:])
		return err
	}
	bufs := net.Buffers{hdr[:], payload}
	_, err := bufs.WriteTo(w)
	return err
}

// readFrameInto 读一帧到调用方提供的缓冲区，返回的切片仅在下次调用前有效。
//
// 热路径（服务端转发、客户端回传）复用同一块缓冲区，避免每包一次堆分配。
func readFrameInto(r io.Reader, buf []byte) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return buf[:0], nil
	}
	if n > maxFrameSize {
		return nil, fmt.Errorf("帧长度非法: %d", n)
	}
	if int(n) > len(buf) {
		return nil, fmt.Errorf("帧超出缓冲区: %d > %d", n, len(buf))
	}
	if _, err := io.ReadFull(r, buf[:n]); err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// readFrame 读一帧（新分配缓冲区，便于测试与低频调用）。
func readFrame(r io.Reader) ([]byte, error) {
	return readFrameInto(r, make([]byte, maxFrameSize))
}
