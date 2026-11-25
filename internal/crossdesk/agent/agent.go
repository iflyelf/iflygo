package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// Config 是原生 CrossDesk Agent 的运行参数。
type Config struct {
	// ServerURL 控制面地址（https://iflygo.example.com 或 wss://.../crossdesk/signal）。
	ServerURL string
	// DeviceID / Password 是 CrossDesk 设备凭据；首次留空会自动注册并保存。
	DeviceID string
	Password string
	// FPS / Quality / MaxWidth 控制画面采集与编码。
	FPS      int
	Quality  int
	MaxWidth int
	// STUN 额外 STUN 服务器（host:port）。
	STUN []string
	// Insecure 跳过 TLS 校验（仅测试自签证书）。
	Insecure bool
	// CredentialsPath 保存自动注册设备的凭据，默认 ~/.config/iflygo/crossdesk-device.json。
	CredentialsPath string
	// Logger 为空时输出到 stdout。
	Logger *log.Logger
	// Screen / Input / Clipboard 可注入测试实现；为空时使用平台原生实现。
	Screen    Screen
	Input     Input
	Clipboard Clipboard
}

type credentials struct {
	Server     string `json:"server"`
	DeviceID   string `json:"device_id"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key,omitempty"`
}

type authError struct{ reason string }

func (e *authError) Error() string { return "CrossDesk 认证失败: " + e.reason }

// Agent 是纯 Go、CGO_ENABLED=0 可交叉编译的 CrossDesk 主机端。
type Agent struct {
	cfg    Config
	log    *log.Logger
	screen Screen
	input  Input

	conn    *websocket.Conn
	writeMu sync.Mutex

	mu       sync.Mutex
	sessions map[string]*session
	viewers  int
	capStop  context.CancelFunc

	clipboard    Clipboard
	clipSessions int
	clipMu       sync.Mutex
	clipText     string

	videoQuality atomic.Int32
	videoFPS     atomic.Int32
	videoPref    atomic.Int32

	deviceID   string
	password   string
	privateKey ed25519.PrivateKey
	publicKey  string

	turnMu sync.Mutex
	turn   *turnCred

	frameSeq atomic.Uint32
}

type turnCred struct {
	Host     string
	Port     int
	Username string
	Password string
}

var errMissingServer = errors.New("--server 必填")

// New 创建 Agent。
func New(cfg Config) *Agent { return &Agent{cfg: cfg} }

// Run 启动 Agent，断线后自动重连，直到 ctx 结束。
func (a *Agent) Run(ctx context.Context) error {
	if err := a.prepare(); err != nil {
		return err
	}
	defer a.closeResources()
	clipCtx, stopClip := context.WithCancel(ctx)
	defer stopClip()
	go a.clipboardLoop(clipCtx)

	var failures int
	for {
		err := a.runOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		var auth *authError
		if errors.As(err, &auth) {
			failures++
			if failures >= 3 {
				return err
			}
		} else {
			failures = 0
		}
		if err != nil {
			a.log.Printf("⚠️ CrossDesk 连接中断: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *Agent) prepare() error {
	if a.log == nil {
		a.log = a.cfg.Logger
	}
	if a.log == nil {
		a.log = log.New(os.Stdout, "", log.LstdFlags)
	}
	if a.cfg.FPS <= 0 {
		a.cfg.FPS = 10
	}
	if a.cfg.FPS > 30 {
		a.cfg.FPS = 30
	}
	if a.cfg.Quality <= 0 {
		a.cfg.Quality = 65
	}
	if a.cfg.MaxWidth < 0 {
		a.cfg.MaxWidth = 1600
	}
	a.sessions = map[string]*session{}
	a.videoQuality.Store(int32(a.cfg.Quality))
	a.videoFPS.Store(int32(a.cfg.FPS))
	a.videoPref.Store(1)
	a.deviceID = strings.TrimSpace(a.cfg.DeviceID)
	a.password = a.cfg.Password
	a.loadCredentials()
	if self, err := a.loadOrCreateKey(); err != nil {
		return err
	} else if self {
		if err := a.saveCredentials(); err != nil {
			a.log.Printf("⚠️ 保存设备密钥失败: %v", err)
		}
	}
	if a.cfg.Screen == nil {
		screen, err := newScreen()
		if err != nil {
			return fmt.Errorf("初始化屏幕采集失败: %w", err)
		}
		a.screen = screen
	} else {
		a.screen = a.cfg.Screen
	}
	if a.cfg.Input == nil {
		input, err := newInput()
		if err != nil {
			return fmt.Errorf("初始化输入注入失败: %w", err)
		}
		a.input = input
	} else {
		a.input = a.cfg.Input
	}
	if a.cfg.Clipboard != nil {
		a.clipboard = a.cfg.Clipboard
	} else if clipboard, err := newClipboard(); err != nil {
		a.log.Printf("⚠️ 剪贴板不可用（远程桌面仍可使用）: %v", err)
	} else {
		a.clipboard = clipboard
	}
	return nil
}

func (a *Agent) closeResources() {
	a.mu.Lock()
	if a.capStop != nil {
		a.capStop()
		a.capStop = nil
	}
	sessions := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = map[string]*session{}
	a.viewers = 0
	a.mu.Unlock()
	for _, s := range sessions {
		s.close()
	}
	if a.screen != nil {
		_ = a.screen.Close()
	}
	if a.input != nil {
		_ = a.input.Close()
	}
	if a.clipboard != nil {
		_ = a.clipboard.Close()
	}
}

func (a *Agent) runOnce(ctx context.Context) error {
	signalURL, err := normalizeSignalURL(a.cfg.ServerURL, "/crossdesk/signal")
	if err != nil {
		return err
	}
	opts := &websocket.DialOptions{}
	if a.cfg.Insecure {
		opts.HTTPClient = &http.Client{Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 用户显式开启
		}}
	}
	conn, resp, err := websocket.Dial(ctx, signalURL, opts)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("连接 %s 失败: HTTP %d", signalURL, resp.StatusCode)
		}
		return fmt.Errorf("连接 %s 失败: %w", signalURL, err)
	}
	a.conn = conn
	conn.SetReadLimit(1 << 20)
	defer func() {
		a.closeAllSessions()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		a.conn = nil
	}()

	payload := map[string]any{
		"type":                   "login",
		"session_resume_version": 1,
		"device_key":             map[string]any{"version": 1, "public_key": a.publicKey},
	}
	if a.deviceID != "" {
		payload["user_id"] = a.deviceID
	}
	if err := a.sendJSON(ctx, payload); err != nil {
		return err
	}

	hbCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go a.heartbeat(hbCtx)

	done := make(chan error, 1)
	go func() { done <- a.readLoop(ctx) }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		return err
	}
}

func (a *Agent) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.conn == nil {
				return
			}
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := a.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (a *Agent) readLoop(ctx context.Context) error {
	for {
		typ, data, err := a.conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if err := a.handle(ctx, msg); err != nil {
			return err
		}
	}
}

func (a *Agent) handle(ctx context.Context, msg map[string]any) error {
	switch msg["type"] {
	case "login":
		return a.handleLogin(msg)
	case "login_challenge":
		return a.handleChallenge(msg)
	case "user_join_transmission":
		if status, _ := msg["status"].(string); status != "success" {
			return nil
		}
		tid, _ := msg["transmission_id"].(string)
		guest, _ := msg["user_id"].(string)
		if tid == a.deviceID && guest != "" {
			a.startSession(guest)
		}
	case "answer":
		a.withSession(msg, func(s *session) { s.handleAnswer(msg) })
	case "new_candidate_mid", "new_candidate":
		a.withSession(msg, func(s *session) { s.handleCandidate(msg) })
	case "user_leave_transmission":
		guest, _ := msg["user_id"].(string)
		tid, _ := msg["transmission_id"].(string)
		if tid == a.deviceID && guest != "" {
			a.closeSession(guest)
		}
	case "turn_credentials":
		if status, _ := msg["status"].(string); status == "success" {
			if t, ok := msg["turn"].(map[string]any); ok {
				a.applyTurn(t)
			}
		}
	}
	return nil
}

func (a *Agent) withSession(msg map[string]any, fn func(*session)) {
	guest, _ := msg["remote_user_id"].(string)
	if guest == "" {
		return
	}
	a.mu.Lock()
	s := a.sessions[guest]
	a.mu.Unlock()
	if s != nil {
		fn(s)
	}
}

// handleChallenge 完成 Ed25519 设备密钥登录第二阶段。
//
// 设备首次绑定密钥时需要一次密码；绑定后只签名，不再发送密码。
func (a *Agent) handleChallenge(msg map[string]any) error {
	nonce, _ := msg["nonce"].(string)
	id, _ := msg["user_id"].(string)
	if nonce == "" || len(a.privateKey) != ed25519.PrivateKeySize {
		return &authError{reason: "设备密钥登录失败：挑战无效"}
	}
	transcript, err := json.Marshal([]string{"crossdesk.device-login.v1", id, a.publicKey, nonce})
	if err != nil {
		return err
	}
	signature := hex.EncodeToString(ed25519.Sign(a.privateKey, transcript))
	identity := id
	required, _ := msg["password_required"].(bool)
	a.log.Printf("🔐 设备密钥挑战：id=%q password_required=%v", id, required)
	if required {
		if a.password == "" {
			return &authError{reason: "设备尚未绑定密钥，请先使用 --password 登录一次完成绑定"}
		}
		identity = id + "@" + a.password
	}
	payload := map[string]any{
		"type": "login", "user_id": identity,
		"device_key": map[string]any{
			"version": 1, "public_key": a.publicKey, "nonce": nonce, "signature": signature,
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.sendJSON(ctx, payload)
}

func (a *Agent) handleLogin(msg map[string]any) error {
	status, _ := msg["status"].(string)
	if status != "success" {
		reason, _ := msg["reason"].(string)
		if reason == "" {
			reason = "未知原因"
		}
		if code, _ := msg["error_code"].(string); code == "DEVICE_KEY_REQUIRED" {
			return &authError{reason: "设备已绑定密钥登录，请使用官方 CrossDesk 客户端或重新注册设备"}
		}
		return &authError{reason: reason}
	}
	id, _ := msg["user_id"].(string)
	if id == "" {
		return &authError{reason: "服务器未返回设备 ID"}
	}
	if i := strings.Index(id, "@"); i >= 0 {
		a.deviceID, a.password = id[:i], id[i+1:]
		if err := a.saveCredentials(); err != nil {
			a.log.Printf("⚠️ 保存设备凭据失败: %v", err)
		}
	} else {
		a.deviceID = id
	}
	if t, ok := msg["turn"].(map[string]any); ok {
		a.applyTurn(t)
	}
	a.log.Printf("✅ 已登录 CrossDesk 设备 %s，等待连接（%d fps / JPEG %d / 最大宽 %d）",
		a.deviceID, a.videoFPS.Load(), a.videoQuality.Load(), a.cfg.MaxWidth)
	return nil
}

func (a *Agent) applyTurn(t map[string]any) {
	cred := &turnCred{}
	cred.Host, _ = t["host"].(string)
	cred.Username, _ = t["username"].(string)
	cred.Password, _ = t["password"].(string)
	if v, ok := t["port"].(float64); ok {
		cred.Port = int(v)
	}
	if cred.Host == "" || cred.Username == "" || cred.Password == "" {
		return
	}
	if cred.Port <= 0 {
		cred.Port = 3478
	}
	a.turnMu.Lock()
	a.turn = cred
	a.turnMu.Unlock()
}

func (a *Agent) sendJSON(ctx context.Context, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.conn == nil {
		return errors.New("信令未连接")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return a.conn.Write(writeCtx, websocket.MessageText, data)
}

func (a *Agent) sendAsync(payload map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.sendJSON(ctx, payload); err != nil {
		a.log.Printf("⚠️ 发送信令失败: %v", err)
	}
}

func (a *Agent) startSession(guest string) {
	if guest == "" || guest == a.deviceID {
		return
	}
	s, err := newSession(a, guest)
	if err != nil {
		a.log.Printf("⚠️ 创建 %s 的会话失败: %v", guest, err)
		return
	}
	a.mu.Lock()
	old := a.sessions[guest]
	a.sessions[guest] = s
	if old == nil {
		a.viewers++
	}
	startCapture := a.viewers == 1 && a.capStop == nil
	if startCapture {
		ctx, cancel := context.WithCancel(context.Background())
		a.capStop = cancel
		go a.captureLoop(ctx)
	}
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
	a.log.Printf("👀 新连接: %s", guest)
}

func (a *Agent) closeSession(guest string) {
	a.mu.Lock()
	s := a.sessions[guest]
	a.mu.Unlock()
	if s != nil {
		s.close()
	}
}

func (a *Agent) closeAllSessions() {
	a.mu.Lock()
	sessions := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessions = map[string]*session{}
	a.viewers = 0
	if a.capStop != nil {
		a.capStop()
		a.capStop = nil
	}
	a.mu.Unlock()
	for _, s := range sessions {
		s.close()
	}
}

// dropSession 由 session 关闭时调用；只有当前映射仍指向它时才清理。
func (a *Agent) dropSession(s *session) {
	a.mu.Lock()
	current := a.sessions[s.guest] == s
	if current {
		delete(a.sessions, s.guest)
		if a.viewers > 0 {
			a.viewers--
		}
		if a.viewers == 0 && a.capStop != nil {
			a.capStop()
			a.capStop = nil
		}
	}
	a.mu.Unlock()
	if current {
		a.log.Printf("👋 连接结束: %s", s.guest)
	}
}

func (a *Agent) captureLoop(ctx context.Context) {
	// 以 60 Hz 轮询，按客户端协商的帧率跳帧；质量同样实时调整。
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()
	var last time.Time
	var lastErrAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		fps := int(a.videoFPS.Load())
		if fps < 1 {
			fps = 10
		}
		if fps > 60 {
			fps = 60
		}
		if !last.IsZero() && time.Since(last) < time.Second/time.Duration(fps) {
			continue
		}
		last = time.Now()
		img, err := a.screen.Capture()
		if err != nil {
			if time.Since(lastErrAt) > 5*time.Second {
				a.log.Printf("⚠️ 屏幕采集失败: %v", err)
				lastErrAt = time.Now()
			}
			continue
		}
		quality := int(a.videoQuality.Load())
		jpeg, w, h, err := encodeJPEG(img, a.cfg.MaxWidth, quality)
		if err != nil {
			if time.Since(lastErrAt) > 5*time.Second {
				a.log.Printf("⚠️ 画面编码失败: %v", err)
				lastErrAt = time.Now()
			}
			continue
		}
		seq := a.frameSeq.Add(1)
		a.broadcast(chunkFrame(seq, w, h, jpeg))
	}
}

func (a *Agent) broadcast(chunks [][]byte) {
	if len(chunks) == 0 {
		return
	}
	a.mu.Lock()
	list := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		list = append(list, s)
	}
	a.mu.Unlock()
	for _, s := range list {
		s.send(chunks)
	}
}

func (a *Agent) rtcConfiguration() (iceServers []iceServer) {
	a.turnMu.Lock()
	turn := a.turn
	a.turnMu.Unlock()
	for _, stun := range a.cfg.STUN {
		stun = strings.TrimSpace(stun)
		if stun == "" {
			continue
		}
		if !strings.HasPrefix(stun, "stun:") && !strings.HasPrefix(stun, "stuns:") {
			stun = "stun:" + stun
		}
		iceServers = append(iceServers, iceServer{URLs: []string{stun}})
	}
	if turn != nil {
		urls := []string{
			fmt.Sprintf("turn:%s:%d?transport=udp", turn.Host, turn.Port),
			fmt.Sprintf("turn:%s:%d?transport=tcp", turn.Host, turn.Port),
		}
		iceServers = append(iceServers, iceServer{URLs: urls, Username: turn.Username, Credential: turn.Password})
	}
	return iceServers
}

func (a *Agent) loadCredentials() {
	path := a.credentialsPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var c credentials
	if json.Unmarshal(data, &c) != nil {
		return
	}
	if a.deviceID == "" {
		a.deviceID = strings.TrimSpace(c.DeviceID)
	}
	if a.password == "" {
		a.password = c.Password
	}
	if a.cfg.ServerURL == "" {
		a.cfg.ServerURL = c.Server
	}
	if len(c.PrivateKey) == ed25519.SeedSize*2 {
		if seed, err := hex.DecodeString(c.PrivateKey); err == nil {
			a.privateKey = ed25519.NewKeyFromSeed(seed)
			a.publicKey = hex.EncodeToString(a.privateKey.Public().(ed25519.PublicKey))
		}
	}
}

// loadOrCreateKey 载入或生成 Ed25519 设备密钥；返回是否为新生成。
func (a *Agent) loadOrCreateKey() (bool, error) {
	if len(a.privateKey) == ed25519.PrivateKeySize && a.publicKey != "" {
		return false, nil
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return false, fmt.Errorf("生成设备密钥失败: %w", err)
	}
	a.privateKey = ed25519.NewKeyFromSeed(seed)
	a.publicKey = hex.EncodeToString(a.privateKey.Public().(ed25519.PublicKey))
	return true, nil
}

func (a *Agent) saveCredentials() error {
	path := a.credentialsPath()
	if path == "" || a.deviceID == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	cred := credentials{Server: a.cfg.ServerURL, DeviceID: a.deviceID, Password: a.password}
	if len(a.privateKey) == ed25519.PrivateKeySize {
		cred.PrivateKey = hex.EncodeToString(a.privateKey.Seed())
	}
	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (a *Agent) credentialsPath() string {
	if a.cfg.CredentialsPath != "" {
		return a.cfg.CredentialsPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "iflygo", "crossdesk-device.json")
}
