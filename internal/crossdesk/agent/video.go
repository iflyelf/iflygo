package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"golang.org/x/image/draw"
)

// 可选 WebRTC 视频轨道：通过外部 ffmpeg 编码 VP8/H.264，替代 JPEG 帧通道。
// 二进制本身仍不依赖 CGO；未启用或编码器不可用时自动回退到 JPEG。
type videoPipeline struct {
	track   *webrtc.TrackLocalStaticSample
	codec   string
	width   int
	height  int
	fps     int
	bitrate int
	command string
	log     *log.Logger

	ctx    context.Context
	cancel context.CancelFunc
	active atomic.Bool
	closed atomic.Bool

	frames chan []byte
	free   chan []byte
	cmd    *exec.Cmd
	mu     sync.Mutex

	lastPush time.Time
}

func newVideoPipeline(cfg Config, logger *log.Logger) (*videoPipeline, error) {
	codec := strings.ToLower(strings.TrimSpace(cfg.VideoCodec))
	if codec == "" {
		return nil, nil
	}
	if codec != "vp8" && codec != "h264" {
		return nil, fmt.Errorf("不支持的视频编码: %s", codec)
	}
	width, height := 1280, 720
	if size := strings.TrimSpace(cfg.VideoSize); size != "" {
		parts := strings.Split(strings.ToLower(size), "x")
		if len(parts) != 2 {
			return nil, fmt.Errorf("--video-size 需为 WxH")
		}
		w, err1 := strconv.Atoi(parts[0])
		h, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || w < 160 || h < 120 || w > 3840 || h > 2160 {
			return nil, fmt.Errorf("--video-size 无效: %s", size)
		}
		width, height = w, h
	}
	fps := cfg.VideoFPS
	if fps <= 0 {
		fps = 30
	}
	if fps > 60 {
		fps = 60
	}
	bitrate := cfg.VideoBitrate
	if bitrate <= 0 {
		bitrate = 2500
	}
	mime := webrtc.MimeTypeVP8
	if codec == "h264" {
		mime = webrtc.MimeTypeH264
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: mime}, "video", "iflygo")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &videoPipeline{
		track: track, codec: codec, width: width, height: height,
		fps: fps, bitrate: bitrate, command: cfg.VideoEncoder, log: logger,
		ctx: ctx, cancel: cancel,
		frames: make(chan []byte, 2),
		free:   make(chan []byte, 2),
	}
	for i := 0; i < 2; i++ {
		p.free <- make([]byte, width*height*4)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil && p.command == "" {
		cancel()
		return nil, fmt.Errorf("未找到 ffmpeg，无法启用视频轨道")
	}
	return p, nil
}

func (p *videoPipeline) start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed.Load() {
		return fmt.Errorf("视频轨道已关闭")
	}
	shell, args := videoCommand(p.command, p.codec, p.width, p.height, p.fps, p.bitrate)
	cmd := exec.CommandContext(p.ctx, shell, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.active.Store(true)
	go p.writeLoop(stdin)
	go p.readLoop(stdout)
	go func() {
		err := cmd.Wait()
		p.active.Store(false)
		if p.ctx.Err() == nil {
			p.log.Printf("⚠️ 视频编码器退出: %v", err)
		}
	}()
	p.log.Printf("🎬 视频轨道已启用: %s %dx%d@%d %dkbps", p.codec, p.width, p.height, p.fps, p.bitrate)
	return nil
}

func videoCommand(custom, codec string, width, height, fps, bitrate int) (string, []string) {
	if custom != "" {
		return audioShell(custom)
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo", "-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", width, height),
		"-r", strconv.Itoa(fps),
		"-i", "-",
	}
	if codec == "h264" {
		args = append(args,
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
			"-pix_fmt", "yuv420p", "-g", strconv.Itoa(fps),
			"-b:v", fmt.Sprintf("%dk", bitrate), "-x264-params", "aud=1",
			"-f", "h264", "-",
		)
	} else {
		args = append(args,
			"-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8",
			"-g", strconv.Itoa(fps), "-b:v", fmt.Sprintf("%dk", bitrate),
			"-pix_fmt", "yuv420p", "-f", "ivf", "-",
		)
	}
	return "ffmpeg", args
}

func (p *videoPipeline) ready() bool {
	return p != nil && p.active.Load()
}

func (p *videoPipeline) writeLoop(stdin io.WriteCloser) {
	defer stdin.Close()
	for {
		select {
		case <-p.ctx.Done():
			return
		case frame := <-p.frames:
			if _, err := stdin.Write(frame); err != nil {
				p.active.Store(false)
				p.free <- frame
				return
			}
			p.free <- frame
		}
	}
}

func (p *videoPipeline) readLoop(stdout io.Reader) {
	if p.codec == "h264" {
		p.readH264(stdout)
		return
	}
	header := make([]byte, 32)
	if _, err := io.ReadFull(stdout, header); err != nil {
		return
	}
	if string(header[0:4]) != "DKIF" {
		p.log.Printf("⚠️ 视频编码器输出不是 IVF")
		return
	}
	duration := time.Second / time.Duration(p.fps)
	for {
		frameHeader := make([]byte, 12)
		if _, err := io.ReadFull(stdout, frameHeader); err != nil {
			return
		}
		size := binary.LittleEndian.Uint32(frameHeader[0:4])
		if size == 0 || size > 16*1024*1024 {
			return
		}
		data := make([]byte, size)
		if _, err := io.ReadFull(stdout, data); err != nil {
			return
		}
		if err := p.track.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
			return
		}
	}
}

// readH264 按 Access Unit Delimiter 切分 Annex-B 码流，每个访问单元写一个样本。
func (p *videoPipeline) readH264(stdout io.Reader) {
	buf := make([]byte, 0, 256*1024)
	tmp := make([]byte, 64*1024)
	var current []byte
	duration := time.Second / time.Duration(p.fps)
	flush := func() {
		if len(current) == 0 {
			return
		}
		if err := p.track.WriteSample(media.Sample{Data: current, Duration: duration}); err != nil {
			current = nil
			return
		}
		current = nil
	}
	consume := func(nalus [][]byte) {
		for _, nalu := range nalus {
			offset := 3
			if len(nalu) > 3 && nalu[2] == 0 {
				offset = 4
			}
			if offset >= len(nalu) {
				continue
			}
			naluType := nalu[offset] & 0x1f
			if naluType == 9 && len(current) > 0 {
				flush()
			}
			current = append(current, nalu...)
		}
	}
	for {
		n, err := stdout.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			nalus, rest := splitNALUs(buf)
			if len(nalus) > 0 {
				consume(nalus)
				buf = append(buf[:0], rest...)
			}
		}
		if err != nil {
			if len(buf) > 0 {
				consume([][]byte{buf})
			}
			flush()
			return
		}
	}
}

// splitNALUs 返回完整的 NALU（含起始码）与尚未结束的尾部。
func splitNALUs(buf []byte) (nalus [][]byte, rest []byte) {
	var starts []int
	for i := 0; i+3 < len(buf); i++ {
		if buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 1 {
			starts = append(starts, i)
			i += 2
			continue
		}
		if i+3 < len(buf) && buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 0 && buf[i+3] == 1 {
			starts = append(starts, i)
			i += 3
		}
	}
	if len(starts) < 2 {
		if len(starts) == 1 && starts[0] > 0 {
			return nil, buf[starts[0]:]
		}
		return nil, buf
	}
	for i := 0; i+1 < len(starts); i++ {
		nalus = append(nalus, buf[starts[i]:starts[i+1]])
	}
	rest = buf[starts[len(starts)-1]:]
	return nalus, rest
}

// push 缩放并投递一帧；仅在 captureLoop 中调用。返回 false 表示丢弃。
func (p *videoPipeline) push(src image.Image) bool {
	if !p.ready() || p.closed.Load() {
		return false
	}
	interval := time.Second / time.Duration(p.fps)
	if !p.lastPush.IsZero() && time.Since(p.lastPush) < interval {
		return true
	}
	p.lastPush = time.Now()

	var buf []byte
	select {
	case buf = <-p.free:
	default:
		return false
	}
	dst := &image.RGBA{Pix: buf, Stride: p.width * 4, Rect: image.Rect(0, 0, p.width, p.height)}
	for i := 0; i < len(buf); i += 4 {
		buf[i], buf[i+1], buf[i+2], buf[i+3] = 0, 0, 0, 255
	}
	sb := src.Bounds()
	scale := float64(p.width) / float64(sb.Dx())
	if h := float64(sb.Dy()) * scale; h > float64(p.height) {
		scale = float64(p.height) / float64(sb.Dy())
	}
	w := int(float64(sb.Dx()) * scale)
	h := int(float64(sb.Dy()) * scale)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	rect := image.Rect((p.width-w)/2, (p.height-h)/2, (p.width-w)/2+w, (p.height-h)/2+h)
	draw.ApproxBiLinear.Scale(dst, rect, src, sb, draw.Src, nil)
	select {
	case p.frames <- buf:
		return true
	default:
		p.free <- buf
		return false
	}
}

func (p *videoPipeline) close() {
	p.closed.Store(true)
	p.cancel()
	p.active.Store(false)
}
