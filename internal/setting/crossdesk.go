package setting

import (
	"fmt"
	"net/url"
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
	}
}
