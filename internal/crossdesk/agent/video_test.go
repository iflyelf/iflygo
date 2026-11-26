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
	_, args := videoCommand("", "vp8", 1280, 720, 30, 2000)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-pix_fmt yuv420p") {
		t.Fatalf("vp8 command missing yuv420p: %s", joined)
	}
	if !strings.Contains(joined, "-f ivf -") {
		t.Fatalf("vp8 command missing ivf: %s", joined)
	}
	_, h264 := videoCommand("", "h264", 1280, 720, 30, 2000)
	if !strings.Contains(strings.Join(h264, " "), "aud=1") {
		t.Fatal("h264 command missing aud=1")
	}
}
