package tcprelay

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// maxConcurrentTunnels 单实例最大并发中继隧道数，防止连接洪泛耗尽资源。
const maxConcurrentTunnels = 4096

// Server TCP 中继服务端：接受客户端 TCP 隧道，解封装后以 UDP 转发到目标 endpoint。
//
// 每条客户端 TCP 隧道绑定一个独立的 UDP 连接（connected socket）到目标，
// 因此目标返回的响应天然回到该隧道，再封装回 TCP 发给客户端。
type Server struct {
	// Listen TCP 监听地址（如 ":8443" 或 "0.0.0.0:443"）。
	Listen string
	// Token 可选共享密钥；非空时客户端握手必须携带相同令牌。
	Token string
	// HandshakeTimeout 握手阶段超时。默认 10 秒。
	HandshakeTimeout time.Duration
	// Logger 可选日志器；nil 时使用标准库 log。
	Logger *log.Logger

	sem chan struct{}
}

// NewServer 创建 TCP 中继服务端。
func NewServer(listen string) *Server {
	return &Server{
		Listen:           listen,
		HandshakeTimeout: 10 * time.Second,
		Logger:           log.Default(),
		sem:              make(chan struct{}, maxConcurrentTunnels),
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.Logger != nil {
		s.Logger.Printf(format, args...)
	}
}

// Start 启动监听并阻塞服务，直到 ctx 取消或监听失败。
func (s *Server) Start(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve 在给定 listener 上服务（便于测试注入）。
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	s.logf("✅ TCP 中继服务已启动，监听 %s", ln.Addr())
	if s.Token == "" {
		s.logf("⚠️ TCP 中继未配置共享令牌（TCP_RELAY_TOKEN），任何可访问该端口者均可使用，请用防火墙限制来源")
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return err
		}
		select {
		case s.sem <- struct{}{}:
			go func(c net.Conn) {
				defer func() { <-s.sem }()
				s.serveConn(c)
			}(conn)
		default:
			s.logf("⚠️ TCP 中继并发隧道已达上限 %d，拒绝 %s", maxConcurrentTunnels, conn.RemoteAddr())
			_ = conn.Close()
		}
	}
}

// serveConn 处理一条 TCP 隧道：握手 → UDP ⇄ TCP 双向转发。
func (s *Server) serveConn(tcp net.Conn) {
	if tc, ok := tcp.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}
	t, err := s.dialTarget(tcp)
	if err != nil {
		s.logf("⚠️ TCP 中继握手失败: %v", err)
		_ = tcp.Close()
		return
	}
	defer t.Close()
	s.logf("🔗 TCP 中继隧道: %s → %s", tcp.RemoteAddr(), t.target)
	pipe(t)
}

// tunnel 一条已建立握手与 UDP 连接的隧道。
type tunnel struct {
	tcp    net.Conn
	udp    *net.UDPConn
	target string
}

func (t *tunnel) Close() error {
	_ = t.tcp.Close()
	return t.udp.Close()
}

func (s *Server) dialTarget(tcp net.Conn) (*tunnel, error) {
	hsTimeout := s.HandshakeTimeout
	if hsTimeout <= 0 {
		hsTimeout = 10 * time.Second
	}
	_ = tcp.SetReadDeadline(time.Now().Add(hsTimeout))
	hs, err := readHandshake(tcp)
	if err != nil {
		return nil, err
	}
	if hs.Version != protocolVersion {
		return nil, errors.New("协议版本不支持")
	}
	if !tokenMatches(s.Token, hs.Token) {
		return nil, errors.New("令牌校验失败")
	}
	udpAddr, err := net.ResolveUDPAddr("udp", hs.Target)
	if err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}
	_ = tcp.SetReadDeadline(time.Time{}) // 清除握手超时
	return &tunnel{tcp: tcp, udp: udp, target: hs.Target}, nil
}

// pipe 双向转发；任一侧出错即关闭整条隧道。
func pipe(t *tunnel) {
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	// TCP → UDP
	go func() {
		defer stop()
		buf := make([]byte, maxFrameSize)
		for {
			payload, err := readFrameInto(t.tcp, buf)
			if err != nil {
				return
			}
			if _, err := t.udp.Write(payload); err != nil {
				return
			}
		}
	}()
	// UDP → TCP
	go func() {
		defer stop()
		buf := make([]byte, maxFrameSize)
		for {
			n, err := t.udp.Read(buf)
			if err != nil {
				return
			}
			if err := writeFrame(t.tcp, buf[:n]); err != nil {
				return
			}
		}
	}()

	<-done
}
