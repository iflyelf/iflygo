package setting

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/iflyelf/iflygo/internal/config"
)

// crossdeskRegistry 远程桌面（CrossDesk）集成配置。
func crossdeskRegistry() []Item {
	validateURL := func(label string) func(string) error {
		return func(v string) error {
			v = strings.TrimSpace(v)
			if v == "" {
				return nil
			}
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("%s 必须是 http/https URL", label)
			}
			return nil
		}
	}
	return []Item{
		{Key: "crossdesk.enabled", Group: "远程桌面（CrossDesk）",
			Label: "启用 CrossDesk 远程桌面集成", Type: "bool",
			Note: "启用后在「远程管理 → 远程桌面（CrossDesk）」显示服务状态、Web 客户端与下载入口。",
			Get:  func(c *config.Config) string { return b2s(c.CrossDesk.Enabled) },
			Set:  func(c *config.Config, v string) { c.CrossDesk.Enabled = v == "true" }},
		{Key: "crossdesk.server_url", Group: "远程桌面（CrossDesk）",
			Label: "CrossDesk Server 地址（如 https://rd.example.com:9099）", Type: "string",
			Note:     "用于状态检查与客户端自托管配置；信令使用该地址的 WSS 端口。",
			Get:      func(c *config.Config) string { return c.CrossDesk.ServerURL },
			Set:      func(c *config.Config, v string) { c.CrossDesk.ServerURL = strings.TrimSpace(v) },
			Validate: validateURL("CrossDesk Server 地址")},
		{Key: "crossdesk.web_url", Group: "远程桌面（CrossDesk）",
			Label: "Web 客户端地址（默认 https://web.crossdesk.cn）", Type: "string",
			Note:     "浏览器远控入口；自托管 Web 客户端时可填自己的 HTTPS 地址。",
			Get:      func(c *config.Config) string { return c.CrossDesk.WebURL },
			Set:      func(c *config.Config, v string) { c.CrossDesk.WebURL = strings.TrimSpace(v) },
			Validate: validateURL("Web 客户端地址")},
		{Key: "crossdesk.download_url", Group: "远程桌面（CrossDesk）",
			Label: "客户端下载地址", Type: "string",
			Note:     "Windows/macOS/Linux/iOS/Android/鸿蒙/CLI 下载入口。",
			Get:      func(c *config.Config) string { return c.CrossDesk.DownloadURL },
			Set:      func(c *config.Config, v string) { c.CrossDesk.DownloadURL = strings.TrimSpace(v) },
			Validate: validateURL("客户端下载地址")},
		{Key: "crossdesk.insecure_tls", Group: "远程桌面（CrossDesk）",
			Label: "状态检查跳过 TLS 证书校验（仅自签证书测试）", Type: "bool",
			Note: "生产环境应导入服务端根证书，不建议开启。",
			Get:  func(c *config.Config) string { return b2s(c.CrossDesk.InsecureTLS) },
			Set:  func(c *config.Config, v string) { c.CrossDesk.InsecureTLS = v == "true" }},
		{Key: "crossdesk.native_signaling", Group: "远程桌面（CrossDesk）",
			Label: "使用 iFlyGo 原生 Go 信令服务", Type: "bool",
			Note: "开启后 WSS 由 iFlyGo 提供，不再依赖外部 CrossDesk Server；设备 ID/密码与 TURN 由 iFlyGo 管理。",
			Get:  func(c *config.Config) string { return b2s(c.CrossDesk.NativeSignaling) },
			Set:  func(c *config.Config, v string) { c.CrossDesk.NativeSignaling = v == "true" }},
		{Key: "crossdesk.signal_path", Group: "远程桌面（CrossDesk）",
			Label: "原生信令 WSS 路径（默认 /crossdesk/signal）", Type: "string", Restart: true,
			Note: "CrossDesk 客户端的自托管地址需包含该路径；反向代理要支持 WebSocket 升级。修改后需重启服务端。",
			Get:  func(c *config.Config) string { return c.CrossDesk.SignalPath },
			Set:  func(c *config.Config, v string) { c.CrossDesk.SignalPath = strings.TrimSpace(v) },
			Validate: func(v string) error {
				v = strings.TrimSpace(v)
				if first := v; first == "" || first[0] != '/' || strings.ContainsAny(v, " \t\r\n") {
					return fmt.Errorf("信令路径必须以 / 开头且不能包含空白")
				}
				return nil
			}},
		{Key: "crossdesk.turn_host", Group: "远程桌面（CrossDesk）",
			Label: "TURN 公网地址（可选，默认取 Server 主机名）", Type: "string", Restart: true,
			Note: "原生信令下发临时 TURN 凭据；留空时从 CrossDesk Server 地址推断。",
			Get:  func(c *config.Config) string { return c.CrossDesk.TurnHost },
			Set:  func(c *config.Config, v string) { c.CrossDesk.TurnHost = strings.TrimSpace(v) }},
		{Key: "crossdesk.turn_port", Group: "远程桌面（CrossDesk）",
			Label: "TURN 端口（默认 3478）", Type: "int", Restart: true,
			Get:      func(c *config.Config) string { return strconv.Itoa(c.CrossDesk.TurnPort) },
			Set:      func(c *config.Config, v string) { setInt(&c.CrossDesk.TurnPort, v) },
			Validate: validateIntRange(1, 65535, "TURN 端口")},
		{Key: "crossdesk.turn_secret", Group: "远程桌面（CrossDesk）", Secret: true,
			Label: "TURN 共享密钥（与 Coturn static-auth-secret 一致）", Type: "string", Restart: true,
			Get: func(c *config.Config) string { return c.CrossDesk.TurnSecret },
			Set: func(c *config.Config, v string) { c.CrossDesk.TurnSecret = strings.TrimSpace(v) }},
		{Key: "crossdesk.turn_ttl", Group: "远程桌面（CrossDesk）",
			Label: "TURN 临时凭据有效期（秒，默认 3600）", Type: "int", Restart: true,
			Get:      func(c *config.Config) string { return strconv.Itoa(c.CrossDesk.TurnTTL) },
			Set:      func(c *config.Config, v string) { setInt(&c.CrossDesk.TurnTTL, v) },
			Validate: validateIntRange(60, 86400, "TURN 凭据有效期")},
	}
}
