//go:build linux && !android

package agent

import "os/exec"

// defaultAudioCommand 依次尝试 PulseAudio / PipeWire 兼容的 parec 与 ffmpeg。
func defaultAudioCommand() string {
	for _, candidate := range []struct {
		bin  string
		args string
	}{
		{"parec", "parec --raw --format=s16le --rate=48000 --channels=2 --latency-msec=20"},
		{"ffmpeg", "ffmpeg -hide_banner -loglevel error -f pulse -i default -f s16le -ar 48000 -ac 2 -"},
	} {
		if _, err := exec.LookPath(candidate.bin); err == nil {
			return candidate.args
		}
	}
	return ""
}
