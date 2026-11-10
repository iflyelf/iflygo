package main

import "testing"

func TestIsDataPlaneOnly(t *testing.T) {
	cases := []struct {
		role    string
		overlay string
		want    bool
	}{
		{"console", "/etc/iflygo/node/config.yml", false},
		{"console,lighthouse", "/etc/iflygo/node/config.yml", false},
		{"lighthouse", "/etc/iflygo/node/config.yml", true},
		{"lighthouse,relay", "/etc/iflygo/node/config.yml", true},
		{"edge", "/etc/iflygo/node/config.yml", true},
		{"lighthouse", "", false},
		{"console", "", false},
	}
	for _, c := range cases {
		if got := isDataPlaneOnly(c.role, c.overlay); got != c.want {
			t.Fatalf("isDataPlaneOnly(%q,%q)=%v want %v", c.role, c.overlay, got, c.want)
		}
	}
}
