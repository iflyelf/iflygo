package crossdesk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

type memStore struct {
	mu        sync.Mutex
	passwords map[string]string
	kinds     map[string]string
	online    map[string]bool
	loginKeys map[string]string
}

func newMemStore() *memStore {
	return &memStore{passwords: map[string]string{}, kinds: map[string]string{}, online: map[string]bool{}, loginKeys: map[string]string{}}
}

func (m *memStore) Register(kind string) (Device, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if kind == "web" || kind == "cli" {
		id := kind + "-123456789"
		m.kinds[id] = kind
		return Device{ID: id, Kind: kind}, "", nil
	}
	id, pwd := "123456789", "abc123"
	m.kinds[id] = "desktop"
	m.passwords[id] = pwd
	return Device{ID: id, Kind: "desktop"}, pwd, nil
}

func (m *memStore) Verify(id, password string) (Device, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kind, ok := m.kinds[id]
	if !ok {
		if isTemporaryID(id) {
			return Device{ID: id, Kind: "controller"}, true
		}
		return Device{}, false
	}
	if kind == "web" || kind == "cli" || isTemporaryID(id) {
		return Device{ID: id, Kind: kind}, true
	}
	if m.passwords[id] != password {
		return Device{}, false
	}
	return Device{ID: id, Kind: kind}, true
}

func (m *memStore) MarkOnline(id string, online bool) error {
	m.mu.Lock()
	m.online[id] = online
	m.mu.Unlock()
	return nil
}

func (m *memStore) IssueReconnect(id string) (string, int64, error) {
	return strings.Repeat("a", 64), time.Now().Add(time.Hour).Unix(), nil
}

func (m *memStore) VerifyReconnect(id, token string) (int64, bool) {
	return time.Now().Add(time.Hour).Unix(), token == strings.Repeat("a", 64)
}

func (m *memStore) RevokeReconnect(id string) error { return nil }

func (m *memStore) ChangePassword(id, newPassword string) error {
	if len(newPassword) != 6 {
		return fmt.Errorf("密码必须为 6 位")
	}
	m.mu.Lock()
	m.passwords[id] = newPassword
	m.mu.Unlock()
	return nil
}

func (m *memStore) LoginKey(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loginKeys[id], true
}

func (m *memStore) BindLoginKey(id, publicKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.loginKeys[id] != "" && m.loginKeys[id] != publicKey {
		return fmt.Errorf("已绑定其他公钥")
	}
	m.loginKeys[id] = publicKey
	return nil
}

func (m *memStore) IssueJoinTicket(deviceID, createdBy, createdIP string) (string, int64, error) {
	return strings.Repeat("b", 64), time.Now().Add(time.Minute).Unix(), nil
}

func (m *memStore) ConsumeJoinTicket(deviceID, token, usedIP string) bool {
	return token == strings.Repeat("b", 64)
}

func (m *memStore) GrantDeviceUser(deviceID, username, createdBy string) error { return nil }
func (m *memStore) RevokeDeviceUser(deviceID, username string) error           { return nil }
func (m *memStore) ListDeviceUsers(deviceID string) ([]string, error)          { return nil, nil }
func (m *memStore) UserCanAccess(deviceID, username string) (bool, error)      { return false, nil }

func (m *memStore) Stats() (int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var online int64
	for _, v := range m.online {
		if v {
			online++
		}
	}
	return int64(len(m.kinds)), online, nil
}

func TestSignalingRelay(t *testing.T) {
	store := newMemStore()
	h := NewHub(store, TurnConfig{Host: "turn.example.com", Port: 3478, Secret: "secret", TTL: time.Hour}, func() bool { return true }, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	host := dial(t, wsURL)
	defer host.Close(websocket.StatusNormalClosure, "")
	guest := dial(t, wsURL)
	defer guest.Close(websocket.StatusNormalClosure, "")

	host.send(t, map[string]any{"type": "login", "user_id": ""})
	login := host.read(t)
	if login["status"] != "success" {
		t.Fatalf("host 登录失败: %v", login)
	}
	hostLogin := login["user_id"].(string)
	parts := strings.SplitN(hostLogin, "@", 2)
	if len(parts) != 2 {
		t.Fatalf("host user_id 应包含密码: %v", hostLogin)
	}
	hostID, hostPwd := parts[0], parts[1]
	if login["turn"] == nil {
		t.Fatalf("登录响应应包含 TURN 凭据: %v", login)
	}

	guest.send(t, map[string]any{"type": "login", "user_id": "web"})
	login = guest.read(t)
	if login["status"] != "success" {
		t.Fatalf("guest 登录失败: %v", login)
	}
	guestID := login["user_id"].(string)

	guest.send(t, map[string]any{
		"type": "join_transmission", "user_id": guestID,
		"transmission_id": hostID + "@" + hostPwd,
	})
	join := host.read(t)
	if join["type"] != "user_join_transmission" || join["user_id"] != guestID {
		t.Fatalf("host 未收到 join: %v", join)
	}
	hostTicket := host.read(t)
	if hostTicket["type"] != "session_ticket" {
		t.Fatalf("host 未收到 session_ticket: %v", hostTicket)
	}
	if ticket := guest.read(t); ticket["type"] != "session_ticket" {
		t.Fatalf("guest 未收到 session_ticket: %v", ticket)
	}
	ticketToken, _ := hostTicket["token"].(string)

	host.send(t, map[string]any{
		"type": "offer", "transmission_id": hostID, "user_id": hostID,
		"remote_user_id": guestID, "sdp": "v=0 offer",
	})
	offer := guest.read(t)
	if offer["type"] != "offer" || offer["sdp"] != "v=0 offer" || offer["remote_user_id"] != hostID {
		t.Fatalf("guest 未收到 offer: %v", offer)
	}

	guest.send(t, map[string]any{
		"type": "answer", "transmission_id": hostID, "user_id": guestID,
		"remote_user_id": hostID, "sdp": "v=0 answer",
	})
	answer := host.read(t)
	if answer["type"] != "answer" || answer["sdp"] != "v=0 answer" {
		t.Fatalf("host 未收到 answer: %v", answer)
	}

	guest.send(t, map[string]any{
		"type": "new_candidate_mid", "transmission_id": hostID, "user_id": guestID,
		"remote_user_id": hostID, "candidate": "candidate:1", "mid": "0",
	})
	candidate := host.read(t)
	if candidate["type"] != "new_candidate_mid" || candidate["candidate"] != "candidate:1" {
		t.Fatalf("host 未收到 candidate: %v", candidate)
	}

	report := map[string]any{
		"type": "session_report", "transmission_id": hostID,
		"guest_id": guestID, "token": ticketToken, "alive": true,
	}
	host.send(t, report)
	guest.send(t, report)
	if resumed := host.read(t); resumed["type"] != "session_resumed" {
		t.Fatalf("host 未收到 session_resumed: %v", resumed)
	}
	if resumed := guest.read(t); resumed["type"] != "session_resumed" {
		t.Fatalf("guest 未收到 session_resumed: %v", resumed)
	}

	guest.send(t, map[string]any{"type": "ping"})
	if pong := guest.read(t); pong["type"] != "pong" {
		t.Fatalf("未收到 pong: %v", pong)
	}

	stats := h.Stats()
	if stats["connections"].(int) != 2 || stats["transmissions"].(int) != 1 {
		t.Fatalf("统计不正确: %v", stats)
	}
}

type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context
}

func dial(t *testing.T, base string) *wsClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, base+"/crossdesk/signal", nil)
	if err != nil {
		t.Fatalf("连接信令失败: %v", err)
	}
	return &wsClient{t: t, conn: conn, ctx: context.Background()}
}

func (c *wsClient) Close(code websocket.StatusCode, reason string) error {
	return c.conn.Close(code, reason)
}

func (c *wsClient) send(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("发送失败: %v", err)
	}
}

func (c *wsClient) read(t *testing.T) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}
	return m
}

func TestReconnectAndChangePassword(t *testing.T) {
	store := newMemStore()
	h := NewHub(store, TurnConfig{}, func() bool { return true }, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	host := dial(t, wsURL)
	host.send(t, map[string]any{"type": "login", "user_id": "", "reconnect_version": 1})
	login := host.read(t)
	if login["status"] != "success" {
		t.Fatalf("注册登录失败: %v", login)
	}
	fullID, _ := login["user_id"].(string)
	parts := strings.SplitN(fullID, "@", 2)
	if len(parts) != 2 {
		t.Fatalf("登录响应缺少密码: %v", fullID)
	}
	id := parts[0]
	token, _ := login["reconnect_token"].(string)
	if token == "" {
		t.Fatalf("登录响应缺少 reconnect_token: %v", login)
	}
	_ = host.Close(websocket.StatusNormalClosure, "")

	re := dial(t, wsURL)
	defer re.Close(websocket.StatusNormalClosure, "")
	re.send(t, map[string]any{
		"type": "login", "user_id": id, "reconnect_token": token, "reconnect_version": 1,
	})
	login = re.read(t)
	if login["status"] != "success" || login["reconnected"] != true {
		t.Fatalf("重连失败: %v", login)
	}

	re.send(t, map[string]any{
		"type": "change_password", "request_id": "req-1", "new_password": "newpwd",
	})
	ch := re.read(t)
	if ch["status"] != "success" {
		t.Fatalf("修改密码失败: %v", ch)
	}
	// 同一 request_id + 同一密码幂等成功。
	re.send(t, map[string]any{
		"type": "change_password", "request_id": "req-1", "new_password": "newpwd",
	})
	if ch = re.read(t); ch["status"] != "success" {
		t.Fatalf("幂等修改密码失败: %v", ch)
	}
	// 同一 request_id + 不同密码应拒绝。
	re.send(t, map[string]any{
		"type": "change_password", "request_id": "req-1", "new_password": "other1",
	})
	if ch = re.read(t); ch["status"] != "fail" {
		t.Fatalf("复用 request_id 应失败: %v", ch)
	}
}

func TestDeviceKeyLogin(t *testing.T) {
	store := newMemStore()
	h := NewHub(store, TurnConfig{}, func() bool { return true }, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := hex.EncodeToString(pub)

	c := dial(t, wsURL)
	c.send(t, map[string]any{
		"type": "login", "user_id": "",
		"device_key": map[string]any{"version": 1, "public_key": publicKey},
	})
	challenge := c.read(t)
	if challenge["type"] != "login_challenge" {
		t.Fatalf("未收到 login_challenge: %v", challenge)
	}
	nonce, _ := challenge["nonce"].(string)

	sign := func(id, nonce string) string {
		transcript, _ := json.Marshal([]string{"crossdesk.device-login.v1", id, publicKey, nonce})
		return hex.EncodeToString(ed25519.Sign(priv, transcript))
	}
	c.send(t, map[string]any{
		"type": "login", "user_id": "",
		"device_key": map[string]any{
			"version": 1, "public_key": publicKey, "nonce": nonce, "signature": sign("", nonce),
		},
	})
	login := c.read(t)
	if login["status"] != "success" || login["device_key_version"] != float64(1) {
		t.Fatalf("设备密钥注册失败: %v", login)
	}
	fullID, _ := login["user_id"].(string)
	parts := strings.SplitN(fullID, "@", 2)
	if len(parts) != 2 {
		t.Fatalf("注册响应缺少密码: %v", fullID)
	}
	id := parts[0]
	_ = c.Close(websocket.StatusNormalClosure, "")

	// 重新连接：设备密钥登录。
	c2 := dial(t, wsURL)
	defer c2.Close(websocket.StatusNormalClosure, "")
	c2.send(t, map[string]any{
		"type": "login", "user_id": id,
		"device_key": map[string]any{"version": 1, "public_key": publicKey},
	})
	challenge = c2.read(t)
	if challenge["type"] != "login_challenge" || challenge["password_required"] != false {
		t.Fatalf("设备密钥重连 challenge 不正确: %v", challenge)
	}
	nonce, _ = challenge["nonce"].(string)
	c2.send(t, map[string]any{
		"type": "login", "user_id": id,
		"device_key": map[string]any{
			"version": 1, "public_key": publicKey, "nonce": nonce, "signature": sign(id, nonce),
		},
	})
	login = c2.read(t)
	if login["status"] != "success" {
		t.Fatalf("设备密钥登录失败: %v", login)
	}

	// 已绑定设备密钥后，普通密码登录应被拒绝。
	c3 := dial(t, wsURL)
	defer c3.Close(websocket.StatusNormalClosure, "")
	c3.send(t, map[string]any{"type": "login", "user_id": fullID})
	fail := c3.read(t)
	if fail["error_code"] != "DEVICE_KEY_REQUIRED" {
		t.Fatalf("密码登录应要求设备密钥: %v", fail)
	}
}

func TestJoinWithTicket(t *testing.T) {
	store := newMemStore()
	h := NewHub(store, TurnConfig{}, func() bool { return true }, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	host := dial(t, wsURL)
	defer host.Close(websocket.StatusNormalClosure, "")
	host.send(t, map[string]any{"type": "login", "user_id": ""})
	login := host.read(t)
	fullID, _ := login["user_id"].(string)
	parts := strings.SplitN(fullID, "@", 2)
	id := parts[0]

	guest := dial(t, wsURL)
	defer guest.Close(websocket.StatusNormalClosure, "")
	guest.send(t, map[string]any{"type": "login", "user_id": "web"})
	login = guest.read(t)
	guestID, _ := login["user_id"].(string)

	guest.send(t, map[string]any{
		"type": "join_transmission", "user_id": guestID,
		"transmission_id": id + "@", "join_ticket": strings.Repeat("b", 64),
	})
	join := host.read(t)
	if join["type"] != "user_join_transmission" || join["user_id"] != guestID || join["status"] != "success" {
		t.Fatalf("免密 join 失败: %v", join)
	}
	_ = host.read(t) // session_ticket
	_ = guest.read(t)
}
