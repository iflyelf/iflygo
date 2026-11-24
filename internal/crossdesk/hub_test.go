package crossdesk

import (
	"context"
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
}

func newMemStore() *memStore {
	return &memStore{passwords: map[string]string{}, kinds: map[string]string{}, online: map[string]bool{}}
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
