package configgen_test

import (
	"strings"
	"testing"

	"github.com/iflyelf/iflygo/internal/configgen"
	"gopkg.in/yaml.v3"
)

func TestBuildNoHardcode(t *testing.T) {
	in := configgen.Input{
		CAPath:             "ca.crt",
		CertPath:           "node.crt",
		KeyPath:            "node.key",
		IsLighthouse:       false,
		LighthouseInterval: 30,
		ListenPort:         0,
		Lighthouses:        []string{"10.77.0.1", "fd77::1"},
		StaticHostMap:      map[string][]string{"10.77.0.1": {"lh.example.com:4242"}, "fd77::1": {"lh.example.com:4242"}},
		Relays:             []string{"10.77.0.2"},
		UseRelays:          true,
		Cidr4:              "10.77.0.0/16",
		Cidr6:              "fd77::/64",
		MTU:                1280,
		Cipher:             "aes",
		TunDevice:          "iflygo0",
		Punch:              true,
		PunchRespond:       true,
		PunchDelay:         "2s",
		LogLevel:           "info",
	}
	b, err := configgen.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)

	for _, want := range []string{
		"lh.example.com:4242",
		"10.77.0.1",
		"fd77::1",
		"mtu: 1280",
		"cipher: aes",
		"dev: iflygo0",
		"punch: true",
		"interval: 30",
		"am_lighthouse: false",
		"use_relays: true",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("缺少 %q\n%s", want, s)
		}
	}
	// 不得出现示例硬编码值
	for _, bad := range []string{"10.88", "6688", "connt.xiaonuo", "nebula1"} {
		if strings.Contains(s, bad) {
			t.Fatalf("出现硬编码 %q\n%s", bad, s)
		}
	}
	// 能被 YAML 解析
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("YAML 解析失败: %v", err)
	}
}

func TestBuildLighthouse(t *testing.T) {
	b, err := configgen.Build(configgen.Input{
		CAPath: "ca.crt", CertPath: "node.crt", KeyPath: "node.key",
		IsLighthouse: true,
		ListenPort:   4242,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "am_lighthouse: true") {
		t.Fatalf("lighthouse 配置缺少 am_lighthouse: true\n%s", s)
	}
	if !strings.Contains(s, "port: 4242") {
		t.Fatalf("lighthouse 配置缺少 listen.port\n%s", s)
	}
}
