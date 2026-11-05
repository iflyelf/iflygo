// Package model 定义控制面领域模型（与数据库表对应）。
package model

// Network overlay 网络。
type Network struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Cidr4     string `json:"cidr4"`
	Cidr6     string `json:"cidr6"`
	Gateway4  string `json:"gateway4"`
	Gateway6  string `json:"gateway6"`
	DNS       string `json:"dns"`
	MTU       int    `json:"mtu"`
	CreatedAt string `json:"created_at"`
}

// CA 证书颁发机构（每个网络一个）。
type CA struct {
	ID          int64  `json:"id"`
	NetworkID   int64  `json:"network_id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	ExpiresAt   string `json:"expires_at"`
	CreatedAt   string `json:"created_at"`
}

// ClientNode 客户端节点。
type ClientNode struct {
	ID        int64  `json:"id"`
	NetworkID int64  `json:"network_id"`
	Name      string `json:"name"`
	IPv4      string `json:"ipv4"`
	IPv6      string `json:"ipv6"`
	Groups    string `json:"groups"`
	Subnets   string `json:"subnets"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Kernel    string `json:"kernel"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	LastSeen  string `json:"last_seen"`
	ConfigRev int64  `json:"config_rev"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Bundle 客户端配置包（证书 + 配置）。
type Bundle struct {
	Network   string `json:"network"`
	IPv4      string `json:"ipv4"`
	IPv6      string `json:"ipv6"`
	MTU       int    `json:"mtu"`
	DNS       string `json:"dns"`
	CA        string `json:"ca"`
	Cert      string `json:"cert"`
	Key       string `json:"key"`
	Config    string `json:"config"`
	ConfigRev int64  `json:"config_rev"`
}

// ServerNode 服务端节点（lighthouse / relay / edge / gateway）。
type ServerNode struct {
	ID         int64  `json:"id"`
	NetworkID  int64  `json:"network_id"`
	Name       string `json:"name"`
	Roles      string `json:"roles"`
	Region     string `json:"region"`
	OverlayIP4 string `json:"overlay_ip4"`
	OverlayIP6 string `json:"overlay_ip6"`
	PublicHost string `json:"public_host"`
	PublicPort int    `json:"public_port"`
	CreatedAt  string `json:"created_at"`
}

// NebulaSettings 网络级 Nebula 选项（存于 networks.nebula，JSON）。
type NebulaSettings struct {
	Cipher               string          `json:"cipher,omitempty"`
	LighthouseInterval   int             `json:"lighthouse_interval,omitempty"`
	ListenPort           int             `json:"listen_port,omitempty"`
	Punch                bool            `json:"punch,omitempty"`
	PunchRespond         bool            `json:"punch_respond,omitempty"`
	PunchDelay           string          `json:"punch_delay,omitempty"`
	AmRelay              bool            `json:"am_relay,omitempty"`
	UseRelays            bool            `json:"use_relays,omitempty"`
	Relays               []string        `json:"relays,omitempty"`
	TunDevice            string          `json:"tun_device,omitempty"`
	TunDisabled          bool            `json:"tun_disabled,omitempty"`
	PreferredRanges      []string        `json:"preferred_ranges,omitempty"`
	RemoteAllowList      map[string]bool `json:"remote_allow_list,omitempty"`
	HandshakeTryInterval string          `json:"handshake_try_interval,omitempty"`
	HandshakeRetries     int             `json:"handshake_retries,omitempty"`
	LogLevel             string          `json:"log_level,omitempty"`
	LogFormat            string          `json:"log_format,omitempty"`
	StatsType            string          `json:"stats_type,omitempty"`
	StatsListen          string          `json:"stats_listen,omitempty"`
}
