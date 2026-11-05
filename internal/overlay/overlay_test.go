package overlay

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRewritePKIPaths(t *testing.T) {
	in := []byte("pki:\n  ca: ca.crt\n  cert: node.crt\n  key: node.key\n")
	out, err := RewritePKIPaths(in, "/var/lib/iflygo/ca.crt", "/var/lib/iflygo/node.crt", "/var/lib/iflygo/node.key")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	pki, ok := m["pki"].(map[string]any)
	if !ok {
		t.Fatal("缺少 pki 段")
	}
	if pki["ca"] != "/var/lib/iflygo/ca.crt" || pki["cert"] != "/var/lib/iflygo/node.crt" || pki["key"] != "/var/lib/iflygo/node.key" {
		t.Fatalf("pki 路径未改写: %+v", pki)
	}
}

func TestRewritePKIPathsNoPkiSection(t *testing.T) {
	out, err := RewritePKIPaths([]byte("listen:\n  port: 0\n"), "/a", "/b", "/c")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["pki"] == nil {
		t.Fatal("缺少 pki 段")
	}
}
