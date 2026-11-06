package ddnsto

import "testing"

// TestSlug 验证客户端名 → 子域名片段转换。
func TestSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"My NAS", "my-nas"},
		{"home-nas", "home-nas"},
		{"  NAS  ", "nas"},
		{"", "host"},
		{"!!!", "host"},  // 全部非法字符 → 空 → host
		{"中文主机", "host"}, // 非 ASCII → 空 → host
		{"a.b.c", "a-b-c"},
		{"NAS_01", "nas-01"},
	}
	for _, c := range cases {
		got := slug(c.in)
		if got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRandSuffix 验证随机后缀长度与字符集。
func TestRandSuffix(t *testing.T) {
	s := randSuffix(6)
	if len(s) != 6 {
		t.Fatalf("randSuffix(6) 长度 = %d, want 6", len(s))
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("randSuffix 含非法字符 %q", c)
		}
	}
}

// TestDomain 验证根域名（默认 + 环境变量）。
func TestDomain(t *testing.T) {
	// 默认（无环境变量）
	d := domain()
	if d == "" {
		t.Error("domain() 返回空")
	}
	// 显式设置
	t.Setenv("DDNSTO_DOMAIN", "ddnsto.example.com.")
	d = domain()
	if d != "ddnsto.example.com" {
		t.Errorf("domain() = %q, want ddnsto.example.com", d)
	}
}

// TestNowStr 验证时间格式。
func TestNowStr(t *testing.T) {
	s := nowStr()
	if len(s) != len("2006-01-02 15:04:05") {
		t.Errorf("nowStr() = %q, 长度不符", s)
	}
}
