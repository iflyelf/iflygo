package agent

import (
	"bytes"
	"testing"
)

func TestClipFrameRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte("剪贴板"), 1000)
	frame := clipFrame(clipKindText, 7, uint32(len(payload)), 1024, payload)
	kind, id, total, offset, ok := parseClipFrame(frame)
	if !ok || kind != clipKindText || id != 7 || total != uint32(len(payload)) || offset != 1024 {
		t.Fatalf("unexpected frame: kind=%d id=%d total=%d offset=%d ok=%v", kind, id, total, offset, ok)
	}
	if !bytes.Equal(frame[clipHeaderSize:], payload) {
		t.Fatal("payload mismatch")
	}
}

func TestParseClipFrameRejectsBadSize(t *testing.T) {
	if _, _, _, _, ok := parseClipFrame(nil); ok {
		t.Fatal("empty frame accepted")
	}
	if _, _, _, _, ok := parseClipFrame(make([]byte, clipHeaderSize+clipChunkSize+1)); ok {
		t.Fatal("oversized frame accepted")
	}
}
