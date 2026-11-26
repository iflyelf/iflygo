package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iflyelf/iflygo/internal/client"
	"github.com/iflyelf/iflygo/internal/crossdesk/agent"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type crossDeskCLI struct {
	base   string
	client *http.Client
}

func newCrossDeskCLI(server, user, pass string, insecure bool) (*crossDeskCLI, error) {
	base := strings.TrimRight(strings.TrimSpace(server), "/")
	if base == "" {
		return nil, fmt.Errorf("--server 必填")
	}
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 用户显式选择
	}
	c := &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: tr}
	if user != "" || pass != "" {
		body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
		req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("登录失败: %s", strings.TrimSpace(string(b)))
		}
	}
	return &crossDeskCLI{base: base, client: c}, nil
}

func (c *crossDeskCLI) get(path string) (any, error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	return c.do(req)
}

func (c *crossDeskCLI) post(path, body string) (any, error) {
	req, _ := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *crossDeskCLI) do(req *http.Request) (any, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("%s", env.Message)
	}
	var out any
	_ = json.Unmarshal(env.Data, &out)
	return out, nil
}

func cmdCrossDesk(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: iflygo-client crossdesk <status|devices|connect|deskhub|host> [flags]")
		os.Exit(2)
	}
	action := args[0]
	fs := flag.NewFlagSet("crossdesk "+action, flag.ExitOnError)
	server := fs.String("server", "", "控制面地址，如 https://iflygo.example.com")
	user := fs.String("username", "", "登录用户名（管理员/授权用户）")
	pass := fs.String("password", "", "登录密码")
	insecure := fs.Bool("insecure", false, "跳过 TLS 证书校验（仅测试）")
	device := fs.String("device", "", "CrossDesk 设备 ID（connect/host）")
	devices := fs.String("devices", "", "逗号分隔设备 ID（deskhub）")
	fps := fs.Int("fps", 10, "画面帧率（host）")
	quality := fs.Int("quality", 65, "JPEG 质量 1-100（host）")
	maxWidth := fs.Int("max-width", 1600, "画面最大宽度，0 表示原始尺寸（host）")
	stun := fs.String("stun", "", "额外 STUN 服务器，逗号分隔（host）")
	credentials := fs.String("credentials", "", "设备凭据保存路径（host，默认 ~/.config/iflygo/crossdesk-device.json）")
	fileRoots := &stringList{}
	fs.Var(fileRoots, "file-root", "允许文件浏览/传输的目录（可重复，host）")
	fileMaxUpload := fs.Int64("file-max-upload", 0, "单文件上传上限（MB，0=不限，host）")
	audio := fs.Bool("audio", false, "启用音频采集（host）")
	audioCmd := fs.String("audio-cmd", "", "音频采集命令，输出 48kHz 双声道 s16le PCM（host）")
	audioCodec := fs.String("audio-codec", "adpcm", "音频编码：adpcm（约 384kbps）或 pcm（1.5Mbps）")
	videoCodec := fs.String("video-codec", "", "WebRTC 视频编码：vp8 / h264（需要 ffmpeg；默认 JPEG 帧）")
	videoSize := fs.String("video-size", "1280x720", "视频轨道分辨率")
	videoFPS := fs.Int("video-fps", 30, "视频轨道帧率")
	videoBitrate := fs.Int("video-bitrate", 2500, "视频轨道码率（kbps）")
	videoEncoder := fs.String("video-encoder", "", "自定义编码命令（stdin=RGBA，stdout=IVF/H264）")
	captureCmd := fs.String("capture-cmd", "", "外部抓屏命令，输出 PNG/JPEG（如 Wayland: grim -t png -）")
	videoHW := fs.String("video-hwaccel", "auto", "视频硬件编码：auto/none/nvenc/qsv/vaapi/videotoolbox（仅 H.264）")
	autoUpdate := fs.Bool("auto-update", false, "自动升级 iflygo-client")
	updateInterval := fs.Duration("update-interval", 6*time.Hour, "自动升级检查间隔")
	mic := fs.Bool("mic", false, "允许浏览器麦克风回传到本机播放（host）")
	micPlay := fs.String("mic-play-cmd", "", "麦克风播放命令，stdin=16kHz 单声道 s16le（host）")
	vaapiDevice := fs.String("vaapi-device", "/dev/dri/renderD128", "VAAPI 设备路径")
	fs.Parse(args[1:])

	if action == "host" {
		cmdCrossDeskHost(*server, *device, *pass, *insecure, *fps, *quality, *maxWidth, *stun, *credentials, *fileRoots, *fileMaxUpload, *audio, *audioCmd, *audioCodec,
			*videoCodec, *videoSize, *videoFPS, *videoBitrate, *videoEncoder, *captureCmd,
			*videoHW, *vaapiDevice, *mic, *micPlay, *autoUpdate, *updateInterval)
		return
	}

	cli, err := newCrossDeskCLI(*server, *user, *pass, *insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	switch action {
	case "status":
		data, err := cli.get("/api/crossdesk/status")
		printResult(data, err)
	case "devices":
		data, err := cli.get("/api/crossdesk/devices")
		printResult(data, err)
	case "connect":
		if strings.TrimSpace(*device) == "" {
			fmt.Fprintln(os.Stderr, "❌ --device 必填")
			os.Exit(2)
		}
		data, err := cli.post("/api/crossdesk/devices/"+url.PathEscape(*device)+"/join-ticket", "{}")
		printResult(data, err)
	case "deskhub":
		ids := splitComma(*devices)
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "❌ --devices 必填")
			os.Exit(2)
		}
		body, _ := json.Marshal(map[string]any{"device_ids": ids})
		data, err := cli.post("/api/crossdesk/deskhub/tickets", string(body))
		printResult(data, err)
	default:
		fmt.Fprintf(os.Stderr, "❌ 未知子命令 %s\n", action)
		os.Exit(2)
	}
}

func cmdCrossDeskHost(server, device, password string, insecure bool, fps, quality, maxWidth int, stun, credentials string, fileRoots []string, fileMaxUpload int64, audio bool, audioCmd, audioCodec string,
	videoCodec, videoSize string, videoFPS, videoBitrate int, videoEncoder, captureCmd, videoHW, vaapiDevice string,
	mic bool, micPlay string, autoUpdate bool, updateInterval time.Duration) {
	if strings.TrimSpace(server) == "" {
		fmt.Fprintln(os.Stderr, "❌ --server 必填")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := agent.New(agent.Config{
		ServerURL: strings.TrimSpace(server), DeviceID: strings.TrimSpace(device), Password: password,
		FPS: fps, Quality: quality, MaxWidth: maxWidth, STUN: splitComma(stun),
		Insecure: insecure, CredentialsPath: credentials, FileRoots: fileRoots,
		FileMaxUpload: fileMaxUpload * 1024 * 1024,
		Audio:         audio, AudioCommand: audioCmd, AudioCodec: audioCodec,
		VideoCodec: videoCodec, VideoSize: videoSize, VideoFPS: videoFPS,
		VideoBitrate: videoBitrate, VideoEncoder: videoEncoder, CaptureCommand: captureCmd,
		VideoHWAccel: videoHW, VAAPIDevice: vaapiDevice,
		Mic: mic, MicPlayCommand: micPlay,
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	})
	if autoUpdate {
		backupDir := credentials
		if backupDir == "" {
			if home, err := os.UserHomeDir(); err == nil {
				backupDir = filepath.Join(home, ".config", "iflygo")
			}
		} else {
			backupDir = filepath.Dir(backupDir)
		}
		go client.AutoUpdateLoop(ctx, strings.TrimSpace(server), backupDir, updateInterval, log.Default())
	}
	if err := a.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	fmt.Println("👋 CrossDesk Agent 已退出")
}

func printResult(data any, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
}

func splitComma(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
