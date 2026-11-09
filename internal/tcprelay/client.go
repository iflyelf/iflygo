package tcprelay

import (
	"context"
	"log"
	"net"
	"sync"
	"time"
)

// Bridge 客户端侧 UDP⇄TCP 桥：本地监听一个 UDP 端口（Nebula 发到这里），
// 把数据报通过 TCP 隧道发往中继服务端，由服务端以 UDP 转发到真实上游。
//
// 一对一：一个 Bridge 对应一个真实上游 endpoint（lighthouse/relay 的 UDP 地址）。
type Bridge struct {
	relayServer string
	token       string
	target      string
	udp         *net.UDPConn

	mu         sync.Mutex
	tcp        net.Conn
	lastSource *net.UDPAddr
	closed     bool
	logger     *log.Logger
}

// StartBridge 在本地 127.0.0.1 的随机 UDP 端口启动桥接，返回桥接对象。
// ctx 取消时桥接自动关闭。
func StartBridge(ctx context.Context, relayServer, token, target string) (*Bridge, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	b := &Bridge{relayServer: relayServer, token: token, target: target, udp: udp, logger: log.Default()}
	go b.udpLoop()
	go func() {
		<-ctx.Done()
		b.Close()
	}()
	return b, nil
}

// LocalAddr 返回本地 UDP 监听地址（供改写 static_host_map 使用）。
func (b *Bridge) LocalAddr() *net.UDPAddr { return b.udp.LocalAddr().(*net.UDPAddr) }

// Close 关闭桥接与底层连接。
func (b *Bridge) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	conn := b.tcp
	b.tcp = nil
	b.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	_ = b.udp.Close()
}

// udpLoop 读取本地 UDP 数据报，经 TCP 隧道发送。
func (b *Bridge) udpLoop() {
	buf := make([]byte, maxFrameSize)
	for {
		n, src, err := b.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		b.mu.Lock()
		b.lastSource = src
		b.mu.Unlock()

		conn, err := b.ensureTCP()
		if err != nil {
			// 丢弃该包，下一个包会尝试重连（UDP 本身不可靠，Nebula 会重传）
			continue
		}
		if err := writeFrame(conn, buf[:n]); err != nil {
			b.dropTCP(conn)
		}
	}
}

// ensureTCP 惰性建立（或复用）TCP 隧道；首次需发送握手（目标 UDP 地址）。
func (b *Bridge) ensureTCP() (net.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, net.ErrClosed
	}
	if b.tcp != nil {
		return b.tcp, nil
	}
	conn, err := net.DialTimeout("tcp", b.relayServer, 10*time.Second)
	if err != nil {
		return nil, err
	}
	if err := writeHandshake(conn, b.target, b.token); err != nil {
		_ = conn.Close()
		return nil, err
	}
	b.tcp = conn
	go b.recvLoop(conn)
	return conn, nil
}

// recvLoop 读取 TCP 帧，写回本地 UDP（发给最近的 Nebula 源地址）。
func (b *Bridge) recvLoop(conn net.Conn) {
	for {
		payload, err := readFrame(conn)
		if err != nil {
			b.dropTCP(conn)
			return
		}
		b.mu.Lock()
		src := b.lastSource
		closed := b.closed
		b.mu.Unlock()
		if closed || src == nil {
			continue
		}
		_, _ = b.udp.WriteToUDP(payload, src)
	}
}

// dropTCP 丢弃指定 TCP 隧道（下次发 UDP 时重连）。
func (b *Bridge) dropTCP(conn net.Conn) {
	b.mu.Lock()
	if b.tcp == conn {
		b.tcp = nil
	}
	b.mu.Unlock()
	_ = conn.Close()
}
