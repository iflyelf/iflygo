// Package model 定义控制面领域模型（与数据库表对应）。
package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Network overlay 网络。
type Network struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Cidr4    string `json:"cidr4"`
	Cidr6    string `json:"cidr6"`
	Gateway4 string `json:"gateway4"`
	Gateway6 string `json:"gateway6"`
	DNS      string `json:"dns"`
	MTU      int    `json:"mtu"`
	// DNSGateway 显式 DNS 网关（overlay IP）；空表示自动选择。
	DNSGateway string `json:"dns_gateway"`
	// EgressMode 出口模式：off / dns / full；空表示按全局 DNS 路由开关自动推断。
	EgressMode string `json:"egress_mode"`
	// EgressExclude 全量出口排除网段（逗号分隔 CIDR）；空使用内置策略。
	EgressExclude string `json:"egress_exclude"`
	CreatedAt     string `json:"created_at"`
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

// ClientOverride 每台主机的客户端参数覆盖（服务端页面配置）。
//
// 零值表示「跟随网络/全局默认」；UseServerDNS 为三态：
//   - nil：跟随默认（有 DNS 就下发）
//   - true：启用服务端 DNS
//   - false：关闭服务端 DNS（Bundle 不下发 DNS）
type ClientOverride struct {
	UseServerDNS  *bool  `json:"use_server_dns,omitempty"`
	DNS           string `json:"dns,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
	Interval      string `json:"interval,omitempty"`
	DataPlaneMode string `json:"data_plane_mode,omitempty"`
	// DNSRoute 为公网 DNS 添加主机路由（三态：nil 跟随全局，true 启用，false 关闭）。
	DNSRoute *bool `json:"dns_route,omitempty"`
	// DNSGateway 每主机 DNS 网关覆盖（overlay IP）；空则跟随网络配置/自动选择。
	DNSGateway string `json:"dns_gateway,omitempty"`
}

// Scan 实现 sql.Scanner（DB 中存 JSON 文本）。
func (c *ClientOverride) Scan(src any) error {
	var raw string
	switch v := src.(type) {
	case nil:
		*c = ClientOverride{}
		return nil
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("client_settings 类型不支持: %T", src)
	}
	if raw == "" {
		*c = ClientOverride{}
		return nil
	}
	return json.Unmarshal([]byte(raw), c)
}

// Value 实现 driver.Valuer（写入 JSON 文本）。
func (c ClientOverride) Value() (driver.Value, error) {
	if c == (ClientOverride{}) {
		return "", nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return string(b), nil
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
	// ClientSettings 每主机客户端参数覆盖（JSON）。
	ClientSettings ClientOverride `json:"client_settings"`
	// Health 计算字段：网关节点健康状态（online/unknown/offline）。
	Health string `json:"health,omitempty"`
}

// Bundle 客户端配置包（证书 + 配置）。
// ClientSettings 服务端下发给客户端的运行参数（写入 client.yaml）。
type ClientSettings struct {
	DataPlaneMode string `json:"data_plane_mode,omitempty"` // auto/tcp/udp
	TCPRelay      string `json:"tcp_relay,omitempty"`
	TCPRelayToken string `json:"tcp_relay_token,omitempty"`
	Interval      string `json:"interval,omitempty"`
}

type Bundle struct {
	Name      string         `json:"name,omitempty"`
	Network   string         `json:"network"`
	Cidr4     string         `json:"cidr4,omitempty"`
	Cidr6     string         `json:"cidr6,omitempty"`
	IPv4      string         `json:"ipv4"`
	IPv6      string         `json:"ipv6"`
	MTU       int            `json:"mtu"`
	DNS       string         `json:"dns"`
	CA        string         `json:"ca"`
	Cert      string         `json:"cert"`
	Key       string         `json:"key"`
	Config    string         `json:"config"`
	ConfigRev int64          `json:"config_rev"`
	Routes    []RouteSpec    `json:"routes,omitempty"`
	Client    ClientSettings `json:"client,omitempty"`
	// BootstrapScript 网关节点部署脚本（NAT 等）；仅 ServerNodeBundle 返回。
	BootstrapScript string `json:"bootstrap_script,omitempty"`
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
	// Health 计算字段：网关节点健康状态（online/unknown/offline）。
	Health string `json:"health,omitempty"`
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

// SubnetRoute 不安全路由（子网路由），通过网关节点转发。
type SubnetRoute struct {
	ID            int64          `json:"id"`
	NetworkID     int64          `json:"network_id"`
	CIDR          string         `json:"cidr"`
	MTU           int            `json:"mtu"`
	Metric        int            `json:"metric"`
	Mode          string         `json:"mode"`
	AllDownPolicy string         `json:"all_down_policy"`
	Enabled       bool           `json:"enabled"`
	Description   string         `json:"description"`
	CreatedAt     string         `json:"created_at"`
	Gateways      []RouteGateway `json:"gateways,omitempty"`
}

// RouteGateway 路由网关（加权，支持优先级分层）。
type RouteGateway struct {
	ID           int64  `json:"id"`
	RouteID      int64  `json:"route_id"`
	NodeID       int64  `json:"node_id,omitempty"`
	OverlayIP    string `json:"overlay_ip"`
	Weight       int    `json:"weight"`
	Priority     int    `json:"priority"`
	HealthTarget string `json:"health_target"`
	HealthPort   int    `json:"health_port"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
}

// GatewaySpec 客户端侧网关描述（用于健康探测与动态 reload）。
type GatewaySpec struct {
	IP           string `json:"ip"`
	Weight       int    `json:"weight"`
	Priority     int    `json:"priority"`
	HealthTarget string `json:"health_target,omitempty"`
	HealthPort   int    `json:"health_port,omitempty"`
}

// RouteSpec 客户端侧路由描述。
type RouteSpec struct {
	CIDR          string        `json:"cidr"`
	MTU           int           `json:"mtu"`
	Metric        int           `json:"metric"`
	Mode          string        `json:"mode"`
	AllDownPolicy string        `json:"all_down_policy"`
	Gateways      []GatewaySpec `json:"gateways"`
}

// EdgeService 边缘接入服务（自定义域名/二级前缀 → overlay 目标）。
type EdgeService struct {
	ID         int64  `json:"id"`
	NetworkID  int64  `json:"network_id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Target     string `json:"target"`
	Protocol   string `json:"protocol"`    // http | https | tcp
	TLSMode    string `json:"tls_mode"`    // terminate | passthrough | off
	PublicPort int    `json:"public_port"` // tcp 协议的公网监听端口
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
}
