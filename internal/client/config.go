package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig 客户端配置文件（默认 <state-dir>/client.yaml，支持热加载）。
//
// 优先级：命令行参数 > 配置文件 > 服务端心跳下发 > 内置默认。
type FileConfig struct {
	// Interval 心跳间隔（如 30s）；留空默认 30s。
	Interval string `yaml:"interval"`
	// DataPlane 数据面策略。
	DataPlane DataPlaneConfig `yaml:"data_plane"`
	// Log 日志（level 目前仅用于展示与后续扩展）。
	Log struct {
		Level string `yaml:"level"`
	} `yaml:"log"`
}

// DataPlaneConfig 数据面配置。
type DataPlaneConfig struct {
	// Mode auto / tcp / udp。auto：UDP 优先，自动兜底 TCP；
	// tcp：始终经 TCP 中继；udp：仅原生 UDP。
	Mode string `yaml:"mode"`
	// TCPRelay 中继服务端地址（host:port）；留空由服务端心跳自动下发。
	TCPRelay string `yaml:"tcp_relay"`
	// TCPRelayToken 中继共享令牌；留空由服务端心跳自动下发。
	TCPRelayToken string `yaml:"tcp_relay_token"`
}

// DefaultFileConfigPath 默认配置文件路径。
func DefaultFileConfigPath(stateDir string) string {
	return filepath.Join(stateDir, "client.yaml")
}

// LoadFileConfig 读取客户端配置；文件不存在时返回空配置（全部走默认/服务端下发）。
func LoadFileConfig(path string) (*FileConfig, error) {
	cfg := &FileConfig{}
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取客户端配置失败: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析客户端配置失败: %w", err)
	}
	return cfg, nil
}

// IntervalDuration 解析心跳间隔；非法或空值返回 def。
func (c *FileConfig) IntervalDuration(def time.Duration) time.Duration {
	if c == nil || strings.TrimSpace(c.Interval) == "" {
		return def
	}
	d, err := time.ParseDuration(strings.TrimSpace(c.Interval))
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// NormalizeMode 归一化数据面模式。
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tcp":
		return "tcp"
	case "udp":
		return "udp"
	default:
		return "auto"
	}
}

// RelaySettings 解析后的数据面策略。
type RelaySettings struct {
	Mode  string
	Addr  string
	Token string
}

// ResolveRelay 按「命令行 > 配置文件 > 服务端心跳」优先级解析数据面策略。
//
// cliRelay 为 "off" 时等价于 mode=udp（兼容旧参数）。
func ResolveRelay(cliMode, cliRelay, cliToken string, file *FileConfig, hb *HeartbeatResult) RelaySettings {
	mode := strings.ToLower(strings.TrimSpace(cliMode))
	if mode == "" && file != nil {
		mode = strings.ToLower(strings.TrimSpace(file.DataPlane.Mode))
	}
	if mode == "" && hb != nil {
		mode = strings.ToLower(strings.TrimSpace(hb.TCPRelayMode))
	}
	mode = NormalizeMode(mode)

	addr := strings.TrimSpace(cliRelay)
	if strings.EqualFold(addr, "off") {
		mode = "udp"
		addr = ""
	}
	if addr == "" && file != nil {
		addr = strings.TrimSpace(file.DataPlane.TCPRelay)
	}
	if addr == "" && hb != nil {
		addr = strings.TrimSpace(hb.TCPRelay)
	}

	token := strings.TrimSpace(cliToken)
	if token == "" && file != nil {
		token = strings.TrimSpace(file.DataPlane.TCPRelayToken)
	}
	if token == "" && hb != nil {
		token = hb.TCPRelayToken
	}

	if mode == "udp" {
		addr = ""
	}
	return RelaySettings{Mode: mode, Addr: addr, Token: token}
}
