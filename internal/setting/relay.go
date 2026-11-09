package setting

import (
	"fmt"
	"net"
	"strings"

	"github.com/iflyelf/iflygo/internal/config"
)

// relayRegistry TCP 中继 / 数据面设置（页面可配置，保存后即时生效）。
//
// mode 会随心跳下发给客户端，作为其默认数据面策略；客户端也可用本地配置文件覆盖。
func relayRegistry() []Item {
	return []Item{
		{Key: "tcp_relay.mode", Group: "数据面 / TCP 中继",
			Label: "客户端默认数据面模式", Type: "enum",
			Options: []string{"auto", "tcp", "udp"},
			Note:    "auto：优先 UDP，不通时自动切 TCP 中继；tcp：始终经 TCP 中继（适合 UDP 被封锁/时通时断）；udp：仅原生 UDP，不使用中继。",
			Get: func(c *config.Config) string {
				if strings.TrimSpace(c.TCPRelay.Mode) == "" {
					return "auto"
				}
				return c.TCPRelay.Mode
			},
			Set: func(c *config.Config, v string) {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "tcp":
					c.TCPRelay.Mode = "tcp"
				case "udp":
					c.TCPRelay.Mode = "udp"
				default:
					c.TCPRelay.Mode = "auto"
				}
			},
			Validate: func(v string) error {
				switch strings.ToLower(strings.TrimSpace(v)) {
				case "auto", "tcp", "udp":
					return nil
				}
				return fmt.Errorf("模式只能是 auto / tcp / udp")
			}},
		{Key: "tcp_relay.listen", Group: "数据面 / TCP 中继",
			Label: "中继监听地址（留空 = 控制面端口 +1；off = 关闭）", Type: "string",
			Note: "保存后立即重启监听，已建立的中继隧道会短暂重连。示例：:8443、0.0.0.0:8443。",
			Get:  func(c *config.Config) string { return c.TCPRelay.Listen },
			Set:  func(c *config.Config, v string) { c.TCPRelay.Listen = strings.TrimSpace(v) },
			Validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" || strings.EqualFold(v, "off") {
					return nil
				}
				if _, err := net.ResolveTCPAddr("tcp", v); err != nil {
					return fmt.Errorf("监听地址非法（示例 :8443 或 0.0.0.0:8443）: %w", err)
				}
				return nil
			}},
		{Key: "tcp_relay.advertise", Group: "数据面 / TCP 中继",
			Label: "对外可达的中继地址（留空 = 按 external_url / 请求 Host 自动推断）", Type: "string",
			Note: "客户端通过心跳获取该地址。反向代理/端口映射场景可显式填写，如 relay.example.com:8443。",
			Get:  func(c *config.Config) string { return c.TCPRelay.Advertise },
			Set:  func(c *config.Config, v string) { c.TCPRelay.Advertise = strings.TrimSpace(v) },
			Validate: func(v string) error {
				v = strings.TrimSpace(v)
				if v == "" {
					return nil
				}
				if strings.ContainsAny(v, " \t\r\n/") {
					return fmt.Errorf("地址不能包含空白或斜杠")
				}
				if _, _, err := net.SplitHostPort(v); err != nil && strings.Contains(v, ":") {
					return fmt.Errorf("地址格式应为 host:port 或 host: %w", err)
				}
				return nil
			}},
		{Key: "tcp_relay.token", Group: "数据面 / TCP 中继",
			Label: "中继共享令牌（可选，建议设置）", Type: "string", Secret: true,
			Note: "客户端会通过已认证的心跳自动获取，无需手工配置。留空则中继不校验令牌，请用防火墙限制来源。",
			Get:  func(c *config.Config) string { return c.TCPRelay.Token },
			Set:  func(c *config.Config, v string) { c.TCPRelay.Token = strings.TrimSpace(v) }},
	}
}
