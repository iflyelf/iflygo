package mobile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/iflyelf/iflygo/internal/client"
)

func TestVPNParams(t *testing.T) {
	dir := t.TempDir()
	st := &client.State{
		Server: "https://example.test", ClientID: 1, Secret: "s",
		Name: "phone", Network: "home", Cidr4: "10.77.0.0/16", Cidr6: "fd77::/64",
		IPv4: "10.77.0.2", IPv6: "fd77::2", MTU: 1280, DNS: "1.1.1.1,8.8.8.8",
	}
	if err := st.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "routes.json"), []byte(`[{"cidr":"10.8.1.0/24"},{"cidr":"10.8.2.0/24"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New("https://example.test", dir)
	out, err := c.VPNParams()
	if err != nil {
		t.Fatalf("VPNParams: %v", err)
	}
	var p vpnParams
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.IPv4 != "10.77.0.2" || p.IPv4Prefix != 16 || p.IPv6 != "fd77::2" || p.IPv6Prefix != 64 {
		t.Fatalf("address/prefix: %+v", p)
	}
	if p.MTU != 1280 {
		t.Fatalf("mtu: %d", p.MTU)
	}
	if len(p.Routes) != 4 {
		t.Fatalf("routes: %v", p.Routes)
	}
	if len(p.DNS) != 2 || p.DNS[0] != "1.1.1.1" {
		t.Fatalf("dns: %v", p.DNS)
	}
}

func TestStatus(t *testing.T) {
	dir := t.TempDir()
	c := New("https://example.test", dir)
	if s := c.Status(); s == "" {
		t.Fatal("empty status")
	}
}
