package ipam_test

import (
	"testing"

	"github.com/iflyelf/iflygo/internal/config"
	"github.com/iflyelf/iflygo/internal/ipam"
	"github.com/iflyelf/iflygo/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.SQLitePath = t.TempDir() + "/test.db"
	st, err := store.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return st
}

func newNetwork(t *testing.T, st *store.Store, cidr4 string) int64 {
	t.Helper()
	res, err := st.DB.Exec(st.Rebind(`INSERT INTO networks (name, cidr4) VALUES ($1, $2)`), "testnet", cidr4)
	if err != nil {
		t.Fatalf("insert network: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestAllocateIPv4(t *testing.T) {
	st := newStore(t)
	defer st.Close()
	nid := newNetwork(t, st, "10.88.0.0/24")
	a := ipam.New(st)

	// 网关 .1 被保留，网络/广播 .0/.255 保留，首个可用为 .2
	got, err := a.Allocate(nid, "10.88.0.0/24", "", []string{"10.88.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.88.0.2" {
		t.Fatalf("want 10.88.0.2, got %s", got)
	}

	// 第二个应为 .3
	if got2, _ := a.Allocate(nid, "10.88.0.0/24", "", []string{"10.88.0.1"}); got2 != "10.88.0.3" {
		t.Fatalf("want 10.88.0.3, got %s", got2)
	}

	// 指定分配
	if got3, _ := a.Allocate(nid, "10.88.0.0/24", "10.88.0.50", nil); got3 != "10.88.0.50" {
		t.Fatalf("want 10.88.0.50, got %s", got3)
	}

	// 重复指定应失败
	if _, err := a.Allocate(nid, "10.88.0.0/24", "10.88.0.50", nil); err == nil {
		t.Fatal("expected error for duplicate IP")
	}

	// 网关被保留应失败
	if _, err := a.Allocate(nid, "10.88.0.0/24", "10.88.0.1", []string{"10.88.0.1"}); err == nil {
		t.Fatal("expected error for reserved gateway")
	}
}

func TestAllocateIPv6(t *testing.T) {
	st := newStore(t)
	defer st.Close()
	nid := newNetwork(t, st, "fd88::/64")
	a := ipam.New(st)

	got, err := a.Allocate(nid, "fd88::/64", "", []string{"fd88::1"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "fd88::2" {
		t.Fatalf("want fd88::2, got %s", got)
	}
}
