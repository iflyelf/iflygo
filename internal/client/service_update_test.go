package client

import (
	"strings"
	"testing"
)

func TestSystemdUnitAutoUpdate(t *testing.T) {
	unit := SystemdUnitWithAutoUpdate("/usr/local/bin/iflygo-client", "/var/lib/iflygo-client", "", "", "", true)
	if !strings.Contains(unit, "--auto-update") || !strings.Contains(unit, "Restart=always") {
		t.Fatalf("unit missing auto-update/restart: %s", unit)
	}
}
