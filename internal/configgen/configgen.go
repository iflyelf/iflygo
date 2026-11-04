// Package configgen 生成客户端配置（当前为占位，接入 overlay 后改为 Nebula 配置）。
package configgen

import (
	"gopkg.in/yaml.v3"
)

// ClientConfig 客户端配置结构。
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

// Render 生成 YAML 配置。
func Render(c ClientConfig) (string, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
