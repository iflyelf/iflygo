// Package ipam 提供 overlay IPv4/IPv6 地址分配。
package ipam

import (
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/iflyelf/iflygo/internal/store"
)

// Allocator 基于数据库唯一约束做并发安全的地址分配。
type Allocator struct {
	s *store.Store
}

// New 创建分配器。
func New(s *store.Store) *Allocator { return &Allocator{s: s} }

// Allocation 一条地址分配记录。
type Allocation struct {
	IP     string `json:"ip"`
	Family int    `json:"family"`
	NodeID int64  `json:"node_id,omitempty"`
	State  string `json:"state"`
}

// Allocate 分配一个地址。requested 非空则按指定分配，否则自动分配。
// exclude 用于保留网关等地址。
func (a *Allocator) Allocate(networkID int64, cidr, requested string, exclude []string) (string, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", fmt.Errorf("CIDR 非法: %w", err)
	}
	prefix = prefix.Masked()

	reserved := map[string]bool{}
	for _, e := range exclude {
		if e != "" {
			reserved[e] = true
		}
	}
	reserved[prefix.Addr().String()] = true
	if prefix.Addr().Is4() && prefix.Bits() < 32 {
		reserved[lastIPv4(prefix).String()] = true
	}
	for _, ip := range a.allocated(networkID) {
		reserved[ip] = true
	}

	if requested != "" {
		ip, err := netip.ParseAddr(requested)
		if err != nil {
			return "", fmt.Errorf("IP 非法: %s", requested)
		}
		if !prefix.Contains(ip) {
			return "", fmt.Errorf("IP %s 不在网段 %s 内", requested, cidr)
		}
		if reserved[requested] {
			return "", fmt.Errorf("IP %s 已被占用或保留", requested)
		}
		if err := a.insert(networkID, requested, familyOf(ip)); err != nil {
			return "", err
		}
		return requested, nil
	}

	for ip := prefix.Addr().Next(); prefix.Contains(ip); ip = ip.Next() {
		if reserved[ip.String()] {
			continue
		}
		if err := a.insert(networkID, ip.String(), familyOf(ip)); err == nil {
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("网段 %s 地址已耗尽", cidr)
}

// insert 原子插入分配记录，冲突返回错误。
func (a *Allocator) insert(networkID int64, ip string, family int) error {
	q := a.s.Rebind(`INSERT INTO ip_allocations (network_id, ip, family, state, created_at)
		VALUES ($1, $2, $3, 'allocated', $4)
		ON CONFLICT (network_id, ip) DO NOTHING
		RETURNING id`)
	var id int64
	now := time.Now().UTC().Format(time.RFC3339)
	if err := a.s.DB.QueryRow(q, networkID, ip, family, now).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("地址 %s 已被占用", ip)
		}
		return err
	}
	return nil
}

// BindNode 将已分配的地址绑定到节点。
func (a *Allocator) BindNode(networkID int64, ip string, nodeID int64) error {
	q := a.s.Rebind(`UPDATE ip_allocations SET node_id = $1 WHERE network_id = $2 AND ip = $3`)
	_, err := a.s.DB.Exec(q, nodeID, networkID, ip)
	return err
}

// ReleaseIP 释放单个地址。
func (a *Allocator) ReleaseIP(networkID int64, ip string) error {
	q := a.s.Rebind(`DELETE FROM ip_allocations WHERE network_id = $1 AND ip = $2`)
	_, err := a.s.DB.Exec(q, networkID, ip)
	return err
}

// ReleaseNode 释放节点占用的所有地址。
func (a *Allocator) ReleaseNode(nodeID int64) error {
	q := a.s.Rebind(`DELETE FROM ip_allocations WHERE node_id = $1`)
	_, err := a.s.DB.Exec(q, nodeID)
	return err
}

// List 返回网络已分配的地址。
func (a *Allocator) List(networkID int64) ([]Allocation, error) {
	q := a.s.Rebind(`SELECT ip, family, COALESCE(node_id,0), state FROM ip_allocations WHERE network_id = $1 ORDER BY ip`)
	rows, err := a.s.DB.Query(q, networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Allocation
	for rows.Next() {
		var al Allocation
		if err := rows.Scan(&al.IP, &al.Family, &al.NodeID, &al.State); err != nil {
			return nil, err
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

func (a *Allocator) allocated(networkID int64) []string {
	q := a.s.Rebind(`SELECT ip FROM ip_allocations WHERE network_id = $1`)
	rows, err := a.s.DB.Query(q, networkID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ip string
		if rows.Scan(&ip) == nil {
			out = append(out, ip)
		}
	}
	return out
}

func familyOf(ip netip.Addr) int {
	if ip.Is4() {
		return 4
	}
	return 6
}

func lastIPv4(p netip.Prefix) netip.Addr {
	a := p.Addr().As4()
	var v uint32 = uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3])
	hostBits := uint32(32 - p.Bits())
	v += (1 << hostBits) - 1
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}
