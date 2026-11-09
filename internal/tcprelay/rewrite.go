package tcprelay

import (
	"context"
	"fmt"
	"net"

	"gopkg.in/yaml.v3"
)

// RewriteConfig 解析 Nebula 配置，把下列真实上游 endpoint 改写为本地 TCP 桥的
// UDP 端口（127.0.0.1:<port>）：
//
//   - static_host_map：lighthouse 等静态节点的公网 host:port；
//   - relay.relays：显式配置的 relay 公网 host:port。
//
// 这样 Nebula 仍以 UDP 发送，但实际数据报进入本地桥，桥通过 TCP 连到中继服务端，
// 服务端再以 UDP 转发到真实上游 —— 实现「UDP 被封锁时走 TCP 中继」。
//
// 返回改写后的 YAML。所有桥在 ctx 取消时自动关闭。
func RewriteConfig(ctx context.Context, relayServer, token string, cfg []byte) ([]byte, error) {
	var root map[string]any
	if err := yaml.Unmarshal(cfg, &root); err != nil {
		return nil, fmt.Errorf("解析 Nebula 配置失败: %w", err)
	}

	changed := false

	// 1) static_host_map: { overlayIP: [host:port, ...] }
	if raw, ok := root["static_host_map"]; ok && raw != nil {
		hostMap, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("static_host_map 类型非法: %T", raw)
		}
		for vpnIP, endpoints := range hostMap {
			list, err := toStrings(endpoints)
			if err != nil {
				return nil, fmt.Errorf("static_host_map[%s]: %w", vpnIP, err)
			}
			rewritten, err := startBridges(ctx, relayServer, token, list)
			if err != nil {
				return nil, fmt.Errorf("static_host_map[%s]: %w", vpnIP, err)
			}
			hostMap[vpnIP] = rewritten
			changed = true
		}
		root["static_host_map"] = hostMap
	}

	// 2) relay.relays: [host:port, ...]
	if raw, ok := root["relay"]; ok && raw != nil {
		relay, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("relay 类型非法: %T", raw)
		}
		if eps, ok := relay["relays"]; ok && eps != nil {
			list, err := toStrings(eps)
			if err != nil {
				return nil, fmt.Errorf("relay.relays: %w", err)
			}
			rewritten, err := startBridges(ctx, relayServer, token, list)
			if err != nil {
				return nil, fmt.Errorf("relay.relays: %w", err)
			}
			relay["relays"] = rewritten
			root["relay"] = relay
			changed = true
		}
	}

	if !changed {
		return cfg, nil // 无需要改写的上游
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("序列化 Nebula 配置失败: %w", err)
	}
	return out, nil
}

// startBridges 为每个可解析的 "host:port" endpoint 启动一个本地桥，
// 返回改写后的本地 UDP 地址列表。无法解析为 host:port 的条目（如纯 overlay IP）
// 原样保留，避免破坏配置。
func startBridges(ctx context.Context, relayServer, token string, endpoints []string) ([]any, error) {
	out := make([]any, 0, len(endpoints))
	for _, ep := range endpoints {
		if _, _, err := net.SplitHostPort(ep); err != nil {
			out = append(out, ep) // 非 host:port（如 overlay IP），保持原样
			continue
		}
		bridge, err := StartBridge(ctx, relayServer, token, ep)
		if err != nil {
			return nil, fmt.Errorf("启动 TCP 桥失败 (%s): %w", ep, err)
		}
		out = append(out, bridge.LocalAddr().String())
	}
	return out, nil
}

// toStrings 把 YAML 解析出的 endpoint 列表统一成 []string。
func toStrings(v any) ([]string, error) {
	switch list := v.(type) {
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("endpoint 非字符串: %T", item)
			}
			out = append(out, s)
		}
		return out, nil
	case []string:
		return list, nil
	case string:
		return []string{list}, nil
	default:
		return nil, fmt.Errorf("endpoint 列表类型非法: %T", v)
	}
}
