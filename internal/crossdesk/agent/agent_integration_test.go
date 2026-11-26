package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"

	"github.com/iflyelf/iflygo/internal/crossdesk"
)

type fakeStore struct {
	mu         sync.Mutex
	registered chan struct{}
	passwords  map[string]string
	kinds      map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		registered: make(chan struct{}, 1),
		passwords:  map[string]string{},
		kinds:      map[string]string{},
	}
}

func (f *fakeStore) Register(kind string) (crossdesk.Device, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if kind == "web" || kind == "cli" {
		id := kind + "-guest"
		f.kinds[id] = kind
		return crossdesk.Device{ID: id, Kind: kind}, "", nil
	}
	id, pwd := "123456789", "abc123"
	if _, exists := f.kinds[id]; !exists {
		f.kinds[id] = "desktop"
		f.passwords[id] = pwd
		select {
		case f.registered <- struct{}{}:
		default:
		}
	}
	return crossdesk.Device{ID: id, Kind: "desktop"}, f.passwords[id], nil
}

func (f *fakeStore) Verify(id, password string) (crossdesk.Device, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kind, ok := f.kinds[id]
	if !ok {
		return crossdesk.Device{}, false
	}
	if kind == "desktop" && f.passwords[id] != password {
		return crossdesk.Device{}, false
	}
	return crossdesk.Device{ID: id, Kind: kind}, true
}

func (f *fakeStore) MarkOnline(id string, online bool) error { return nil }
func (f *fakeStore) Stats() (int64, int64, error)            { return 1, 1, nil }
func (f *fakeStore) IssueReconnect(string) (string, int64, error) {
	return "", 0, fmt.Errorf("not supported")
}
func (f *fakeStore) VerifyReconnect(string, string) (int64, bool) { return 0, false }
func (f *fakeStore) RevokeReconnect(string) error                 { return nil }
func (f *fakeStore) ChangePassword(string, string) error          { return nil }
func (f *fakeStore) LoginKey(string) (string, bool)               { return "", false }
func (f *fakeStore) BindLoginKey(string, string) error            { return nil }
func (f *fakeStore) IssueJoinTicket(string, string, string) (string, int64, error) {
	return "", 0, fmt.Errorf("not supported")
}
func (f *fakeStore) ConsumeJoinTicket(string, string, string) bool { return false }
func (f *fakeStore) GrantDeviceUser(string, string, string) error  { return nil }
func (f *fakeStore) RevokeDeviceUser(string, string) error         { return nil }
func (f *fakeStore) ListDeviceUsers(string) ([]string, error)      { return nil, nil }
func (f *fakeStore) UserCanAccess(string, string) (bool, error)    { return false, nil }

type fakeScreen struct {
	img *image.RGBA
}

func newFakeScreen() *fakeScreen {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 120, A: 255})
		}
	}
	return &fakeScreen{img: img}
}

func (f *fakeScreen) Capture() (image.Image, error) { return f.img, nil }
func (f *fakeScreen) Close() error                  { return nil }

func (f *fakeScreen) Displays() ([]DisplayInfo, error) {
	return []DisplayInfo{
		{ID: 0, Name: "主屏", X: 0, Y: 0, Width: 64, Height: 48, Primary: true},
		{ID: 1, Name: "副屏", X: 64, Y: 0, Width: 64, Height: 48},
	}, nil
}

func (f *fakeScreen) CaptureDisplay(id int) (image.Image, error) {
	if id != 0 && id != 1 {
		return nil, fmt.Errorf("unknown display %d", id)
	}
	return f.img, nil
}

type fakeMouse struct {
	X, Y   float64
	Flag   int
	Scroll int
}

type fakeInput struct {
	mu     sync.Mutex
	mouse  []fakeMouse
	keys   []int
	region [4]int
}

func (f *fakeInput) Mouse(x, y float64, flag, scroll int) error {
	f.mu.Lock()
	f.mouse = append(f.mouse, fakeMouse{X: x, Y: y, Flag: flag, Scroll: scroll})
	f.mu.Unlock()
	return nil
}

func (f *fakeInput) Key(vk int, down bool) error {
	f.mu.Lock()
	f.keys = append(f.keys, vk)
	f.mu.Unlock()
	return nil
}

func (f *fakeInput) Close() error { return nil }

func (f *fakeInput) SetDisplay(x, y, w, h int) {
	f.mu.Lock()
	f.region = [4]int{x, y, w, h}
	f.mu.Unlock()
}

type fakeAudio struct {
	started atomic.Bool
}

func (f *fakeAudio) Start(ctx context.Context) (io.ReadCloser, error) {
	f.started.Store(true)
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		chunk := make([]byte, audioChunk)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := writer.Write(chunk); err != nil {
					return
				}
			}
		}
	}()
	return reader, nil
}

func (f *fakeAudio) Close() error { return nil }

type fakeMicSink struct {
	mu   sync.Mutex
	data []byte
}

func (f *fakeMicSink) Start(ctx context.Context) (io.WriteCloser, error) {
	return &fakeMicWriter{sink: f}, nil
}

func (f *fakeMicSink) Close() error { return nil }

type fakeMicWriter struct{ sink *fakeMicSink }

func (w *fakeMicWriter) Write(p []byte) (int, error) {
	w.sink.mu.Lock()
	w.sink.data = append(w.sink.data, p...)
	w.sink.mu.Unlock()
	return len(p), nil
}

func (w *fakeMicWriter) Close() error { return nil }

type fakeClipboard struct {
	mu     sync.Mutex
	text   string
	writes []string
}

func (f *fakeClipboard) ReadText() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, nil
}

func (f *fakeClipboard) WriteText(text string) error {
	f.mu.Lock()
	f.text = text
	f.writes = append(f.writes, text)
	f.mu.Unlock()
	return nil
}

func (f *fakeClipboard) Close() error { return nil }

func TestAgentStreamsFramesAndAcceptsInput(t *testing.T) {
	store := newFakeStore()
	hub := crossdesk.NewHub(store, crossdesk.TurnConfig{}, func() bool { return true }, log.New(io.Discard, "", 0))
	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer srv.Close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	input := &fakeInput{}
	clip := &fakeClipboard{text: "hello-from-host"}
	a := New(Config{
		ServerURL: srv.URL, FPS: 30, Quality: 55, MaxWidth: 0,
		CredentialsPath: t.TempDir() + "/device.json",
		Logger:          log.New(io.Discard, "", 0),
		Screen:          newFakeScreen(), Input: input, Clipboard: clip,
		FileRoots: []string{root}, Audio: true, AudioSource: &fakeAudio{},
		Mic: true, MicSink: &fakeMicSink{},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	select {
	case <-store.registered:
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not register")
	}
	// 等待 host 完成登录并绑定传输。
	deadline := time.Now().Add(10 * time.Second)
	for {
		if len(hub.Sessions()) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent did not bind transmission")
		}
		time.Sleep(50 * time.Millisecond)
	}

	guestCtx, guestCancel := context.WithTimeout(ctx, 30*time.Second)
	defer guestCancel()
	guest, _, err := websocket.Dial(guestCtx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("guest dial: %v", err)
	}
	defer guest.Close(websocket.StatusNormalClosure, "")
	send := func(payload map[string]any) {
		data, _ := json.Marshal(payload)
		if err := guest.Write(guestCtx, websocket.MessageText, data); err != nil {
			t.Fatalf("guest write: %v", err)
		}
	}
	recv := func() map[string]any {
		typ, data, err := guest.Read(guestCtx)
		if err != nil {
			t.Fatalf("guest read: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("unexpected message type %v", typ)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("guest decode: %v", err)
		}
		return msg
	}

	send(map[string]any{"type": "login", "user_id": "web"})
	for {
		msg := recv()
		if msg["type"] == "login" {
			if msg["status"] != "success" {
				t.Fatalf("guest login failed: %v", msg)
			}
			break
		}
	}
	send(map[string]any{"type": "join_transmission", "user_id": "web-guest", "transmission_id": "123456789@abc123"})

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()

	frames := make(chan []byte, 8)
	controlText := make(chan []byte, 16)
	controlReady := make(chan *webrtc.DataChannel, 1)
	motionReady := make(chan *webrtc.DataChannel, 1)
	clipReady := make(chan *webrtc.DataChannel, 1)
	clipMessages := make(chan []byte, 16)
	audioReady := make(chan *webrtc.DataChannel, 1)
	audioPackets := make(chan []byte, 64)
	micReady := make(chan *webrtc.DataChannel, 1)
	fileReady := make(chan *webrtc.DataChannel, 1)
	feedbackReady := make(chan *webrtc.DataChannel, 1)
	filePackets := make(chan []byte, 64)
	feedbackPackets := make(chan []byte, 64)
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case "iflygo-mic-v1":
			dc.OnOpen(func() {
				select {
				case micReady <- dc:
				default:
				}
			})
		case "iflygo-audio-v1":
			dc.OnOpen(func() {
				select {
				case audioReady <- dc:
				default:
				}
			})
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					select {
					case audioPackets <- append([]byte(nil), m.Data...):
					default:
					}
				}
			})
		case "file":
			dc.OnOpen(func() {
				select {
				case fileReady <- dc:
				default:
				}
			})
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					select {
					case filePackets <- append([]byte(nil), m.Data...):
					default:
					}
				}
			})
		case "file_feedback":
			dc.OnOpen(func() {
				select {
				case feedbackReady <- dc:
				default:
				}
			})
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				if !m.IsString {
					select {
					case feedbackPackets <- append([]byte(nil), m.Data...):
					default:
					}
				}
			})
		case "web-clipboard":
			dc.OnOpen(func() {
				select {
				case clipReady <- dc:
				default:
				}
			})
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				if m.IsString {
					return
				}
				select {
				case clipMessages <- append([]byte(nil), m.Data...):
				default:
				}
			})
		case "data-stream":
			dc.OnOpen(func() {
				select {
				case controlReady <- dc:
				default:
				}
			})
			dc.OnMessage(func(m webrtc.DataChannelMessage) {
				data := append([]byte(nil), m.Data...)
				if m.IsString {
					select {
					case controlText <- data:
					default:
					}
					return
				}
				select {
				case frames <- data:
				default:
				}
			})
		case "crossdesk-input-v1":
			dc.OnOpen(func() {
				select {
				case motionReady <- dc:
				default:
				}
			})
		}
	})

	var offerSent bool
	for !offerSent {
		msg := recv()
		switch msg["type"] {
		case "offer":
			sdp, _ := msg["sdp"].(string)
			if sdp == "" {
				t.Fatalf("empty offer: %v", msg)
			}
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
				t.Fatalf("set remote offer: %v", err)
			}
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				t.Fatalf("create answer: %v", err)
			}
			if err := pc.SetLocalDescription(answer); err != nil {
				t.Fatalf("set local answer: %v", err)
			}
			send(map[string]any{
				"type": "answer", "transmission_id": "123456789", "user_id": "web-guest",
				"remote_user_id": "123456789", "sdp": pc.LocalDescription().SDP,
			})
			offerSent = true
		case "new_candidate_mid", "new_candidate":
			// 极少数情况下候选先于 offer 到达；忽略即可，offer 处理后会继续读取。
		}
	}
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		payload := map[string]any{
			"type": "new_candidate_mid", "transmission_id": "123456789", "user_id": "web-guest",
			"remote_user_id": "123456789", "candidate": init.Candidate,
		}
		if init.SDPMid != nil {
			payload["mid"] = *init.SDPMid
		}
		if init.SDPMLineIndex != nil {
			payload["sdpMLineIndex"] = *init.SDPMLineIndex
		}
		data, _ := json.Marshal(payload)
		_ = guest.Write(ctx, websocket.MessageText, data)
	})

	// 持续转发候选，直到收到画面。
	candidateDone := make(chan struct{})
	go func() {
		defer close(candidateDone)
		for {
			typ, data, err := guest.Read(guestCtx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var msg map[string]any
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			if msg["type"] != "new_candidate_mid" && msg["type"] != "new_candidate" {
				continue
			}
			cand, _ := msg["candidate"].(string)
			if cand == "" {
				continue
			}
			init := webrtc.ICECandidateInit{Candidate: cand}
			if mid, ok := msg["mid"].(string); ok {
				init.SDPMid = &mid
			}
			if v, ok := msg["sdpMLineIndex"].(float64); ok {
				idx := uint16(v)
				init.SDPMLineIndex = &idx
			}
			_ = pc.AddICECandidate(init)
		}
	}()

	var dc *webrtc.DataChannel
	select {
	case dc = <-controlReady:
	case <-time.After(15 * time.Second):
		t.Fatal("data channel did not open")
	}
	var motion *webrtc.DataChannel
	select {
	case motion = <-motionReady:
	case <-time.After(15 * time.Second):
		t.Fatal("motion channel did not open")
	}

	var firstChunk []byte
	select {
	case firstChunk = <-frames:
	case <-time.After(15 * time.Second):
		t.Fatal("no frame received")
	}
	seq, index, total, width, height, ok := frameHeader(firstChunk)
	if !ok || index != 0 || width != 64 || height != 48 {
		t.Fatalf("bad frame header: seq=%d index=%d total=%d %dx%d ok=%v", seq, index, total, width, height, ok)
	}

	// 发送鼠标与键盘输入，验证 Agent 注入。
	data, _ := json.Marshal(map[string]any{
		"type": 0, "mouse": map[string]any{"x": 0.25, "y": 0.75, "flag": 0, "s": 0},
	})
	if err := dc.SendText(string(data)); err != nil {
		t.Fatalf("send mouse: %v", err)
	}
	data, _ = json.Marshal(map[string]any{
		"type": 1, "keyboard": map[string]any{"key_value": 65, "flag": 0},
	})
	if err := dc.SendText(string(data)); err != nil {
		t.Fatalf("send key: %v", err)
	}

	// 剪贴板通道：HELLO -> READY，主机推送文本，客户端写入文本。
	var clipDC *webrtc.DataChannel
	select {
	case clipDC = <-clipReady:
	case <-time.After(15 * time.Second):
		t.Fatal("clipboard channel did not open")
	}
	if err := clipDC.Send(clipFrame(clipKindHello, 0, clipVersion, 0, nil)); err != nil {
		t.Fatalf("send clipboard hello: %v", err)
	}
	readClip := func(wantKind uint32) (kind, id uint32, payload []byte) {
		deadline := time.After(10 * time.Second)
		for {
			select {
			case msg := <-clipMessages:
				k, i, _, _, ok := parseClipFrame(msg)
				if !ok {
					continue
				}
				if k == wantKind {
					return k, i, msg[clipHeaderSize:]
				}
			case <-deadline:
				t.Fatalf("clipboard frame kind %d not received", wantKind)
			}
		}
	}
	readClip(clipKindReady)
	if err := clipDC.Send(clipFrame(clipKindText, 7, uint32(len("guest-to-host")), 0, []byte("guest-to-host"))); err != nil {
		t.Fatalf("send clipboard text: %v", err)
	}
	_, appliedID, _ := readClip(clipKindApplied)
	if appliedID != 7 {
		t.Fatalf("unexpected clipboard ack id: %d", appliedID)
	}
	clip.mu.Lock()
	writes := append([]string(nil), clip.writes...)
	clip.mu.Unlock()
	if len(writes) == 0 || writes[len(writes)-1] != "guest-to-host" {
		t.Fatalf("clipboard write not applied: %v", writes)
	}
	// 主机剪贴板变化应推送到 Guest。
	clip.mu.Lock()
	clip.text = "host-update"
	clip.mu.Unlock()
	_, _, payload := readClip(clipKindText)
	if string(payload) != "host-update" {
		t.Fatalf("unexpected clipboard payload: %q", string(payload))
	}

	// 多显示器：host_info 应包含两台显示器，切换后输入区域随之更新。
	displays, active := a.displaySnapshot()
	if len(displays) != 2 || active != 0 {
		t.Fatalf("unexpected displays: %+v active=%d", displays, active)
	}
	input.mu.Lock()
	initialRegion := input.region
	input.mu.Unlock()
	if initialRegion != [4]int{0, 0, 64, 48} {
		t.Fatalf("initial input region: %v", initialRegion)
	}
	if err := dc.SendText(`{"type":4,"display_id":1}`); err != nil {
		t.Fatalf("send display switch: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		input.mu.Lock()
		region := input.region
		input.mu.Unlock()
		if a.activeDisplay.Load() == 1 && region == [4]int{64, 0, 64, 48} {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("display switch not applied: active=%d region=%v", a.activeDisplay.Load(), region)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 画质/帧率协商：type 11 -> type 12，并更新编码参数。
	if err := dc.SendText(`{"type":11,"video_settings":{"quality":1,"frame_rate":60,"preference":2,"request_id":9}}`); err != nil {
		t.Fatalf("send video settings: %v", err)
	}
	settingsDeadline := time.After(5 * time.Second)
	for {
		select {
		case data := <-controlText:
			var reply struct {
				Type          int `json:"type"`
				VideoSettings struct {
					Accepted   bool   `json:"accepted"`
					Quality    int    `json:"quality"`
					FrameRate  int    `json:"frame_rate"`
					Preference int    `json:"preference"`
					RequestID  uint32 `json:"request_id"`
				} `json:"video_settings"`
			}
			if json.Unmarshal(data, &reply) != nil || reply.Type != 12 {
				continue
			}
			if !reply.VideoSettings.Accepted || reply.VideoSettings.Quality != 1 ||
				reply.VideoSettings.FrameRate != 60 || reply.VideoSettings.Preference != 2 ||
				reply.VideoSettings.RequestID != 9 {
				t.Fatalf("unexpected video settings reply: %s", string(data))
			}
			if a.videoFPS.Load() != 60 || a.videoQuality.Load() != 62 {
				t.Fatalf("agent settings not updated: fps=%d quality=%d", a.videoFPS.Load(), a.videoQuality.Load())
			}
			goto settingsDone
		case <-settingsDeadline:
			t.Fatal("video settings reply not received")
		}
	}
settingsDone:

	// 独立验证无序 motion 通道（Web 客户端拖动鼠标时使用）。
	data, _ = json.Marshal(map[string]any{
		"type": 0, "mouse": map[string]any{"x": 0.9, "y": 0.1, "flag": 0, "s": 0},
	})
	if err := motion.SendText(string(data)); err != nil {
		t.Fatalf("send motion: %v", err)
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		input.mu.Lock()
		mice, keys := len(input.mouse), len(input.keys)
		foundMotion := false
		for _, ev := range input.mouse {
			if ev.Flag == MouseMove && ev.X == 0.9 && ev.Y == 0.1 {
				foundMotion = true
				break
			}
		}
		input.mu.Unlock()
		if foundMotion && keys > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("input not injected: mice=%d keys=%d motion=%v", mice, keys, foundMotion)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// --- 音频 ---
	var audioDC *webrtc.DataChannel
	select {
	case audioDC = <-audioReady:
	case <-time.After(10 * time.Second):
		t.Fatal("audio channel did not open")
	}
	if err := dc.SendText(`{"type":2,"audio_capture":true}`); err != nil {
		t.Fatalf("enable audio: %v", err)
	}
	select {
	case packet := <-audioPackets:
		if len(packet) != audioHeader+audioChunk/4 {
			t.Fatalf("unexpected audio packet size: %d", len(packet))
		}
		format := binary.LittleEndian.Uint16(packet[14:16])
		if string(packet[0:4]) != "CDA1" ||
			binary.LittleEndian.Uint32(packet[8:12]) != audioRate ||
			binary.LittleEndian.Uint16(packet[12:14]) != audioChannels ||
			(format != audioFormatADPCM && format != audioFormatADPCMReset) {
			t.Fatalf("bad audio header: %x", packet[:audioHeader])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("audio packet not received")
	}
	if err := dc.SendText(`{"type":2,"audio_capture":false}`); err != nil {
		t.Fatalf("disable audio: %v", err)
	}
	_ = audioDC

	var micDC *webrtc.DataChannel
	select {
	case micDC = <-micReady:
	case <-time.After(10 * time.Second):
		t.Fatal("mic channel did not open")
	}
	micPayload := []byte("mic-pcm")
	micFrame := make([]byte, audioHeader+len(micPayload))
	copy(micFrame[0:4], "CDA1")
	binary.LittleEndian.PutUint32(micFrame[4:8], 1)
	binary.LittleEndian.PutUint32(micFrame[8:12], micRate)
	binary.LittleEndian.PutUint16(micFrame[12:14], micChannels)
	binary.LittleEndian.PutUint16(micFrame[14:16], audioFormatPCM)
	copy(micFrame[audioHeader:], micPayload)
	if err := micDC.Send(micFrame); err != nil {
		t.Fatalf("send mic frame: %v", err)
	}
	micDeadline := time.Now().Add(3 * time.Second)
	for {
		sink := a.mic.(*fakeMicSink)
		sink.mu.Lock()
		got := append([]byte(nil), sink.data...)
		sink.mu.Unlock()
		if string(got) == string(micPayload) {
			break
		}
		if time.Now().After(micDeadline) {
			t.Fatalf("mic payload not played: %q", got)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// --- 文件浏览与传输 ---
	var fileDC, feedbackDC *webrtc.DataChannel
	select {
	case fileDC = <-fileReady:
	case <-time.After(10 * time.Second):
		t.Fatal("file channel did not open")
	}
	select {
	case feedbackDC = <-feedbackReady:
	case <-time.After(10 * time.Second):
		t.Fatal("file feedback channel did not open")
	}
	_ = feedbackDC

	sendRPC := func(id int64, payload map[string]any) {
		payload["type"] = "file_browser"
		payload["version"] = 1
		payload["id"] = id
		data, _ := json.Marshal(payload)
		if err := dc.SendText(string(data)); err != nil {
			t.Fatalf("send file rpc %d: %v", id, err)
		}
	}
	readReply := func(id int64) map[string]any {
		timeout := time.After(5 * time.Second)
		for {
			select {
			case data := <-controlText:
				var msg map[string]any
				if json.Unmarshal(data, &msg) != nil || msg["type"] != "file_browser" || msg["op"] != "reply" {
					continue
				}
				if v, ok := msg["id"].(float64); !ok || int64(v) != id {
					continue
				}
				return msg
			case <-timeout:
				t.Fatalf("file reply %d not received", id)
			}
		}
	}

	sendRPC(101, map[string]any{"op": "list", "path": "@roots", "pagination": 1})
	roots := readReply(101)
	if roots["ok"] != true {
		t.Fatalf("roots list failed: %v", roots)
	}
	foundRoot := false
	for _, raw := range roots["entries"].([]any) {
		entry, _ := raw.(map[string]any)
		if entry["path"] == realRoot {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Fatalf("root %s missing from %v", realRoot, roots["entries"])
	}

	sendRPC(102, map[string]any{"op": "list", "path": root, "pagination": 1})
	listing := readReply(102)
	if listing["ok"] != true {
		t.Fatalf("directory list failed: %v", listing)
	}
	foundFile := false
	for _, raw := range listing["entries"].([]any) {
		entry, _ := raw.(map[string]any)
		if entry["name"] == "hello.txt" && entry["directory"] == false &&
			entry["size"] == float64(len("hello-file")) {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatalf("hello.txt missing from %v", listing["entries"])
	}
	sendRPC(103, map[string]any{"op": "list", "path": "/etc", "pagination": 1})
	if readReply(103)["ok"] == true {
		t.Fatal("path outside roots should be rejected")
	}

	// 下载：Agent -> Guest。
	sendRPC(104, map[string]any{
		"op": "copy", "paths": []string{filepath.Join(root, "hello.txt")}, "transfer_id": "download-1",
	})
	if readReply(104)["ok"] != true {
		t.Fatal("copy request rejected")
	}
	var packet []byte
	select {
	case packet = <-filePackets:
	case <-time.After(10 * time.Second):
		t.Fatal("download packet not received")
	}
	if len(packet) < fileHeaderSize || binary.LittleEndian.Uint32(packet[0:4]) != fileChunkMagic {
		t.Fatalf("bad download packet: %d bytes", len(packet))
	}
	if binary.LittleEndian.Uint32(packet[4:8]) == 0 {
		t.Fatal("missing download id")
	}
	if binary.LittleEndian.Uint64(packet[8:16]) != 0 ||
		binary.LittleEndian.Uint64(packet[16:24]) != uint64(len("hello-file")) {
		t.Fatal("bad download offset/total")
	}
	nameSize := int(binary.LittleEndian.Uint16(packet[28:30]))
	downloadPayload := packet[fileHeaderSize+nameSize:]
	if packet[30]&3 != 3 || string(packet[fileHeaderSize:fileHeaderSize+nameSize]) != "hello.txt" ||
		string(downloadPayload) != "hello-file" {
		t.Fatalf("bad download payload: flags=%d name=%q payload=%q", packet[30],
			string(packet[fileHeaderSize:fileHeaderSize+nameSize]), string(downloadPayload))
	}

	// 目录下载：Agent 先向浏览器发送 prepare，再按预留 ID 发送文件。
	folder := filepath.Join(root, "folder")
	if err := os.MkdirAll(filepath.Join(folder, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(folder, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	sendRPC(106, map[string]any{
		"op": "copy", "paths": []string{folder}, "destination": "/local/test", "transfer_id": "dir-download",
	})
	var prepare map[string]any
	copyOK := false
	prepDeadline := time.After(8 * time.Second)
	for prepare == nil || !copyOK {
		select {
		case data := <-controlText:
			var msg map[string]any
			if json.Unmarshal(data, &msg) != nil || msg["type"] != "file_browser" {
				continue
			}
			if msg["op"] == "reply" {
				if v, ok := msg["id"].(float64); ok && int64(v) == 106 {
					if msg["ok"] != true {
						t.Fatalf("directory copy rejected: %v", msg)
					}
					copyOK = true
				}
			} else if msg["op"] == "prepare" {
				prepare = msg
			}
		case <-prepDeadline:
			t.Fatalf("prepare not received (copyOK=%v)", copyOK)
		}
	}
	items := prepare["items"].([]any)
	dirByID := map[uint32]string{}
	dirs := map[string]bool{}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		dir, _ := item["directory"].(string)
		dirs[dir] = true
		if id, ok := item["file_id"].(float64); ok && id != 0 {
			dirByID[uint32(id)] = dir
		}
	}
	for _, want := range []string{"folder", "folder/sub", "folder/empty"} {
		if !dirs[want] {
			t.Fatalf("prepare missing directory %q: %v", want, dirs)
		}
	}
	reply := map[string]any{"type": "file_browser", "version": 1, "id": prepare["id"], "op": "reply", "ok": true}
	replyData, _ := json.Marshal(reply)
	if err := dc.SendText(string(replyData)); err != nil {
		t.Fatalf("send prepare reply: %v", err)
	}
	gotFiles := map[string]string{}
	fileDeadline := time.After(8 * time.Second)
	for len(gotFiles) < 2 {
		select {
		case packet := <-filePackets:
			if len(packet) < fileHeaderSize || binary.LittleEndian.Uint32(packet[0:4]) != fileChunkMagic {
				continue
			}
			id := binary.LittleEndian.Uint32(packet[4:8])
			nameSize := int(binary.LittleEndian.Uint16(packet[28:30]))
			name := string(packet[fileHeaderSize : fileHeaderSize+nameSize])
			payload := string(packet[fileHeaderSize+nameSize:])
			gotFiles[filepath.ToSlash(filepath.Join(dirByID[id], name))] = payload
		case <-fileDeadline:
			t.Fatalf("directory download files missing: %v", gotFiles)
		}
	}
	if gotFiles["folder/a.txt"] != "A" || gotFiles["folder/sub/b.txt"] != "B" {
		t.Fatalf("unexpected directory download: %v", gotFiles)
	}

	// 上传：Guest -> Agent。
	sendRPC(105, map[string]any{
		"op": "prepare", "destination": root,
		"items": []map[string]any{
			{"file_id": 0, "directory": "uploads"},
			{"file_id": 77, "directory": "uploads"},
		},
	})
	if readReply(105)["ok"] != true {
		t.Fatal("prepare request rejected")
	}
	uploadName := []byte("uploaded.txt")
	uploadData := []byte("from-browser")
	upload := make([]byte, fileHeaderSize+len(uploadName)+len(uploadData))
	binary.LittleEndian.PutUint32(upload[0:4], fileChunkMagic)
	binary.LittleEndian.PutUint32(upload[4:8], 77)
	binary.LittleEndian.PutUint64(upload[8:16], 0)
	binary.LittleEndian.PutUint64(upload[16:24], uint64(len(uploadData)))
	binary.LittleEndian.PutUint32(upload[24:28], uint32(len(uploadData)))
	binary.LittleEndian.PutUint16(upload[28:30], uint16(len(uploadName)))
	upload[30] = 3
	copy(upload[fileHeaderSize:], uploadName)
	copy(upload[fileHeaderSize+len(uploadName):], uploadData)
	if err := fileDC.Send(upload); err != nil {
		t.Fatalf("send upload: %v", err)
	}
	select {
	case ack := <-feedbackPackets:
		if len(ack) != fileAckSize || binary.LittleEndian.Uint32(ack[0:4]) != fileAckMagic ||
			binary.LittleEndian.Uint32(ack[4:8]) != 77 || binary.LittleEndian.Uint32(ack[24:28])&1 == 0 {
			t.Fatalf("bad upload ack: %v", ack)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upload ack not received")
	}
	got, err := os.ReadFile(filepath.Join(root, "uploads", "uploaded.txt"))
	if err != nil || string(got) != "from-browser" {
		t.Fatalf("uploaded file mismatch: %q err=%v", string(got), err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("agent stopped with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not stop")
	}
}
