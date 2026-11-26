package handler

import (
	"strings"
	"testing"
)

func TestClientInstallScripts(t *testing.T) {
	scripts := buildClientInstallScripts("https://iflygo.example.com", "TOKEN123", "edge-1")
	for name, script := range map[string]string{"linux": scripts.Linux, "darwin": scripts.Darwin, "windows": scripts.Windows} {
		if !strings.Contains(script, "TOKEN123") || !strings.Contains(script, "https://iflygo.example.com") ||
			!strings.Contains(script, "--auto-update") {
			t.Fatalf("%s script missing token/base/auto-update", name)
		}
	}
	if !strings.Contains(scripts.Linux, "iflygo-client enroll") ||
		!strings.Contains(scripts.Linux, "api/client/download?os=linux") ||
		!strings.Contains(scripts.Linux, "service install") {
		t.Fatal("linux script incomplete")
	}
	if !strings.Contains(scripts.Darwin, "launchctl") {
		t.Fatal("darwin script missing launchd")
	}
	if !strings.Contains(scripts.Windows, "Expand-Archive") || !strings.Contains(scripts.Windows, "service install") {
		t.Fatal("windows script incomplete")
	}
}

func TestCrossDeskAgentScript(t *testing.T) {
	linux := buildCrossDeskAgentScript("https://iflygo.example.com", "linux", "amd64", "/srv/share", true, true)
	for _, want := range []string{"crossdesk host", "--file-root=/srv/share", "--audio", "--video-codec vp8", "--auto-update", "systemctl enable --now iflygo-crossdesk"} {
		if !strings.Contains(linux, want) {
			t.Fatalf("linux crossdesk script missing %q", want)
		}
	}
	windows := buildCrossDeskAgentScript("https://iflygo.example.com", "windows", "amd64", `C:\share`, false, false)
	if !strings.Contains(windows, "Register-ScheduledTask") || !strings.Contains(windows, "crossdesk host") {
		t.Fatal("windows crossdesk script incomplete")
	}
	darwin := buildCrossDeskAgentScript("https://iflygo.example.com", "darwin", "arm64", "", false, false)
	if !strings.Contains(darwin, "launchctl") || !strings.Contains(darwin, "crossdesk") {
		t.Fatal("darwin crossdesk script incomplete")
	}
}
