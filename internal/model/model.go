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
