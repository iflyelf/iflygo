package crossdesk

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TurnConfig 原生信令下发的 TURN 临时凭据配置。
type TurnConfig struct {
	Host   string
	Port   int
	Secret string
	TTL    time.Duration
}

// turnFor 生成 Coturn REST API 兼容的临时用户名/密码。
func (c TurnConfig) turnFor(userID string) map[string]any {
	host := strings.TrimSpace(c.Host)
	if host == "" || strings.TrimSpace(c.Secret) == "" || userID == "" {
		return nil
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	port := c.Port
	if port <= 0 {
		port = 3478
	}
	exp := time.Now().Add(ttl).Unix()
	username := fmt.Sprintf("%d:%s", exp, userID)
	mac := hmac.New(sha1.New, []byte(c.Secret))
	mac.Write([]byte(username))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return map[string]any{
		"host": host, "port": port, "username": username,
		"password": password, "expires_at": exp,
	}
}

// TurnConfigFromURL 从 CrossDesk Server URL 推断 TURN 主机（同主机，默认 3478）。
func TurnConfigFromURL(raw, secret string, port, ttl int) TurnConfig {
	host := ""
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil {
		host = u.Hostname()
	}
	return TurnConfig{Host: host, Port: port, Secret: secret, TTL: time.Duration(ttl) * time.Second}
}
