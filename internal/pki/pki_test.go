package pki_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/slackhq/nebula/cert"
)

func TestCreateCAAndIssue(t *testing.T) {
	ca, err := pki.CreateCA("test-ca", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if ca.CertPEM == "" || ca.KeyPEM == "" || ca.Fingerprint == "" {
		t.Fatal("CA 字段缺失")
	}

	node, err := pki.GenerateNodeKey()
	if err != nil {
		t.Fatal(err)
	}

	networks := []netip.Prefix{
		netip.MustParsePrefix("10.88.0.101/16"),
		netip.MustParsePrefix("fd88::101/64"),
	}
	unsafe := []netip.Prefix{netip.MustParsePrefix("10.8.1.0/24")}
	groups := []string{"home", "ssh"}

	certPEM, err := pki.IssueNodeCert(ca, node, "node-1", networks, unsafe, groups, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// 用 Nebula 证书解析校验
	c, _, err := cert.UnmarshalCertificateFromPEM([]byte(certPEM))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "node-1" {
		t.Fatalf("name 不符: %s", c.Name())
	}
	gotNets := c.Networks()
	if len(gotNets) != 2 {
		t.Fatalf("want 2 networks, got %d", len(gotNets))
	}
	if len(c.UnsafeNetworks()) != 1 {
		t.Fatalf("want 1 unsafe network, got %d", len(c.UnsafeNetworks()))
	}
	if len(c.Groups()) != 2 {
		t.Fatalf("want 2 groups, got %d", len(c.Groups()))
	}
	// 证书应由 CA 签名且未过期
	if c.Expired(time.Now()) {
		t.Fatal("证书不应已过期")
	}
}

func TestParseCert(t *testing.T) {
	ca, err := pki.CreateCA("ca", 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	node, _ := pki.GenerateNodeKey()
	certPEM, err := pki.IssueNodeCert(ca, node, "n", []netip.Prefix{netip.MustParsePrefix("10.88.0.2/16")}, nil, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fp, exp, err := pki.ParseCert(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if fp == "" || exp.IsZero() {
		t.Fatal("ParseCert 返回空值")
	}
}
