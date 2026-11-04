// Package client 实现 iflygo-client 的核心能力。
package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State 客户端持久化状态。
type State struct {
	Server    string `json:"server"`
	ClientID  int64  `json:"client_id"`
	Secret    string `json:"secret"`
	Name      string `json:"name"`
	Network   string `json:"network"`
	IPv4      string `json:"ipv4"`
	IPv6      string `json:"ipv6"`
	ConfigRev int64  `json:"config_rev"`
}

// DefaultStateDir 默认状态目录。
func DefaultStateDir() string {
	if d := os.Getenv("IFLYGO_STATE_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".iflygo"
	}
	return filepath.Join(home, ".iflygo")
}

// Save 保存状态到 state.json（0600）。
func (s *State) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), b, 0o600)
}

// LoadState 读取状态。
func LoadState(dir string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("解析状态文件: %w", err)
	}
	return &s, nil
}
