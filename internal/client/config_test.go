package client

import (
	"os"

	"github.com/iflyelf/iflygo/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadFileConfigAndInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.yaml")
	body := `interval: 5s
data_plane:
  mode: tcp
  tcp_relay: relay.example.com:8443
  tcp_relay_token: tok
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFileConfig(path)
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if got := cfg.IntervalDuration(time.Second); got != 5*time.Second {
		t.Fatalf("interval: got %s", got)
	}
	got := ResolveRelay("", "", "", cfg, nil)
	want := RelaySettings{Mode: "tcp", Addr: "relay.example.com:8443", Token: "tok"}
	if got != want {
		t.Fatalf("ResolveRelay: got %+v want %+v", got, want)
	}
}

func TestLoadFileConfigMissing(t *testing.T) {
	cfg, err := LoadFileConfig(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatalf("缺失文件应返回空配置: %v", err)
	}
	if cfg.IntervalDuration(30*time.Second) != 30*time.Second {
		t.Fatalf("空配置应使用默认间隔")
	}
	if got := ResolveRelay("", "", "", cfg, nil); got.Mode != "auto" {
		t.Fatalf("默认模式应为 auto，got %s", got.Mode)
	}
}

func TestResolveRelayPriority(t *testing.T) {
	file := &FileConfig{DataPlane: DataPlaneConfig{
		Mode: "udp", TCPRelay: "file.example.com:8443", TCPRelayToken: "filetok",
	}}
	hb := &HeartbeatResult{
		TCPRelayMode: "tcp", TCPRelay: "hb.example.com:9443", TCPRelayToken: "hbtok",
	}

	// 命令行模式优先于文件与服务端；地址未指定则沿用文件。
	got := ResolveRelay("auto", "", "", file, hb)
	if got.Mode != "auto" || got.Addr != "file.example.com:8443" || got.Token != "filetok" {
		t.Fatalf("unexpected: %+v", got)
	}

	// 文件未配置时使用服务端下发。
	empty := &FileConfig{}
	got = ResolveRelay("", "", "", empty, hb)
	if got.Mode != "tcp" || got.Addr != "hb.example.com:9443" || got.Token != "hbtok" {
		t.Fatalf("unexpected: %+v", got)
	}

	// off 等价 udp，且清空地址。
	got = ResolveRelay("", "off", "", empty, hb)
	if got.Mode != "udp" || got.Addr != "" {
		t.Fatalf("off 应关闭中继: %+v", got)
	}

	// udp 模式清空地址。
	got = ResolveRelay("udp", "explicit.example.com:1", "", empty, hb)
	if got.Mode != "udp" || got.Addr != "" {
		t.Fatalf("udp 模式不应保留地址: %+v", got)
	}

	// 命令行地址/令牌优先级最高。
	got = ResolveRelay("tcp", "cli.example.com:7443", "clitok", file, hb)
	if got.Mode != "tcp" || got.Addr != "cli.example.com:7443" || got.Token != "clitok" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestApplyBundleWritesClientYAML(t *testing.T) {
	dir := t.TempDir()
	b := &model.Bundle{
		Network: "home", IPv4: "10.77.0.2", Cidr4: "10.77.0.0/16",
		Config: "pki:\n  ca: ca.crt\n", ConfigRev: 3,
		Client: model.ClientSettings{
			DataPlaneMode: "tcp", TCPRelay: "relay.example.com:8443",
			TCPRelayToken: "tok", Interval: "45s",
		},
	}
	if err := ApplyBundle(dir, b); err != nil {
		t.Fatalf("ApplyBundle: %v", err)
	}
	fc, err := LoadFileConfig(filepath.Join(dir, "client.yaml"))
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	if fc.Interval != "45s" || fc.DataPlane.Mode != "tcp" ||
		fc.DataPlane.TCPRelay != "relay.example.com:8443" || fc.DataPlane.TCPRelayToken != "tok" {
		t.Fatalf("client.yaml 未按服务端下发写入: %+v", fc)
	}
}
