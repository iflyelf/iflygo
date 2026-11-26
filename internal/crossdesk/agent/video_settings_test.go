package agent

import "testing"

func TestVideoTargets(t *testing.T) {
	cases := []struct {
		quality, fps, preference int
		wantFPS, wantBitrate     int
	}{
		{2, 30, 1, 30, 2000},
		{2, 60, 1, 60, 2000},
		{0, 30, 0, 30, 640},
		{1, 30, 2, 30, 1170},
		{2, 15, 1, 30, 2000},
	}
	for _, c := range cases {
		fps, bitrate := videoTargets(c.quality, c.fps, c.preference, 30, 2000)
		if fps != c.wantFPS || bitrate != c.wantBitrate {
			t.Fatalf("videoTargets(%d,%d,%d)=%d,%d want %d,%d",
				c.quality, c.fps, c.preference, fps, bitrate, c.wantFPS, c.wantBitrate)
		}
	}
}
