// Package pki 负责 CA 与节点证书的签发（Nebula 证书格式）。
//
// 与 Nebula 保持一致：
//   - CA 使用 ed25519 签名密钥（curve 记为 Curve_CURVE25519）
//   - 节点使用 X25519 密钥对（Noise 握手用），证书绑定 networks/groups/unsafe_networks
package pki

import (
	"crypto/rand"
	"fmt"
	"net/netip"
	"time"

	"github.com/slackhq/nebula/cert"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ed25519"
)

// CA 证书颁发机构。
type CA struct {
	Name        string
	CertPEM     string
	KeyPEM      string
	Fingerprint string
	ExpiresAt   time.Time
}

// NodeKey 节点密钥对（X25519）。
type NodeKey struct {
	PublicKeyPEM  string
	PrivateKeyPEM string
}

// CreateCA 生成自签 CA（ed25519 签名）。
func CreateCA(name string, duration time.Duration) (*CA, error) {
	pub, rawPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	t := &cert.TBSCertificate{
		Version:   cert.Version2,
		Name:      name,
		NotBefore: now.Add(-time.Minute),
		NotAfter:  now.Add(duration),
		PublicKey: pub,
		IsCA:      true,
		Curve:     cert.Curve_CURVE25519,
	}
	c, err := t.Sign(nil, cert.Curve_CURVE25519, rawPriv)
	if err != nil {
		return nil, err
	}
	certPEM, err := c.MarshalPEM()
	if err != nil {
		return nil, err
	}
	fp, err := c.Fingerprint()
	if err != nil {
		return nil, err
	}
	return &CA{
		Name:        name,
		CertPEM:     string(certPEM),
		KeyPEM:      string(cert.MarshalSigningPrivateKeyToPEM(cert.Curve_CURVE25519, rawPriv)),
		Fingerprint: fp,
		ExpiresAt:   now.Add(duration),
	}, nil
}

// GenerateNodeKey 生成节点 X25519 密钥对。
func GenerateNodeKey() (*NodeKey, error) {
	privkey := make([]byte, 32)
	if _, err := rand.Read(privkey); err != nil {
		return nil, err
	}
	pubkey, err := curve25519.X25519(privkey, curve25519.Basepoint)
	if err != nil {
		return nil, err
	}
	return &NodeKey{
		PublicKeyPEM:  string(cert.MarshalPublicKeyToPEM(cert.Curve_CURVE25519, pubkey)),
		PrivateKeyPEM: string(cert.MarshalPrivateKeyToPEM(cert.Curve_CURVE25519, privkey)),
	}, nil
}

// IssueNodeCert 用 CA 为节点签发证书，绑定 networks / unsafe_networks / groups。
func IssueNodeCert(ca *CA, node *NodeKey, name string, networks, unsafeNetworks []netip.Prefix, groups []string, duration time.Duration) (string, error) {
	caCert, _, err := cert.UnmarshalCertificateFromPEM([]byte(ca.CertPEM))
	if err != nil {
		return "", fmt.Errorf("解析 CA 证书: %w", err)
	}
	caKey, _, _, err := cert.UnmarshalSigningPrivateKeyFromPEM([]byte(ca.KeyPEM))
	if err != nil {
		return "", fmt.Errorf("解析 CA 私钥: %w", err)
	}
	pub, _, _, err := cert.UnmarshalPublicKeyFromPEM([]byte(node.PublicKeyPEM))
	if err != nil {
		return "", fmt.Errorf("解析节点公钥: %w", err)
	}

	now := time.Now()
	t := &cert.TBSCertificate{
		Version:        cert.Version2,
		Name:           name,
		Networks:       networks,
		UnsafeNetworks: unsafeNetworks,
		Groups:         groups,
		NotBefore:      now.Add(-time.Minute),
		NotAfter:       now.Add(duration),
		PublicKey:      pub,
		IsCA:           false,
		Curve:          cert.Curve_CURVE25519,
	}
	c, err := t.Sign(caCert, cert.Curve_CURVE25519, caKey)
	if err != nil {
		return "", fmt.Errorf("签发节点证书: %w", err)
	}
	b, err := c.MarshalPEM()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseCert 解析证书，返回指纹与到期时间（用于展示/校验）。
func ParseCert(certPEM string) (fingerprint string, expiresAt time.Time, err error) {
	c, _, err := cert.UnmarshalCertificateFromPEM([]byte(certPEM))
	if err != nil {
		return "", time.Time{}, err
	}
	fp, err := c.Fingerprint()
	if err != nil {
		return "", time.Time{}, err
	}
	return fp, c.NotAfter(), nil
}
