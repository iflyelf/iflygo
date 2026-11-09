package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &State{
		Server: "https://console.example.com", ClientID: 42, Secret: "secret",
		Name: "node-1", Network: "home", IPv4: "10.88.0.2", IPv6: "fd88::2", ConfigRev: 3,
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClientID != s.ClientID || got.IPv4 != s.IPv4 || got.ConfigRev != s.ConfigRev {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	// 状态文件应为 0600
	info, _ := os.Stat(filepath.Join(dir, "state.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state.json 权限应为 0600，实际 %o", info.Mode().Perm())
	}
}

func TestSystemdUnit(t *testing.T) {
	u := SystemdUnit("/usr/local/bin/iflygo-client", "/var/lib/iflygo")
	for _, want := range []string{"[Service]", "ExecStart=/usr/local/bin/iflygo-client run --state-dir /var/lib/iflygo", "Restart=always"} {
		if !strings.Contains(u, want) {
			t.Fatalf("unit 缺少 %q", want)
		}
	}
}

func TestSystemdUnitTCPRelay(t *testing.T) {
	u := SystemdUnitWithRelay("/usr/local/bin/iflygo-client", "/var/lib/iflygo",
		"relay.example.com:8443", "s3cret")
	want := "ExecStart=/usr/local/bin/iflygo-client run --state-dir /var/lib/iflygo --tcp-relay relay.example.com:8443 --tcp-relay-token s3cret"
	if !strings.Contains(u, want) {
		t.Fatalf("unit 缺少 %q:\n%s", want, u)
	}
}
