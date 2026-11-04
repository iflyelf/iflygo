// Package pki 负责 CA 与节点证书的签发。
//
// 注意：当前为占位实现（Ed25519 + x509），用于打通控制面证书/配置下发流程；
// 接入 overlay 时需替换为 Nebula 证书格式（curve25519 + 自定义结构）。
package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"time"
)

// CA 证书颁发机构。
type CA struct {
	Name        string
	CertPEM     string
	KeyPEM      string
	Fingerprint string
	ExpiresAt   time.Time
}

// NodeKey 节点密钥对。
type NodeKey struct {
	PublicKeyPEM  string
	PrivateKeyPEM string
}

// CreateCA 生成自签 CA。
func CreateCA(name string, duration time.Duration) (*CA, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(duration),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	fp := sha256.Sum256(der)
	return &CA{
		Name:        name,
		CertPEM:     string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM:      string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		Fingerprint: hex.EncodeToString(fp[:]),
		ExpiresAt:   now.Add(duration),
	}, nil
}

// GenerateNodeKey 生成节点 Ed25519 密钥对。
func GenerateNodeKey() (*NodeKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return &NodeKey{
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}, nil
}

// IssueNodeCert 用 CA 为节点签发证书，绑定 IP 地址。
func IssueNodeCert(ca *CA, node *NodeKey, name string, ips []netip.Addr, duration time.Duration) (string, error) {
	caCert, err := parseCert(ca.CertPEM)
	if err != nil {
		return "", err
	}
	caKey, err := parseKey(ca.KeyPEM)
	if err != nil {
		return "", err
	}
	nodePub, err := parsePubKey(node.PublicKeyPEM)
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	ipAddrs := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		ipAddrs = append(ipAddrs, ip.AsSlice())
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(duration),
		IPAddresses:  ipAddrs,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, nodePub, caKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

func parseCert(pemStr string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("证书 PEM 解码失败")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseKey(pemStr string) (any, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("私钥 PEM 解码失败")
	}
	return x509.ParsePKCS8PrivateKey(block.Bytes)
}

func parsePubKey(pemStr string) (any, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("公钥 PEM 解码失败")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}
