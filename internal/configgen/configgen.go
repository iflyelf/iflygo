// Package configgen 生成 Nebula 配置（覆盖全部主要 section）。
//
// 所有值均来自 Input（数据模型/设置），不硬编码任何 IP/CIDR/端口/域名；
// 未设置的字段省略，由 Nebula 自身默认值兜底。
package configgen

import (
	"gopkg.in/yaml.v3"
)

// Config 完整 Nebula 配置。
type Config struct {
	PKI             PKI                 `yaml:"pki"`
	StaticHostMap   map[string][]string `yaml:"static_host_map,omitempty"`
	StaticMap       *StaticMap          `yaml:"static_map,omitempty"`
	Lighthouse      Lighthouse          `yaml:"lighthouse"`
	Listen          Listen              `yaml:"listen"`
	Punchy          *Punchy             `yaml:"punchy,omitempty"`
	Cipher          string              `yaml:"cipher,omitempty"`
	PreferredRanges []string            `yaml:"preferred_ranges,omitempty"`
	Relay           Relay               `yaml:"relay"`
	Tun             Tun                 `yaml:"tun"`
	Logging         *Logging            `yaml:"logging,omitempty"`
	Handshakes      *Handshakes         `yaml:"handshakes,omitempty"`
	Firewall        *Firewall           `yaml:"firewall,omitempty"`
	Stats           *Stats              `yaml:"stats,omitempty"`
	DNS             *DNS                `yaml:"dns,omitempty"`
}

type PKI struct {
	CA                string   `yaml:"ca"`
	Cert              string   `yaml:"cert"`
	Key               string   `yaml:"key"`
	Blocklist         []string `yaml:"blocklist,omitempty"`
	InitiatingVersion int      `yaml:"initiating_version,omitempty"`
}

type StaticMap struct {
	Cadence       string `yaml:"cadence,omitempty"`
	Network       string `yaml:"network,omitempty"`
	LookupTimeout string `yaml:"lookup_timeout,omitempty"`
}

type Lighthouse struct {
	AmLighthouse    bool            `yaml:"am_lighthouse"`
	Interval        int             `yaml:"interval,omitempty"`
	Hosts           []string        `yaml:"hosts,omitempty"`
	RemoteAllowList map[string]bool `yaml:"remote_allow_list,omitempty"`
}

type Listen struct {
	Host          string `yaml:"host,omitempty"`
	Port          int    `yaml:"port"`
	ReadBuffer    int    `yaml:"read_buffer,omitempty"`
	WriteBuffer   int    `yaml:"write_buffer,omitempty"`
	SendRecvError string `yaml:"send_recv_error,omitempty"`
}

type Punchy struct {
	Punch   bool   `yaml:"punch"`
	Respond bool   `yaml:"respond"`
	Delay   string `yaml:"delay,omitempty"`
}

type Relay struct {
	Relays    []string `yaml:"relays,omitempty"`
	AmRelay   bool     `yaml:"am_relay"`
	UseRelays bool     `yaml:"use_relays"`
}

type Route struct {
	Route  string `yaml:"route"`
	Via    any    `yaml:"via"`
	MTU    int    `yaml:"mtu,omitempty"`
	Metric int    `yaml:"metric,omitempty"`
}

type Gateway struct {
	Gateway string `yaml:"gateway"`
	Weight  int    `yaml:"weight"`
}

type Tun struct {
	Disabled           bool    `yaml:"disabled"`
	Dev                string  `yaml:"dev,omitempty"`
	DropLocalBroadcast bool    `yaml:"drop_local_broadcast,omitempty"`
	DropMulticast      bool    `yaml:"drop_multicast,omitempty"`
	TxQueue            int     `yaml:"tx_queue,omitempty"`
	MTU                int     `yaml:"mtu,omitempty"`
	Routes             []Route `yaml:"routes,omitempty"`
	UnsafeRoutes       []Route `yaml:"unsafe_routes,omitempty"`
}

type Logging struct {
	Level  string `yaml:"level,omitempty"`
	Format string `yaml:"format,omitempty"`
}

type Handshakes struct {
	TryInterval   string `yaml:"try_interval,omitempty"`
	Retries       int    `yaml:"retries,omitempty"`
	TriggerBuffer int    `yaml:"trigger_buffer,omitempty"`
}

type FirewallRule struct {
	Port      any      `yaml:"port"`
	Proto     string   `yaml:"proto"`
	Host      string   `yaml:"host,omitempty"`
	Group     string   `yaml:"group,omitempty"`
	Groups    []string `yaml:"groups,omitempty"`
	CIDR      string   `yaml:"cidr,omitempty"`
	LocalCIDR string   `yaml:"local_cidr,omitempty"`
}

type Conntrack struct {
	TCPTimeout     string `yaml:"tcp_timeout,omitempty"`
	UDPTimeout     string `yaml:"udp_timeout,omitempty"`
	DefaultTimeout string `yaml:"default_timeout,omitempty"`
}

type Firewall struct {
	Conntrack Conntrack      `yaml:"conntrack,omitempty"`
	Outbound  []FirewallRule `yaml:"outbound,omitempty"`
	Inbound   []FirewallRule `yaml:"inbound,omitempty"`
}

type Stats struct {
	Type     string `yaml:"type"`
	Listen   string `yaml:"listen"`
	Interval string `yaml:"interval,omitempty"`
}

type DNS struct {
	Host   string `yaml:"host,omitempty"`
	Port   int    `yaml:"port,omitempty"`
	Server string `yaml:"server,omitempty"`
}

// Input 生成配置所需的数据。
type Input struct {
	CAPath   string
	CertPath string
	KeyPath  string

	Blocklist []string

	IsLighthouse bool
	ListenHost   string
	ListenPort   int

	Lighthouses   []string            // lighthouse.hosts（overlay IP）
	StaticHostMap map[string][]string // overlay IP -> 公网 host:port

	AmRelay   bool
	UseRelays bool
	Relays    []string

	Cidr4, Cidr6 string
	MTU          int
	DNS          []string

	Cipher          string
	TunDevice       string
	TunDisabled     bool
	PreferredRanges []string
	RemoteAllowList map[string]bool

	Punch        bool
	PunchRespond bool
	PunchDelay   string

	HandshakeTryInterval string
	HandshakeRetries     int

	LogLevel    string
	LogFormat   string
	StatsType   string
	StatsListen string

	UnsafeRoutes []Route
}

// Build 生成完整 Nebula YAML。
func Build(in Input) ([]byte, error) {
	cfg := Config{
		PKI: PKI{
			CA:                in.CAPath,
			Cert:              in.CertPath,
			Key:               in.KeyPath,
			Blocklist:         in.Blocklist,
			InitiatingVersion: 2,
		},
		StaticHostMap: in.StaticHostMap,
		Lighthouse: Lighthouse{
			AmLighthouse:    in.IsLighthouse,
			Hosts:           in.Lighthouses,
			RemoteAllowList: in.RemoteAllowList,
		},
		Listen: Listen{
			Host: in.ListenHost,
			Port: in.ListenPort,
		},
		Cipher:          in.Cipher,
		PreferredRanges: in.PreferredRanges,
		Relay: Relay{
			Relays:    in.Relays,
			AmRelay:   in.AmRelay,
			UseRelays: in.UseRelays,
		},
		Tun: Tun{
			Disabled:     in.TunDisabled,
			Dev:          in.TunDevice,
			MTU:          in.MTU,
			UnsafeRoutes: in.UnsafeRoutes,
		},
	}

	if in.Punch || in.PunchRespond || in.PunchDelay != "" {
		cfg.Punchy = &Punchy{Punch: in.Punch, Respond: in.PunchRespond, Delay: in.PunchDelay}
	}
	if in.LogLevel != "" || in.LogFormat != "" {
		cfg.Logging = &Logging{Level: in.LogLevel, Format: in.LogFormat}
	}
	if in.HandshakeTryInterval != "" || in.HandshakeRetries != 0 {
		cfg.Handshakes = &Handshakes{TryInterval: in.HandshakeTryInterval, Retries: in.HandshakeRetries}
	}
	if in.StatsType != "" || in.StatsListen != "" {
		cfg.Stats = &Stats{Type: in.StatsType, Listen: in.StatsListen}
	}
	if len(in.DNS) > 0 {
		cfg.DNS = &DNS{Server: in.DNS[0]}
	}
	cfg.Firewall = defaultFirewall()

	return yaml.Marshal(cfg)
}

// defaultFirewall 生成默认放行策略（后续可从设置配置）。
func defaultFirewall() *Firewall {
	allow := FirewallRule{Port: "any", Proto: "any", Host: "any"}
	return &Firewall{
		Outbound: []FirewallRule{allow},
		Inbound:  []FirewallRule{allow},
	}
}

// ClientConfig 保留旧的客户端展示配置（用于 Web 展示，非 Nebula 配置）。
type ClientConfig struct {
	Network     string   `yaml:"network"`
	IPv4        string   `yaml:"ipv4,omitempty"`
	IPv6        string   `yaml:"ipv6,omitempty"`
	MTU         int      `yaml:"mtu"`
	DNS         string   `yaml:"dns,omitempty"`
	Lighthouses []string `yaml:"lighthouses,omitempty"`
	Relays      []string `yaml:"relays,omitempty"`
	Routers     []string `yaml:"routers,omitempty"`
}

// Render 生成展示用 YAML。
func Render(c ClientConfig) (string, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
