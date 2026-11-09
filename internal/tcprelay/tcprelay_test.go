package tcprelay

import (
	"context"
	"net"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// startUDPEcho 启动一个 UDP 回显服务器，返回地址与关闭函数。
func startUDPEcho(t *testing.T) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("UDP echo listen: %v", err)
	}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	return pc.LocalAddr().String(), func() { _ = pc.Close() }
}

// deadUDP 返回一个绑定了但永不应答的 UDP socket 与地址，用于模拟 UDP 被封锁。
func deadUDP(t *testing.T) (*net.UDPConn, string, func()) {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("dead UDP listen: %v", err)
	}
	return pc, pc.LocalAddr().String(), func() { _ = pc.Close() }
}

// startRelayServer 启动一个 TCP 中继服务端（随机端口），返回地址。
func startRelayServer(t *testing.T, ctx context.Context, token string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("relay listen: %v", err)
	}
	srv := NewServer("")
	srv.Token = token
	go func() { _ = srv.Serve(ctx, ln) }()
	return ln.Addr().String()
}

// roundTrip 模拟 Nebula：向桥的本地 UDP 端口发数据报并读回显。
func roundTrip(t *testing.T, local *net.UDPAddr, msg string) {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, local)
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf[:n]) != msg {
		t.Fatalf("回显不匹配: got %q, want %q", buf[:n], msg)
	}
}

func waitMode(t *testing.T, b *Bridge, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.Mode() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待桥切换到 %s 超时，当前 %s", want, b.Mode())
}

// sendProbe 模拟 Nebula 向桥发一个数据报。
func sendProbe(t *testing.T, conn *net.UDPConn, payload string) {
	t.Helper()
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write probe: %v", err)
	}
}

// TestBridgeDirectPath 验证正常网络下走直连 UDP，不切换到中继。
func TestBridgeDirectPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "")

	b, err := StartBridge(ctx, relayAddr, "", echoAddr)
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer b.Close()

	roundTrip(t, b.LocalAddr(), "hello direct")
	if b.Mode() != "direct" {
		t.Fatalf("正常网络应保持直连，实际 %s", b.Mode())
	}
}

// TestBridgeAutoSwitchToRelay 验证直连无响应时自动切换到 TCP 中继。
func TestBridgeAutoSwitchToRelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, deadAddr, closeDead := deadUDP(t)
	defer closeDead()
	relayAddr := startRelayServer(t, ctx, "")

	b, err := StartBridgeWithOptions(ctx, BridgeOptions{
		RelayServer:     relayAddr,
		Target:          deadAddr,
		ResponseTimeout: 100 * time.Millisecond,
		MinSent:         1,
		DialTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer b.Close()

	conn, err := net.DialUDP("udp", nil, b.LocalAddr())
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	sendProbe(t, conn, "probe-1")
	time.Sleep(150 * time.Millisecond)
	sendProbe(t, conn, "probe-2")

	waitMode(t, b, "relay")
}

// TestBridgeSwitchBackToDirect 验证中继期间直连恢复后自动切回。
func TestBridgeSwitchBackToDirect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dead, deadAddr, closeDead := deadUDP(t)
	defer closeDead()
	relayAddr := startRelayServer(t, ctx, "")

	b, err := StartBridgeWithOptions(ctx, BridgeOptions{
		RelayServer:     relayAddr,
		Target:          deadAddr,
		ResponseTimeout: 100 * time.Millisecond,
		MinSent:         1,
		RetryInterval:   time.Hour, // 关闭主动探测，改为手动注入直连响应
		DialTimeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer b.Close()

	nebula, err := net.DialUDP("udp", nil, b.LocalAddr())
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer nebula.Close()
	sendProbe(t, nebula, "probe-1")
	time.Sleep(150 * time.Millisecond)
	sendProbe(t, nebula, "probe-2")
	waitMode(t, b, "relay")

	// 模拟真实上游恢复：从目标地址向桥的直连 socket 回包。
	directLocal := b.direct.LocalAddr().(*net.UDPAddr)
	if _, err := dead.WriteToUDP([]byte("recovered"), directLocal); err != nil {
		t.Fatalf("inject direct response: %v", err)
	}
	waitMode(t, b, "direct")
}

// TestBridgeForceRelay 验证 tcp 模式：启动即走中继，且不回落直连。
func TestBridgeForceRelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "")

	b, err := StartBridgeWithOptions(ctx, BridgeOptions{
		RelayServer: relayAddr,
		Target:      echoAddr,
		ForceRelay:  true,
		DialTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer b.Close()
	if b.Mode() != "relay" {
		t.Fatalf("强制中继模式应立即为 relay，实际 %s", b.Mode())
	}
	roundTrip(t, b.LocalAddr(), "hello forced tcp")

	// 中继不可用时不回退直连（策略模式）。
	bad, err := StartBridgeWithOptions(ctx, BridgeOptions{
		RelayServer: "127.0.0.1:1", // 不可达
		Target:      echoAddr,
		ForceRelay:  true,
		DialTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer bad.Close()
	conn, err := net.DialUDP("udp", nil, bad.LocalAddr())
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, _ = conn.Write([]byte("must-not-leak"))
	buf := make([]byte, 64)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("tcp 模式不应回退直连，却收到 %q", buf[:n])
	}
}

// TestBridgeRelayPath 验证在强制中继状态下数据确实经 TCP 中继转发。
func TestBridgeRelayPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "")

	b, err := StartBridge(ctx, relayAddr, "", echoAddr)
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer b.Close()

	b.mu.Lock()
	b.state = bridgeRelay
	b.lastProbe = time.Now() // 避免首个报文同时触发直连探测
	b.mu.Unlock()

	roundTrip(t, b.LocalAddr(), "hello relay")
}

// TestServerTokenAuth 验证中继服务端的共享令牌校验。
func TestServerTokenAuth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "s3cret")

	// 错误令牌：握手后服务端立即断开，读不到回显。
	bad, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	if err := writeHandshake(bad, echoAddr, "wrong"); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := writeFrame(bad, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = bad.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := readFrame(bad); err == nil {
		t.Fatalf("错误令牌不应收到回显")
	}
	_ = bad.Close()

	// 正确令牌：正常转发。
	good, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer good.Close()
	if err := writeHandshake(good, echoAddr, "s3cret"); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := writeFrame(good, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = good.SetReadDeadline(time.Now().Add(2 * time.Second))
	payload, err := readFrame(good)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(payload) != "ping" {
		t.Fatalf("回显不匹配: %q", payload)
	}
}

// TestRewriteConfig 验证 static_host_map 与 relay.relays 均被改写为本地桥端口。
func TestRewriteConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "")

	cfg := []byte(`
static_host_map:
  "10.77.0.1":
    - "` + echoAddr + `"
relay:
  am_relay: false
  use_relays: true
  relays:
    - "` + echoAddr + `"
`)
	out, err := RewriteConfig(ctx, relayAddr, "", cfg)
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}

	var root struct {
		StaticHostMap map[string][]string `yaml:"static_host_map"`
		Relay         struct {
			UseRelays bool     `yaml:"use_relays"`
			Relays    []string `yaml:"relays"`
		} `yaml:"relay"`
	}
	if err := yaml.Unmarshal(out, &root); err != nil {
		t.Fatalf("unmarshal rewritten: %v", err)
	}
	eps := root.StaticHostMap["10.77.0.1"]
	if len(eps) != 1 {
		t.Fatalf("期望 1 个 endpoint，got %v", eps)
	}
	if eps[0] == echoAddr {
		t.Fatalf("static_host_map endpoint 未被改写: %s", eps[0])
	}
	if !isLoopback(eps[0]) {
		t.Fatalf("改写后应为本地地址，got %s", eps[0])
	}
	if len(root.Relay.Relays) != 1 || !isLoopback(root.Relay.Relays[0]) {
		t.Fatalf("relay.relays 未被改写: %v", root.Relay.Relays)
	}
	if !root.Relay.UseRelays {
		t.Fatalf("relay.use_relays 应保留")
	}

	// 验证改写后的地址确实能经桥到达上游（正常网络走直连 UDP）
	conn, err := net.Dial("udp", eps[0])
	if err != nil {
		t.Fatalf("dial rewritten endpoint: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf[:n]) != "ping" {
		t.Fatalf("回显不匹配: %q", buf[:n])
	}
}

// TestRewriteConfigKeepsPlainOverlayIP 验证非 host:port 的条目（纯 overlay IP）原样保留。
func TestRewriteConfigKeepsPlainOverlayIP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := []byte(`
relay:
  relays:
    - "10.77.0.2"
`)
	out, err := RewriteConfig(ctx, "127.0.0.1:1", "", cfg)
	if err != nil {
		t.Fatalf("RewriteConfig: %v", err)
	}
	var root struct {
		Relay struct {
			Relays []string `yaml:"relays"`
		} `yaml:"relay"`
	}
	if err := yaml.Unmarshal(out, &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(root.Relay.Relays) != 1 || root.Relay.Relays[0] != "10.77.0.2" {
		t.Fatalf("纯 overlay IP 应保留，got %v", root.Relay.Relays)
	}
}

func isLoopback(endpoint string) bool {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return false
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
