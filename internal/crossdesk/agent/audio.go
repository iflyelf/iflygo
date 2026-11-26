package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"github.com/pion/webrtc/v4"
)

// 音频帧协议（iflygo-audio-v1，无序 DataChannel）：
//
//	0..3   magic "CDA1"
//	4..7   sequence
//	8..11  sample rate（48000）
//	12..13 channels（2）
//	14..15 format（1 = s16le）
//	16..   PCM 数据
const (
	audioProtocol  = "iflygo-audio-v1"
	audioHeader    = 16
	audioRate      = 48000
	audioChannels  = 2
	audioFormatS16 = 1
	audioChunk     = audioRate / 50 * audioChannels * 2 // 20ms
)

var audioMagic = [4]byte{'C', 'D', 'A', '1'}

// AudioSource 启动一个输出 48kHz 双声道 s16le PCM 的采集源。
type AudioSource interface {
	Start(ctx context.Context) (io.ReadCloser, error)
	Close() error
}

type commandAudio struct {
	command string
}

func (c *commandAudio) Start(ctx context.Context) (io.ReadCloser, error) {
	shell, args := audioShell(c.command)
	cmd := exec.CommandContext(ctx, shell, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &commandAudioReader{ReadCloser: stdout, cmd: cmd}, nil
}

func (c *commandAudio) Close() error { return nil }

type commandAudioReader struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (r *commandAudioReader) Close() error {
	err := r.ReadCloser.Close()
	_ = r.cmd.Process.Kill()
	_ = r.cmd.Wait()
	return err
}

func audioShell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}

func newAudioSource(custom string) (AudioSource, error) {
	if custom != "" {
		return &commandAudio{command: custom}, nil
	}
	command := defaultAudioCommand()
	if command == "" {
		return nil, fmt.Errorf("未找到可用的音频采集命令，请使用 --audio-cmd 指定")
	}
	return &commandAudio{command: command}, nil
}

// audioFrame 组装一帧 PCM。
func audioFrame(seq uint32, pcm []byte) []byte {
	frame := make([]byte, audioHeader+len(pcm))
	copy(frame[0:4], audioMagic[:])
	binary.LittleEndian.PutUint32(frame[4:8], seq)
	binary.LittleEndian.PutUint32(frame[8:12], audioRate)
	binary.LittleEndian.PutUint16(frame[12:14], audioChannels)
	binary.LittleEndian.PutUint16(frame[14:16], audioFormatS16)
	copy(frame[audioHeader:], pcm)
	return frame
}

func (a *Agent) refreshAudio() {
	if a.audio == nil {
		return
	}
	wanted := 0
	a.mu.Lock()
	for _, s := range a.sessions {
		if s.audioOn {
			wanted++
		}
	}
	a.mu.Unlock()

	a.audioMu.Lock()
	defer a.audioMu.Unlock()
	if wanted > 0 && a.audioStop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		a.audioStop = cancel
		go a.audioLoop(ctx)
	}
	if wanted == 0 && a.audioStop != nil {
		a.audioStop()
		a.audioStop = nil
	}
}

func (a *Agent) stopAudio() {
	a.audioMu.Lock()
	if a.audioStop != nil {
		a.audioStop()
		a.audioStop = nil
	}
	a.audioMu.Unlock()
}

func (a *Agent) audioLoop(ctx context.Context) {
	reader, err := a.audio.Start(ctx)
	if err != nil {
		a.log.Printf("⚠️ 音频采集启动失败: %v", err)
		return
	}
	defer reader.Close()
	a.log.Printf("🎙️ 音频采集已启动")
	defer a.log.Printf("🎙️ 音频采集已停止")
	buf := make([]byte, audioChunk)
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, err := io.ReadFull(reader, buf); err != nil {
			if ctx.Err() == nil {
				a.log.Printf("⚠️ 音频采集结束: %v", err)
			}
			return
		}
		seq++
		a.broadcastAudio(audioFrame(seq, buf))
	}
}

func (a *Agent) broadcastAudio(frame []byte) {
	a.mu.Lock()
	list := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		list = append(list, s)
	}
	a.mu.Unlock()
	for _, s := range list {
		s.sendAudio(frame)
	}
}

func (s *session) sendAudio(frame []byte) {
	dc := s.audioChannel
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	if dc.BufferedAmount() > 256*1024 {
		return
	}
	_ = dc.Send(frame)
}
