package overlay_test

import (
	"fmt"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/configgen"
	"github.com/iflyelf/iflygo/internal/overlay"
	"github.com/iflyelf/iflygo/internal/pki"
)

const lighthousePort = 51700

// TestP2PTunnel 验证：lighthouse + 2 个 client 在无 TUN（headless）下能建立 P2P 直连隧道。
func TestP2PTunnel(t *testing.T) {
	ca, err := pki.CreateCA("p2p-test-ca", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	makeNode := func(name, ip string) (cert, key string) {
		nk, err := pki.GenerateNodeKey()
		if err != nil {
			t.Fatal(err)
		}
		certPEM, err := pki.IssueNodeCert(ca, nk, name, []netip.Prefix{netip.MustParsePrefix(ip + "/24")}, nil, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return certPEM, nk.PrivateKeyPEM
	}

	lhCert, lhKey := makeNode("lh", "10.99.0.1")
	aCert, aKey := makeNode("a", "10.99.0.2")
	bCert, bKey := makeNode("b", "10.99.0.3")

	buildLighthouse := func() []byte {
		y, _ := configgen.Build(configgen.Input{
			CAPath: "ca.crt", CertPath: "node.crt", KeyPath: "node.key",
			IsLighthouse: true, ListenHost: "127.0.0.1", ListenPort: lighthousePort, TunDisabled: true,
		})
		return y
	}
	buildClient := func() []byte {
		y, _ := configgen.Build(configgen.Input{
			CAPath: "ca.crt", CertPath: "node.crt", KeyPath: "node.key",
			Lighthouses:   []string{"10.99.0.1"},
			StaticHostMap: map[string][]string{"10.99.0.1": {fmt.Sprintf("127.0.0.1:%d", lighthousePort)}},
			ListenHost:    "127.0.0.1", ListenPort: 0, TunDisabled: true,
		})
		return y
	}

	start := func(t *testing.T, ca, cert, key string, cfg []byte) *overlay.Overlay {
		t.Helper()
		dir := t.TempDir()
		if err := overlay.WritePKI(dir, ca, cert, key); err != nil {
			t.Fatal(err)
		}
		caP, certP, keyP := overlay.PKIPaths(dir)
		full, err := overlay.RewritePKIPaths(cfg, caP, certP, keyP)
		if err != nil {
			t.Fatal(err)
		}
		ov := overlay.New(slog.Default())
		if err := ov.Start(full); err != nil {
			t.Fatalf("启动 overlay 失败: %v", err)
		}
		return ov
	}

	lh := start(t, ca.CertPEM, lhCert, lhKey, buildLighthouse())
	defer lh.Stop()
	a := start(t, ca.CertPEM, aCert, aKey, buildClient())
	defer a.Stop()
	b := start(t, ca.CertPEM, bCert, bKey, buildClient())
	defer b.Stop()

	// 触发 A -> B 握手
	a.EnsureTunnel("10.99.0.3")

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if a.Reachable("10.99.0.3") && b.Reachable("10.99.0.2") {
			return // 成功
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("10s 内未建立 P2P 隧道；A peers=%+v, B peers=%+v", a.Peers(), b.Peers())
}
