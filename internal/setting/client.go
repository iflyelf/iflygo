package setting

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/config"
)

// clientRegistry 客户端下发默认参数（网络级 DNS/MTU 优先）。
func clientRegistry() []Item {
	return []Item{
		{Key: "client.dns", Group: "客户端下发",
			Label: "客户端默认 DNS（逗号分隔，建议 overlay 内可达，如 10.77.0.1）", Type: "string",
			Note: "网络级 DNS 优先；Android VpnService 会把该 DNS 下发给系统，用于解析 overlay/内网域名。",
			Get:  func(c *config.Config) string { return c.Client.DNS },
			Set:  func(c *config.Config, v string) { c.Client.DNS = strings.TrimSpace(v) },
			Validate: func(v string) error {
				for _, s := range splitComma(v) {
					if net.ParseIP(s) == nil {
						return fmt.Errorf("DNS 包含非法 IP: %s", s)
					}
				}
				return nil
			}},
		{Key: "client.mtu", Group: "客户端下发",
			Label: "客户端默认 MTU（网络级 MTU 优先）", Type: "int",
			Note:     "默认 1280；Android VpnService 使用该值设置隧道 MTU。",
			Get:      func(c *config.Config) string { return strconv.Itoa(c.Client.MTU) },
			Set:      func(c *config.Config, v string) { setInt(&c.Client.MTU, v) },
			Validate: validateIntRange(576, 9000, "客户端 MTU")},
		{Key: "client.interval", Group: "客户端下发",
			Label: "客户端心跳间隔（如 30s）", Type: "string",
			Note: "写入客户端 client.yaml；客户端约 2 秒内热加载。",
			Get:  func(c *config.Config) string { return c.Client.Interval },
			Set:  func(c *config.Config, v string) { c.Client.Interval = strings.TrimSpace(v) },
			Validate: func(v string) error {
				d, err := time.ParseDuration(strings.TrimSpace(v))
				if err != nil || d <= 0 {
					return fmt.Errorf("心跳间隔格式非法（示例 30s、1m）")
				}
				return nil
			}},
	}
}
