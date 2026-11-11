package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientOverrideValueScan(t *testing.T) {
	var empty ClientOverride
	if err := empty.Scan(""); err != nil {
		t.Fatalf("Scan empty: %v", err)
	}
	if empty != (ClientOverride{}) {
		t.Fatalf("空值应解析为零值，得到 %+v", empty)
	}

	on := true
	src := ClientOverride{
		UseServerDNS:  &on,
		DNS:           "10.77.0.53",
		MTU:           1360,
		Interval:      "45s",
		DataPlaneMode: "tcp",
	}
	v, err := src.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	var got ClientOverride
	if err := got.Scan(v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.DNS != src.DNS || got.MTU != src.MTU || got.Interval != src.Interval ||
		got.DataPlaneMode != src.DataPlaneMode || got.UseServerDNS == nil || !*got.UseServerDNS {
		t.Fatalf("往返不一致: %+v", got)
	}
}

func TestClientOverrideJSONInherit(t *testing.T) {
	// 未设置的字段不应写入 JSON，便于「跟随默认」不产生覆盖。
	b, err := json.Marshal(ClientOverride{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != "{}" {
		t.Fatalf("零值应序列化为 {}，得到 %s", b)
	}
}
