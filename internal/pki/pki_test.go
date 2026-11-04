package pki_test

import (
	"crypto/x509"
	"encoding/pem"
	"net/netip"
	"testing"
	"time"

	"github.com/iflyelf/iflygo/internal/pki"
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

	ips := []netip.Addr{netip.MustParseAddr("10.88.0.101"), netip.MustParseAddr("fd88::101")}
	certPEM, err := pki.IssueNodeCert(ca, node, "node-1", ips, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("证书 PEM 解码失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) != 2 {
		t.Fatalf("want 2 IP SANs, got %d", len(cert.IPAddresses))
	}
}
