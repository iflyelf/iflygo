package setting

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
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
		{Key: "client.dns_route", Group: "客户端下发",
			Label: "为公网 DNS 自动添加主机路由（/32 或 /128，经 overlay 网关）", Type: "bool",
			Note: "开启后，若客户端 DNS 不在 overlay 网段内，会下发对应主机路由；" +
				"需在网关节点执行 NAT 策略（隧道 → 参数下发页可生成脚本）。主机级可单独覆盖。",
			Get: func(c *config.Config) string { return b2s(c.Client.DNSRoute) },
			Set: func(c *config.Config, v string) { c.Client.DNSRoute = v == "true" }},
		{Key: "client.auto_nat", Group: "客户端下发",
			Label: "自动为健康网关应用 DNS / 全量出口 NAT", Type: "bool",
			Note: "网关节点匹配到 Agent 后，后台每分钟检查并自动下发 NAT 命令任务；" +
				"需 agent.allow_exec_mode=true。网关健康变化时自动切换并递增客户端配置版本。",
			Get: func(c *config.Config) string { return b2s(c.Client.AutoNAT) },
			Set: func(c *config.Config, v string) { c.Client.AutoNAT = v == "true" }},
		{Key: "client.auto_nat_max_retries", Group: "客户端下发",
			Label: "自动 NAT 最大重试次数（达到后停止并告警）", Type: "int",
			Note:     "默认 5；任务失败按 10 分钟退避重试，达到上限后不再自动重建，可在参数下发页查看状态。",
			Get:      func(c *config.Config) string { return strconv.Itoa(c.Client.AutoNATMaxRetries) },
			Set:      func(c *config.Config, v string) { setInt(&c.Client.AutoNATMaxRetries, v) },
			Validate: validateIntRange(1, 100, "自动 NAT 最大重试次数")},
		{Key: "client.auto_nat_webhook", Group: "客户端下发",
			Label: "NAT 失败/恢复告警 Webhook（可选，http/https）", Type: "string",
			Note: "达到最大重试次数或恢复成功时 POST JSON；留空则只写审计日志并在页面展示状态。",
			Get:  func(c *config.Config) string { return c.Client.AutoNATWebhook },
			Set:  func(c *config.Config, v string) { c.Client.AutoNATWebhook = strings.TrimSpace(v) },
			Validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				u, err := url.Parse(v)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return fmt.Errorf("Webhook 必须是 http/https URL")
				}
				return nil
			}},
		{Key: "client.egress_exclude", Group: "客户端下发",
			Label: "全量出口额外排除网段（逗号分隔 CIDR，如 10.0.0.0/8,192.168.0.0/16）", Type: "string",
			Note: "在内置内网/控制面排除策略之外追加；全量出口只下发未被排除的公网前缀。",
			Get:  func(c *config.Config) string { return c.Client.EgressExclude },
			Set:  func(c *config.Config, v string) { c.Client.EgressExclude = strings.TrimSpace(v) },
			Validate: func(v string) error {
				for _, s := range splitComma(v) {
					if _, err := netip.ParsePrefix(s); err != nil {
						return fmt.Errorf("排除网段不是合法 CIDR: %s", s)
					}
				}
				return nil
			}},
	}
}
