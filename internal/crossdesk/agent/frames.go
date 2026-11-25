package agent

import (
	"encoding/binary"
	"fmt"
)

// 画面帧分片协议（二进制 DataChannel 消息）。
//
// 每条消息固定 16 字节头：
//
//	0..3   magic  "CDS1"
//	4..7   frame sequence（大端）
//	8..9   分片序号
//	10..11 分片总数
//	12..13 画面宽度（像素）
//	14..15 画面高度（像素）
//
// 之后紧跟 JPEG 分片。Web 客户端按 seq 重组后绘制到 canvas。
const (
	frameHeaderSize   = 16
	frameChunkPayload = 48 << 10
)

var frameMagic = [4]byte{'C', 'D', 'S', '1'}

// chunkFrame 把一帧 JPEG 拆成不超过 48 KiB 的 DataChannel 消息。
func chunkFrame(seq uint32, width, height int, jpeg []byte) [][]byte {
	if width <= 0 || height <= 0 || width > 0xffff || height > 0xffff {
		return nil
	}
	total := (len(jpeg) + frameChunkPayload - 1) / frameChunkPayload
	if total == 0 {
		total = 1
	}
	if total > 0xffff {
		return nil
	}
	out := make([][]byte, total)
	for i := 0; i < total; i++ {
		start := i * frameChunkPayload
		end := start + frameChunkPayload
		if end > len(jpeg) {
			end = len(jpeg)
		}
		buf := make([]byte, frameHeaderSize+end-start)
		copy(buf[0:4], frameMagic[:])
		binary.BigEndian.PutUint32(buf[4:8], seq)
		binary.BigEndian.PutUint16(buf[8:10], uint16(i))
		binary.BigEndian.PutUint16(buf[10:12], uint16(total))
		binary.BigEndian.PutUint16(buf[12:14], uint16(width))
		binary.BigEndian.PutUint16(buf[14:16], uint16(height))
		copy(buf[frameHeaderSize:], jpeg[start:end])
		out[i] = buf
	}
	return out
}

// frameHeader 解析并校验帧分片头。
func frameHeader(msg []byte) (seq uint32, index, total int, width, height int, ok bool) {
	if len(msg) < frameHeaderSize || msg[0] != frameMagic[0] || msg[1] != frameMagic[1] ||
		msg[2] != frameMagic[2] || msg[3] != frameMagic[3] {
		return 0, 0, 0, 0, 0, false
	}
	seq = binary.BigEndian.Uint32(msg[4:8])
	index = int(binary.BigEndian.Uint16(msg[8:10]))
	total = int(binary.BigEndian.Uint16(msg[10:12]))
	width = int(binary.BigEndian.Uint16(msg[12:14]))
	height = int(binary.BigEndian.Uint16(msg[14:16]))
	if total <= 0 || index < 0 || index >= total || total > 1<<12 {
		return 0, 0, 0, 0, 0, false
	}
	return seq, index, total, width, height, true
}

// reassembleFrame 用于测试与诊断：把分片重新拼成完整 JPEG。
func reassembleFrame(parts [][]byte) ([]byte, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty frame")
	}
	first := parts[0]
	seq, _, total, width, height, ok := frameHeader(first)
	if !ok {
		return nil, fmt.Errorf("invalid frame header")
	}
	if total != len(parts) {
		return nil, fmt.Errorf("frame %d: got %d/%d chunks", seq, len(parts), total)
	}
	out := make([]byte, 0, total*frameChunkPayload)
	for i, part := range parts {
		s, idx, _, w, h, ok := frameHeader(part)
		if !ok || s != seq || idx != i || w != width || h != height {
			return nil, fmt.Errorf("frame %d: chunk %d mismatch", seq, i)
		}
		out = append(out, part[frameHeaderSize:]...)
	}
	return out, nil
}
