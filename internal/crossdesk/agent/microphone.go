package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"runtime"
)

// 浏览器麦克风回传：iflygo-mic-v1 上的 16kHz 单声道 s16le PCM。
const (
	micProtocol = "iflygo-mic-v1"
	micRate     = 16000
	micChannels = 1
)

// MicSink 把浏览器麦克风 PCM 写入本机播放设备。
type MicSink interface {
	Start(ctx context.Context) (io.WriteCloser, error)
	Close() error
}

type commandMicSink struct {
	command string
}

func (m *commandMicSink) Start(ctx context.Context) (io.WriteCloser, error) {
	shell, args := audioShell(m.command)
	cmd := exec.CommandContext(ctx, shell, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &micWriter{WriteCloser: stdin, cmd: cmd}, nil
}

func (m *commandMicSink) Close() error { return nil }

type micWriter struct {
	io.WriteCloser
	cmd *exec.Cmd
}

func (w *micWriter) Close() error {
	err := w.WriteCloser.Close()
	_ = w.cmd.Wait()
	return err
}

func newMicSink(custom string) (MicSink, error) {
	if custom != "" {
		return &commandMicSink{command: custom}, nil
	}
	command := defaultMicCommand()
	if command == "" {
		return nil, fmt.Errorf("未找到可用的音频播放命令，请使用 --mic-play-cmd 指定")
	}
	return &commandMicSink{command: command}, nil
}

func defaultMicCommand() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	for _, candidate := range []struct {
		bin  string
		args string
	}{
		{"pacat", "pacat --playback --format=s16le --rate=16000 --channels=1"},
		{"aplay", "aplay -q -f S16_LE -r 16000 -c 1"},
		{"ffplay", "ffplay -hide_banner -loglevel error -f s16le -ar 16000 -ac 1 -nodisp -autoexit -"},
	} {
		if _, err := exec.LookPath(candidate.bin); err == nil {
			return candidate.args
		}
	}
	return ""
}

func (a *Agent) writeMic(pcm []byte) {
	if a.mic == nil || len(pcm) == 0 {
		return
	}
	a.micMu.Lock()
	defer a.micMu.Unlock()
	if a.micWriter == nil {
		writer, err := a.mic.Start(a.micCtx)
		if err != nil {
			a.log.Printf("⚠️ 麦克风播放启动失败: %v", err)
			return
		}
		a.micWriter = writer
		a.log.Printf("🎤 浏览器麦克风播放已启动")
	}
	if _, err := a.micWriter.Write(pcm); err != nil {
		_ = a.micWriter.Close()
		a.micWriter = nil
	}
}

func (a *Agent) closeMic() {
	a.micMu.Lock()
	if a.micWriter != nil {
		_ = a.micWriter.Close()
		a.micWriter = nil
	}
	if a.micCancel != nil {
		a.micCancel()
		a.micCancel = nil
	}
	a.micMu.Unlock()
}

func (a *Agent) onMicFrame(data []byte) {
	if len(data) <= audioHeader {
		return
	}
	if string(data[0:4]) != "CDA1" ||
		binary.LittleEndian.Uint32(data[8:12]) != micRate ||
		binary.LittleEndian.Uint16(data[12:14]) != micChannels ||
		binary.LittleEndian.Uint16(data[14:16]) != audioFormatPCM {
		return
	}
	a.writeMic(data[audioHeader:])
}
