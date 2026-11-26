package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

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
	audioProtocol = "iflygo-audio-v1"
	audioHeader   = 16
	audioRate     = 48000
	audioChannels = 2
	audioChunk    = audioRate / 50 * audioChannels * 2 // 20ms s16le
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

// audioFrame 组装一帧音频。
func audioFrame(seq uint32, format uint16, payload []byte) []byte {
	frame := make([]byte, audioHeader+len(payload))
	copy(frame[0:4], audioMagic[:])
	binary.LittleEndian.PutUint32(frame[4:8], seq)
	binary.LittleEndian.PutUint32(frame[8:12], audioRate)
	binary.LittleEndian.PutUint16(frame[12:14], audioChannels)
	binary.LittleEndian.PutUint16(frame[14:16], format)
	copy(frame[audioHeader:], payload)
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
	codec := strings.ToLower(strings.TrimSpace(a.cfg.AudioCodec))
	if codec != "pcm" {
		codec = "adpcm"
	}
	encoder := &adpcmEncoder{}
	reset := true
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
		format := uint16(audioFormatPCM)
		payload := buf
		if codec == "adpcm" {
			// 每 250ms 重置一次编码状态，便于新客户端在丢帧后重新同步。
			if reset || seq%12 == 1 {
				encoder.reset()
				format = audioFormatADPCMReset
			} else {
				format = audioFormatADPCM
			}
			reset = false
			payload = encoder.encode(buf)
		}
		if !a.broadcastAudio(audioFrame(seq, format, payload)) && codec == "adpcm" {
			reset = true
		}
	}
}

func (a *Agent) broadcastAudio(frame []byte) bool {
	a.mu.Lock()
	list := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		list = append(list, s)
	}
	a.mu.Unlock()
	ok := true
	for _, s := range list {
		if !s.sendAudio(frame) {
			ok = false
		}
	}
	return ok
}

func (s *session) sendAudio(frame []byte) bool {
	dc := s.audioChannel
	if dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return true
	}
	if dc.BufferedAmount() > 256*1024 {
		return false
	}
	return dc.Send(frame) == nil
}
