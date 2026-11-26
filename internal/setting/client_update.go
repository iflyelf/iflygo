package setting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/iflyelf/iflygo/internal/config"
)

// clientUpdateRegistry 客户端自动升级配置。
func clientUpdateRegistry() []Item {
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
		{Key: "client_update.enabled", Group: "客户端升级",
			Label: "启用 iflygo-client 自动升级", Type: "bool",
			Note: "启用后，安装脚本与带 --auto-update 的客户端会从清单检查并升级；关闭后服务端只返回 enabled=false。",
			Get:  func(c *config.Config) string { return b2s(c.ClientUpdate.Enabled) },
			Set:  func(c *config.Config, v string) { c.ClientUpdate.Enabled = v == "true" }},
		{Key: "client_update.manifest_url", Group: "客户端升级",
			Label: "客户端升级清单 URL", Type: "string",
			Note:     "清单 JSON 需包含 version 与 assets（linux-amd64 等），assets 项含 file/sha256/size 或 url。",
			Get:      func(c *config.Config) string { return c.ClientUpdate.ManifestURL },
			Set:      func(c *config.Config, v string) { c.ClientUpdate.ManifestURL = strings.TrimSpace(v) },
			Validate: validateURL("客户端升级清单")},
		{Key: "client_update.base_url", Group: "客户端升级",
			Label: "客户端升级制品基础 URL", Type: "string",
			Note: "清单中的 file 为相对文件名时，与 base_url 拼接为下载地址；可指向 GitHub Release 或自建制品站。",
			Get:  func(c *config.Config) string { return c.ClientUpdate.BaseURL },
			Set: func(c *config.Config, v string) {
				c.ClientUpdate.BaseURL = strings.TrimRight(strings.TrimSpace(v), "/")
			},
			Validate: validateURL("客户端升级制品基础 URL")},
	}
}
