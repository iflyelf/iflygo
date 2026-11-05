package edge

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

// TCPRouter TCP 端口 → 目标 路由表。
type TCPRouter struct {
	mu     sync.RWMutex
	routes map[int]string // public_port -> overlayIP:port
}

// NewTCPRouter 构建 TCP 路由表（仅 Port 路由）。
func NewTCPRouter(routes []Route) *TCPRouter {
	r := &TCPRouter{routes: map[int]string{}}
	for _, rt := range routes {
		if rt.Port > 0 {
			r.routes[rt.Port] = rt.Target
		}
	}
	return r
}

// Set 替换路由表。
func (r *TCPRouter) Set(routes []Route) {
	m := map[int]string{}
	for _, rt := range routes {
		if rt.Port > 0 {
			m[rt.Port] = rt.Target
		}
	}
	r.mu.Lock()
	r.routes = m
	r.mu.Unlock()
}

// Snapshot 返回当前路由快照。
func (r *TCPRouter) Snapshot() map[int]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[int]string, len(r.routes))
	for p, t := range r.routes {
		out[p] = t
	}
	return out
}

// TCPProxy 监听公网端口并转发到 overlay 目标。
type TCPProxy struct {
	router    *TCPRouter
	bindHost  string
	mu        sync.Mutex
	listeners map[int]net.Listener
}

// NewTCPProxy 创建 TCP 转发代理。
func NewTCPProxy(router *TCPRouter, bindHost string) *TCPProxy {
	if bindHost == "" {
		bindHost = "0.0.0.0"
	}
	return &TCPProxy{router: router, bindHost: bindHost, listeners: map[int]net.Listener{}}
}

// Sync 使监听端口与路由表一致（新增/移除）。
func (p *TCPProxy) Sync() error {
	want := p.router.Snapshot()
	p.mu.Lock()
	defer p.mu.Unlock()

	// 移除已删除的端口
	for port, ln := range p.listeners {
		if _, ok := want[port]; !ok {
			ln.Close()
			delete(p.listeners, port)
			log.Printf("🌐 TCP 端口 %d 已停止转发", port)
		}
	}
	// 新增端口
	for port, target := range want {
		if _, ok := p.listeners[port]; ok {
			continue
		}
		addr := fmt.Sprintf("%s:%d", p.bindHost, port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("监听 %s 失败: %w", addr, err)
		}
		p.listeners[port] = ln
		go p.acceptLoop(ln, target)
		log.Printf("🌐 TCP 转发 %s -> %s", addr, target)
	}
	return nil
}

// Close 关闭所有监听。
func (p *TCPProxy) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ln := range p.listeners {
		ln.Close()
	}
	p.listeners = map[int]net.Listener{}
}

func (p *TCPProxy) acceptLoop(ln net.Listener, target string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // 监听已关闭
		}
		go p.forward(conn, target)
	}
}

func (p *TCPProxy) forward(client net.Conn, target string) {
	defer client.Close()
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer upstream.Close()
	go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
	_, _ = io.Copy(client, upstream)
}
