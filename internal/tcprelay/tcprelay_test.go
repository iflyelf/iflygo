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

// startRelayServer 启动一个 TCP 中继服务端（随机端口），返回地址与取消函数。
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

// TestBridgeServerRoundTrip 端到端：UDP 客户端 → 本地桥 → TCP 中继 → UDP 上游 → 回程。
func TestBridgeServerRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "")

	bridge, err := StartBridge(ctx, relayAddr, "", echoAddr)
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer bridge.Close()
	roundTrip(t, bridge.LocalAddr(), "hello over tcp relay")
}

// TestBridgeServerToken 验证共享令牌：错误令牌无法建立隧道，正确令牌可用。
func TestBridgeServerToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, closeEcho := startUDPEcho(t)
	defer closeEcho()
	relayAddr := startRelayServer(t, ctx, "s3cret")

	bad, err := StartBridge(ctx, relayAddr, "wrong", echoAddr)
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer bad.Close()
	conn, err := net.DialUDP("udp", nil, bad.LocalAddr())
	if err != nil {
		t.Fatalf("dial bad bridge: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 64)
	if n, err := conn.Read(buf); err == nil {
		t.Fatalf("错误令牌不应收到回显，却收到 %q", buf[:n])
	}

	good, err := StartBridge(ctx, relayAddr, "s3cret", echoAddr)
	if err != nil {
		t.Fatalf("StartBridge: %v", err)
	}
	defer good.Close()
	roundTrip(t, good.LocalAddr(), "authorized")
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

	// 验证改写后的地址确实能经 TCP 中继到达上游
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
