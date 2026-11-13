package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/svc"
)

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func nowStr() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// baseURL 计算对外地址（用于安装命令）。
func baseURL(sc *svc.ServiceContext, r *http.Request) string {
	if snap := sc.ConfigSnapshot(); snap.Server.ExternalURL != "" {
		return snap.Server.ExternalURL
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// normalizeTunnelName 校验隧道资源名称（客户端/服务端节点）。
func normalizeTunnelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("名称必填")
	}
	if len([]rune(name)) > 100 {
		return "", fmt.Errorf("名称最多 100 个字符")
	}
	if strings.ContainsAny(name, "\r\n\t") {
		return "", fmt.Errorf("名称不能包含换行或制表符")
	}
	return name, nil
}
