package liaison

import (
	"encoding/hex"
	"testing"

	"github.com/tomatome/grdp/protocol/nla"
)

// TestNTLMv2Hash 验证 NTLMv2 哈希（MS-NLMP 官方测试向量）。
func TestNTLMv2Hash(t *testing.T) {
	// MS-NLMP 3.3.2：Password/User/Domain → NTOWFv2 已知向量
	got := nla.NTOWFv2("Password", "User", "Domain")
	want := "0c868a403bfd7a93a3001ef22ef02e3f"
	if hex.EncodeToString(got) != want {
		t.Errorf("NTOWFv2 = %x, want %s", got, want)
	}
}

// TestNTLMv2Negotiate 验证 NLA 客户端能生成 NEGOTIATE 消息。
func TestNTLMv2Negotiate(t *testing.T) {
	cli := nla.NewNTLMv2("DOMAIN", "Administrator", "password123")
	nego := cli.GetNegotiateMessage()
	if nego == nil {
		t.Fatal("GetNegotiateMessage 返回 nil")
	}
	// NEGOTIATE 消息应包含 NTLMSSP 签名
	data := nego.Serialize()
	if len(data) < 12 || string(data[0:8]) != "NTLMSSP\x00" {
		t.Errorf("NEGOTIATE 消息缺少 NTLMSSP 签名: %x", data[:min(len(data), 12)])
	}
}

// TestNTLMv2Authenticate 验证 NLA 客户端能处理 CHALLENGE 并生成 AUTHENTICATE。
func TestNTLMv2Authenticate(t *testing.T) {
	cli := nla.NewNTLMv2("DOMAIN", "Administrator", "password123")
	_ = cli.GetNegotiateMessage()

	// 构造一个最小 CHALLENGE 消息（NTLMSSP signature + type=2 + target info）
	challenge := buildChallenge()
	auth, _ := cli.GetAuthenticateMessage(challenge)
	if auth == nil {
		t.Fatal("GetAuthenticateMessage 返回 nil")
	}
	data := auth.Serialize()
	if len(data) < 12 || string(data[0:8]) != "NTLMSSP\x00" {
		t.Errorf("AUTHENTICATE 消息缺少 NTLMSSP 签名")
	}
}

// TestNTLMv2CredSSP 验证 CredSSP 凭据编码（TSRequest）。
func TestNTLMv2CredSSP(t *testing.T) {
	cli := nla.NewNTLMv2("DOMAIN", "Administrator", "password123")
	domain, user, pwd := cli.GetEncodedCredentials()
	if len(domain) == 0 || len(user) == 0 || len(pwd) == 0 {
		t.Error("GetEncodedCredentials 返回空凭据")
	}
}

// buildChallenge 构造一个最小 NTLM CHALLENGE 消息（type=2）。
func buildChallenge() []byte {
	m := &nla.ChallengeMessage{
		Signature:       []byte("NTLMSSP\x00"),
		MessageType:     2,
		NegotiateFlags:  0x00820289,
		ServerChallenge: [8]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef},
	}
	return m.Serialize()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
