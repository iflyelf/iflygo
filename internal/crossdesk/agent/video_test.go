package agent

import (
	"strings"
	"testing"
)

func TestSplitNALUs(t *testing.T) {
	// 4 字节起始码 + IDR，3 字节起始码 + SPS，3 字节起始码 + PPS，尾随起始码 + 数据
	buf := []byte{
		0, 0, 0, 1, 0x65, 0x11,
		0, 0, 1, 0x67, 0x22,
		0, 0, 1, 0x68, 0x33,
		0, 0, 1, 0x65, 0x44, 0x55,
	}
	nalus, rest := splitNALUs(buf)
	if len(nalus) != 3 {
		t.Fatalf("expected 3 complete NALUs, got %d", len(nalus))
	}
	if len(rest) == 0 || rest[3] != 0x65 {
		t.Fatalf("unexpected rest: %x", rest)
	}
	if len(nalus[0]) != 6 || nalus[0][4] != 0x65 {
		t.Fatalf("bad first NALU: %x", nalus[0])
	}
}

func TestVideoCommandForcesYUV420P(t *testing.T) {
	_, args := videoCommand("", "vp8", 1280, 720, 30, 2000, "none", "/dev/dri/renderD128")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-pix_fmt yuv420p") {
		t.Fatalf("vp8 command missing yuv420p: %s", joined)
	}
	if !strings.Contains(joined, "-f ivf -") {
		t.Fatalf("vp8 command missing ivf: %s", joined)
	}
	_, h264 := videoCommand("", "h264", 1280, 720, 30, 2000, "none", "/dev/dri/renderD128")
	if !strings.Contains(strings.Join(h264, " "), "aud=1") {
		t.Fatal("h264 command missing aud=1")
	}
}

func TestVideoHardwareCommands(t *testing.T) {
	_, nvenc := videoCommand("", "h264", 1280, 720, 30, 2000, "nvenc", "")
	if !strings.Contains(strings.Join(nvenc, " "), "h264_nvenc") {
		t.Fatal("nvenc command missing h264_nvenc")
	}
	_, vaapi := videoCommand("", "h264", 1280, 720, 30, 2000, "vaapi", "/dev/dri/renderD128")
	joined := strings.Join(vaapi, " ")
	if !strings.Contains(joined, "h264_vaapi") || !strings.Contains(joined, "/dev/dri/renderD128") {
		t.Fatalf("vaapi command invalid: %s", joined)
	}
	_, vp8 := videoCommand("", "vp8", 1280, 720, 30, 2000, "nvenc", "")
	if !strings.Contains(strings.Join(vp8, " "), "libvpx") {
		t.Fatal("vp8 should stay on libvpx")
	}
}

func TestPickHWAccel(t *testing.T) {
	available := map[string]bool{"h264_nvenc": true}
	if got := pickHWAccel("auto", available, "linux"); got != "nvenc" {
		t.Fatalf("auto = %s", got)
	}
	if got := pickHWAccel("vaapi", available, "linux"); got != "none" {
		t.Fatalf("vaapi = %s", got)
	}
	if got := pickHWAccel("nvenc", available, "windows"); got != "nvenc" {
		t.Fatalf("nvenc = %s", got)
	}
	if got := pickHWAccel("none", available, "linux"); got != "none" {
		t.Fatalf("none = %s", got)
	}
}
