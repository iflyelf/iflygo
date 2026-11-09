package tcprelay

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// bridgeState 桥的当前转发方式。
type bridgeState int

const (
	// bridgeDirect 直接以 UDP 转发到真实上游（与原生 UDP 等价）。
	bridgeDirect bridgeState = iota
	// bridgeRelay 经 TCP 中继转发（UDP 被封锁时的兜底）。
	bridgeRelay
)

var (
	errNoRelay      = errors.New("未配置 TCP 中继")
	errRelayBackoff = errors.New("TCP 中继拨号退避中")
)

// Bridge 客户端侧 UDP 桥：本地监听一个 UDP 端口（Nebula 把数据报发到这里），
// 由桥转发到真实上游。
//
// 智能切换（无需人工指定协议）：
//   - 默认走直连 UDP，与原生行为完全一致；
//   - 若在判定窗口内直连始终无响应，自动切换到 TCP 中继；
//   - 中继期间周期性重试直连，一旦收到直连响应立即切回。
//
// 一对一：一个 Bridge 对应一个真实上游 endpoint（lighthouse/relay 的 UDP 地址）。
type Bridge struct {
	relayServer string
	token       string
	target      string
	targetAddr  *net.UDPAddr

	// udp 是 Nebula 侧监听 socket（绑定 127.0.0.1）；direct 是通往真实上游的
	// 已连接 UDP socket。两者分离，才能在不改变 Nebula 视角的前提下切换路径。
	udp    *net.UDPConn
	direct *net.UDPConn

	responseTimeout time.Duration
	minSent         int
	retryInterval   time.Duration
	dialTimeout     time.Duration
	dialBackoff     time.Duration
	forceRelay      bool
	logger          *log.Logger

	mu        sync.Mutex
	tcp       net.Conn
	nextDial  time.Time
	nebulaSrc *net.UDPAddr
	state     bridgeState
	sent      int
	lastResp  time.Time
	lastProbe time.Time
	closed    bool
}

// BridgeOptions 桥接参数；零值字段使用默认值。
type BridgeOptions struct {
	RelayServer string
	Token       string
	Target      string
	// ResponseTimeout 直连无响应多久判定为不可用，默认 5s。
	ResponseTimeout time.Duration
	// MinSent 判定前至少发出的报文数，默认 3。
	MinSent int
	// RetryInterval 中继模式下重试直连的间隔，默认 30s。
	RetryInterval time.Duration
	// DialTimeout TCP 中继拨号超时，默认 2s。
	DialTimeout time.Duration
	// DialBackoff 拨号失败后的退避，默认 5s。
	DialBackoff time.Duration
	// ForceRelay 强制走 TCP 中继（tcp 模式）：启动即使用中继，
	// 且中继不可用时不回退直连（保证流量策略）。
	ForceRelay bool
	Logger     *log.Logger
}

// StartBridge 在本地 127.0.0.1 的随机 UDP 端口启动桥接，返回桥接对象。
// ctx 取消时桥接自动关闭。relayServer 为空时始终直连（不做切换）。
func StartBridge(ctx context.Context, relayServer, token, target string) (*Bridge, error) {
	return StartBridgeWithOptions(ctx, BridgeOptions{
		RelayServer: relayServer,
		Token:       token,
		Target:      target,
	})
}

// StartBridgeWithOptions 以显式参数启动桥接（便于测试与调优）。
func StartBridgeWithOptions(ctx context.Context, opts BridgeOptions) (*Bridge, error) {
	addr, err := net.ResolveUDPAddr("udp", opts.Target)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	direct, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		_ = udp.Close()
		return nil, err
	}
	b := &Bridge{
		relayServer:     opts.RelayServer,
		token:           opts.Token,
		target:          opts.Target,
		targetAddr:      addr,
		udp:             udp,
		direct:          direct,
		responseTimeout: opts.ResponseTimeout,
		minSent:         opts.MinSent,
		retryInterval:   opts.RetryInterval,
		dialTimeout:     opts.DialTimeout,
		dialBackoff:     opts.DialBackoff,
		logger:          opts.Logger,
		lastResp:        time.Now(),
		forceRelay:      opts.ForceRelay,
	}
	if b.logger == nil {
		b.logger = log.Default()
	}
	if b.forceRelay && b.relayServer != "" {
		b.state = bridgeRelay
	}
	go b.nebulaLoop()
	go b.directLoop()
	go func() {
		<-ctx.Done()
		b.Close()
	}()
	return b, nil
}

// LocalAddr 返回本地 UDP 监听地址（供改写 static_host_map 使用）。
func (b *Bridge) LocalAddr() *net.UDPAddr { return b.udp.LocalAddr().(*net.UDPAddr) }

// Mode 返回当前转发方式（"direct" / "relay"），用于日志与观测。
func (b *Bridge) Mode() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == bridgeRelay {
		return "relay"
	}
	return "direct"
}

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
	_ = b.direct.Close()
	_ = b.udp.Close()
}

// nebulaLoop 只接收 Nebula 发到本地监听端口的数据报，交给转发逻辑。
func (b *Bridge) nebulaLoop() {
	buf := make([]byte, maxFrameSize)
	for {
		n, src, err := b.udp.ReadFromUDP(buf)
		if err != nil {
			return
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])

		b.mu.Lock()
		b.nebulaSrc = src
		b.mu.Unlock()
		b.forward(payload)
	}
}

// directLoop 接收真实上游经直连 UDP 返回的响应。
func (b *Bridge) directLoop() {
	buf := make([]byte, maxFrameSize)
	for {
		n, err := b.direct.Read(buf)
		if err != nil {
			if b.isClosed() || errors.Is(err, net.ErrClosed) {
				return
			}
			// 目标不可达时可能收到 ICMP 错误（connection refused），
			// 短暂退避后继续读取，等待目标恢复或切换中继。
			time.Sleep(100 * time.Millisecond)
			continue
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])
		b.onDirectResponse(payload)
	}
}

// forward 按当前状态转发 Nebula 发来的数据报。
func (b *Bridge) forward(payload []byte) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	state := b.state
	if state == bridgeDirect {
		b.sent++
	}
	// 直连判定：有中继可用、已发够报文、且超过判定窗口仍无响应。
	switched := false
	if state == bridgeDirect && !b.forceRelay && b.relayServer != "" &&
		b.sent >= b.minSentValue() && time.Since(b.lastResp) > b.responseTimeoutValue() {
		b.state = bridgeRelay
		b.lastProbe = time.Now()
		state = bridgeRelay
		switched = true
	}
	// 中继模式下周期性探测直连是否恢复。
	probe := false
	if state == bridgeRelay && b.relayServer != "" && !b.forceRelay && time.Since(b.lastProbe) >= b.retryIntervalValue() {
		probe = true
		b.lastProbe = time.Now()
	}
	b.mu.Unlock()

	if switched {
		b.logf("🧵 UDP 直连 %s 无响应，自动切换到 TCP 中继 %s", b.target, b.relayServer)
	}
	if state == bridgeDirect {
		_, _ = b.direct.Write(payload)
		return
	}

	conn, err := b.ensureTCP()
	if err != nil {
		if !b.forceRelay {
			// 中继暂不可用（未配置/拨号失败）：尽力直连，避免完全断流。
			_, _ = b.direct.Write(payload)
		}
	} else if err := writeFrame(conn, payload); err != nil {
		b.dropTCP(conn)
		if !b.forceRelay {
			_, _ = b.direct.Write(payload)
		}
	}
	if probe {
		_, _ = b.direct.Write(payload)
	}
}

// onDirectResponse 处理直连上游的响应：刷新健康状态，必要时切回直连。
func (b *Bridge) onDirectResponse(payload []byte) {
	b.mu.Lock()
	b.lastResp = time.Now()
	switched := b.state == bridgeRelay && !b.forceRelay
	conn := net.Conn(nil)
	if switched {
		b.state = bridgeDirect
		b.sent = 0
		conn = b.tcp
		b.tcp = nil
	}
	src := b.nebulaSrc
	b.mu.Unlock()

	if switched {
		if conn != nil {
			_ = conn.Close()
		}
		b.logf("✅ UDP 直连 %s 已恢复，自动切回直连", b.target)
	}
	if src != nil {
		_, _ = b.udp.WriteToUDP(payload, src)
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
	if b.relayServer == "" {
		return nil, errNoRelay
	}
	if now := time.Now(); now.Before(b.nextDial) {
		return nil, errRelayBackoff
	}
	conn, err := net.DialTimeout("tcp", b.relayServer, b.dialTimeoutValue())
	if err != nil {
		b.nextDial = time.Now().Add(b.dialBackoffValue())
		return nil, err
	}
	if err := writeHandshake(conn, b.target, b.token); err != nil {
		_ = conn.Close()
		b.nextDial = time.Now().Add(b.dialBackoffValue())
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
		src := b.nebulaSrc
		closed := b.closed
		b.mu.Unlock()
		if closed || src == nil {
			continue
		}
		_, _ = b.udp.WriteToUDP(payload, src)
	}
}

// dropTCP 丢弃指定 TCP 隧道（后续按需重连）。
func (b *Bridge) dropTCP(conn net.Conn) {
	b.mu.Lock()
	if b.tcp == conn {
		b.tcp = nil
	}
	b.mu.Unlock()
	_ = conn.Close()
}

func (b *Bridge) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *Bridge) logf(format string, args ...any) {
	if b.logger != nil {
		b.logger.Printf(format, args...)
	}
}

func (b *Bridge) responseTimeoutValue() time.Duration {
	if b.responseTimeout > 0 {
		return b.responseTimeout
	}
	return 5 * time.Second
}

func (b *Bridge) minSentValue() int {
	if b.minSent > 0 {
		return b.minSent
	}
	return 3
}

func (b *Bridge) retryIntervalValue() time.Duration {
	if b.retryInterval > 0 {
		return b.retryInterval
	}
	return 30 * time.Second
}

func (b *Bridge) dialTimeoutValue() time.Duration {
	if b.dialTimeout > 0 {
		return b.dialTimeout
	}
	return 2 * time.Second
}

func (b *Bridge) dialBackoffValue() time.Duration {
	if b.dialBackoff > 0 {
		return b.dialBackoff
	}
	return 5 * time.Second
}
