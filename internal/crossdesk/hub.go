package crossdesk

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Hub 原生 CrossDesk 信令服务。
type Hub struct {
	store DeviceStore
	turn  TurnConfig
	log   *log.Logger
	// enabled 在请求时读取设置，支持页面开关而无需重启路由。
	enabled func() bool

	mu            sync.Mutex
	clients       map[string]*client
	transmissions map[string]*transmission
}

type transmission struct {
	host   string
	guests map[string]bool
}

type client struct {
	hub    *Hub
	ws     *websocket.Conn
	userID string
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func NewHub(store DeviceStore, turn TurnConfig, enabled func() bool, logger *log.Logger) *Hub {
	if logger == nil {
		logger = log.Default()
	}
	return &Hub{
		store: store, turn: turn, enabled: enabled, log: logger,
		clients: map[string]*client{}, transmissions: map[string]*transmission{},
	}
}

// ServeWS 处理 CrossDesk WSS 信令连接。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if h.enabled != nil && !h.enabled() {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:     []string{"*"},
		CompressionMode:    websocket.CompressionDisabled,
		InsecureSkipVerify: false,
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 20)
	c := &client{
		hub: h, ws: conn, send: make(chan []byte, 64), done: make(chan struct{}),
	}
	go c.writeLoop()
	c.readLoop()
}

func (c *client) readLoop() {
	defer c.close()
	for {
		typ, data, err := c.ws.Read(context.Background())
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
		c.hub.dispatch(c, msg)
	}
}

func (c *client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.ws.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.done)
		c.hub.unregister(c)
		_ = c.ws.Close(websocket.StatusNormalClosure, "")
	})
}

func (c *client) sendJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.sendRaw(b)
}

func (c *client) sendRaw(b []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.send <- b:
	default:
		c.close()
	}
}

// unregister 清理连接、在线状态与传输关系。
func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	current := c.userID != "" && h.clients[c.userID] == c
	if current {
		delete(h.clients, c.userID)
		if !isTemporaryID(c.userID) {
			if tr := h.transmissions[c.userID]; tr != nil {
				for g := range tr.guests {
					if gc := h.clients[g]; gc != nil {
						gc.sendJSON(map[string]any{
							"type": "user_leave_transmission", "transmission_id": c.userID, "user_id": c.userID,
						})
					}
				}
			}
			delete(h.transmissions, c.userID)
		}
		// 从其他传输中移除该 guest，并通知 host。
		for tid, tr := range h.transmissions {
			if tr.guests[c.userID] {
				delete(tr.guests, c.userID)
				if host := h.clients[tr.host]; host != nil {
					host.sendJSON(map[string]any{
						"type": "user_leave_transmission", "transmission_id": tid, "user_id": c.userID,
					})
				}
			}
		}
	}
	h.mu.Unlock()
	if current {
		_ = h.store.MarkOnline(c.userID, false)
	}
}

func (h *Hub) dispatch(c *client, msg map[string]any) {
	typ, _ := msg["type"].(string)
	switch typ {
	case "ping":
		c.sendJSON(map[string]any{"type": "pong"})
	case "login":
		h.handleLogin(c, msg)
	case "join_transmission":
		h.handleJoin(c, msg)
	case "query_user_id_list":
		h.handleUserList(c, msg)
	case "offer":
		h.relay(c, msg, "offer")
	case "answer":
		h.relay(c, msg, "answer")
	case "new_candidate":
		h.relay(c, msg, "new_candidate")
	case "new_candidate_mid":
		h.relay(c, msg, "new_candidate_mid")
	case "user_leave_transmission":
		h.handleLeave(c, msg)
	case "turn_credentials":
		h.handleTurn(c)
	case "recent_connections_presence":
		h.handlePresence(c, msg)
	case "session_report":
		// 会话状态由客户端自行维护；Phase 1 不持久化。
	case "client_info", "disconnect_peer":
		// 管理类消息在 Phase 1 暂不处理。
	case "change_password":
		c.sendJSON(map[string]any{"type": "change_password", "status": "fail", "reason": "Phase 1 暂不支持修改密码"})
	default:
		// 未知类型保持兼容：忽略而不是断开连接。
	}
}

func (h *Hub) handleLogin(c *client, msg map[string]any) {
	id, _ := msg["user_id"].(string)
	id = strings.TrimSpace(id)
	dev, ret, pwd, ok := h.resolveLogin(id)
	if !ok {
		c.sendJSON(map[string]any{
			"type": "login", "user_id": "", "status": "fail", "reason": "Authentication failed",
		})
		return
	}
	c.userID = dev.ID
	h.bind(c)
	_ = h.store.MarkOnline(dev.ID, true)
	resp := map[string]any{"type": "login", "user_id": ret, "status": "success"}
	if t := h.turn.turnFor(dev.ID); t != nil {
		resp["turn"] = t
	}
	c.sendJSON(resp)
	_ = pwd
}

func (h *Hub) resolveLogin(raw string) (Device, string, string, bool) {
	switch {
	case strings.HasPrefix(raw, "C-"):
		return Device{ID: raw, Kind: "controller"}, raw, "", true
	case raw == "web" || raw == "cli":
		dev, _, err := h.store.Register(raw)
		if err != nil {
			return Device{}, "", "", false
		}
		return dev, dev.ID, "", true
	case raw == "":
		dev, pwd, err := h.store.Register("desktop")
		if err != nil {
			return Device{}, "", "", false
		}
		return dev, dev.ID + "@" + pwd, pwd, true
	case strings.Contains(raw, "@"):
		parts := strings.SplitN(raw, "@", 2)
		dev, ok := h.store.Verify(parts[0], parts[1])
		if !ok {
			return Device{}, "", "", false
		}
		return dev, parts[0], parts[1], true
	case isTemporaryID(raw):
		return Device{ID: raw, Kind: "controller"}, raw, "", true
	default:
		dev, ok := h.store.Verify(raw, "")
		if !ok {
			return Device{}, "", "", false
		}
		return dev, raw, "", true
	}
}

func (h *Hub) bind(c *client) {
	h.mu.Lock()
	old := h.clients[c.userID]
	h.clients[c.userID] = c
	if !isTemporaryID(c.userID) {
		h.transmissions[c.userID] = &transmission{host: c.userID, guests: map[string]bool{}}
	}
	h.mu.Unlock()
	if old != nil && old != c {
		go old.close()
	}
}

func (h *Hub) handleJoin(c *client, msg map[string]any) {
	if c.userID == "" {
		c.sendJSON(map[string]any{"type": "user_join_transmission", "status": "failed", "reason": "Not authenticated"})
		return
	}
	tidRaw, _ := msg["transmission_id"].(string)
	target, pwd := splitIDPassword(tidRaw)
	if target == "" {
		c.sendJSON(map[string]any{"type": "user_join_transmission", "status": "failed", "reason": "Invalid transmission"})
		return
	}
	if pwd == "" {
		c.sendJSON(map[string]any{
			"type": "user_join_transmission", "transmission_id": target,
			"status": "failed", "error_code": "PASSWORD_REQUIRED", "reason": "Password required",
		})
		return
	}
	if _, ok := h.store.Verify(target, pwd); !ok {
		c.sendJSON(map[string]any{
			"type": "user_join_transmission", "transmission_id": target,
			"status": "failed", "error_code": "AUTHENTICATION_FAILED", "reason": "Authentication failed",
		})
		return
	}

	h.mu.Lock()
	host := h.clients[target]
	tr := h.transmissions[target]
	if tr == nil && host != nil {
		tr = &transmission{host: target, guests: map[string]bool{}}
		h.transmissions[target] = tr
	}
	if host == nil || tr == nil {
		h.mu.Unlock()
		c.sendJSON(map[string]any{
			"type": "user_join_transmission", "transmission_id": target,
			"status": "failed", "error_code": "REMOTE_UNAVAILABLE", "reason": "Remote unavailable",
		})
		return
	}
	tr.guests[c.userID] = true
	h.mu.Unlock()

	host.sendJSON(map[string]any{
		"type": "user_join_transmission", "transmission_id": target,
		"user_id": c.userID, "status": "success",
	})
}

func (h *Hub) handleUserList(c *client, msg map[string]any) {
	tidRaw, _ := msg["transmission_id"].(string)
	target, pwd := splitIDPassword(tidRaw)
	if target == "" || pwd == "" {
		c.sendJSON(map[string]any{"type": "user_id_list", "transmission_id": target, "user_id_list": []string{}, "status": "failed"})
		return
	}
	if _, ok := h.store.Verify(target, pwd); !ok {
		c.sendJSON(map[string]any{"type": "user_id_list", "transmission_id": target, "user_id_list": []string{}, "status": "failed"})
		return
	}
	h.mu.Lock()
	list := []string{target}
	if tr := h.transmissions[target]; tr != nil {
		for g := range tr.guests {
			list = append(list, g)
		}
	}
	h.mu.Unlock()
	c.sendJSON(map[string]any{"type": "user_id_list", "transmission_id": target, "user_id_list": list, "status": "success"})
}

func (h *Hub) relay(c *client, msg map[string]any, kind string) {
	if c.userID == "" {
		return
	}
	tid, _ := msg["transmission_id"].(string)
	userID, _ := msg["user_id"].(string)
	remote, _ := msg["remote_user_id"].(string)
	if userID != "" && userID != c.userID {
		return
	}
	h.mu.Lock()
	tr := h.transmissions[tid]
	if tr == nil || !authorized(tr, c.userID) || !authorized(tr, remote) {
		h.mu.Unlock()
		return
	}
	dest := h.clients[remote]
	h.mu.Unlock()
	if dest == nil {
		return
	}
	out := map[string]any{"type": kind, "transmission_id": tid, "remote_user_id": c.userID}
	switch kind {
	case "offer", "answer":
		sdp, _ := msg["sdp"].(string)
		if sdp == "" {
			return
		}
		out["sdp"] = sdp
		if kind == "offer" {
			if t := h.turn.turnFor(remote); t != nil {
				out["turn"] = t
			}
		}
	case "new_candidate_mid":
		candidate, _ := msg["candidate"].(string)
		mid, _ := msg["mid"].(string)
		if candidate == "" {
			return
		}
		out["candidate"], out["mid"] = candidate, mid
	case "new_candidate":
		candidate, _ := msg["candidate"].(string)
		if candidate == "" {
			candidate, _ = msg["sdp"].(string)
		}
		if candidate == "" {
			return
		}
		out["sdp"] = candidate
	}
	for _, k := range []string{"username_fragment", "ice_ufrag"} {
		if v, ok := msg[k].(string); ok && v != "" {
			out[k] = v
		}
	}
	dest.sendJSON(out)
}

func (h *Hub) handleLeave(c *client, msg map[string]any) {
	tid, _ := msg["transmission_id"].(string)
	h.mu.Lock()
	tr := h.transmissions[tid]
	if tr == nil {
		h.mu.Unlock()
		return
	}
	delete(tr.guests, c.userID)
	host := h.clients[tr.host]
	h.mu.Unlock()
	if host != nil {
		host.sendJSON(map[string]any{"type": "user_leave_transmission", "transmission_id": tid, "user_id": c.userID})
	}
}

func (h *Hub) handleTurn(c *client) {
	if c.userID == "" {
		c.sendJSON(map[string]any{"type": "turn_credentials", "status": "fail", "reason": "Not authenticated"})
		return
	}
	t := h.turn.turnFor(c.userID)
	if t == nil {
		c.sendJSON(map[string]any{"type": "turn_credentials", "status": "fail", "reason": "TURN credentials are not configured"})
		return
	}
	c.sendJSON(map[string]any{"type": "turn_credentials", "status": "success", "turn": t})
}

func (h *Hub) handlePresence(c *client, msg map[string]any) {
	ids, _ := msg["devices"].([]any)
	resp := map[string]any{"type": "presence", "devices": []any{}}
	list := make([]any, 0, len(ids))
	h.mu.Lock()
	for _, v := range ids {
		id, _ := v.(string)
		if id == "" {
			continue
		}
		list = append(list, map[string]any{"id": id, "online": h.clients[id] != nil})
	}
	h.mu.Unlock()
	resp["devices"] = list
	c.sendJSON(resp)
}

func splitIDPassword(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "@"); i >= 0 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}

func authorized(tr *transmission, id string) bool {
	if tr == nil || id == "" {
		return false
	}
	return tr.host == id || tr.guests[id]
}

// ServeStats 原生 CrossDesk 统计接口。
func (h *Hub) ServeStats(w http.ResponseWriter, r *http.Request) {
	if h.enabled != nil && !h.enabled() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(h.Stats())
}

// Stats 返回设备与连接统计（供原生 /crossdesk/stats 使用）。
func (h *Hub) Stats() map[string]any {
	total, online, err := h.store.Stats()
	if err != nil {
		total, online = 0, 0
	}
	h.mu.Lock()
	conns, transmissions := len(h.clients), len(h.transmissions)
	h.mu.Unlock()
	return map[string]any{
		"devices": total, "online_devices": online,
		"connections": conns, "transmissions": transmissions,
		"time": time.Now().UTC().Format(time.RFC3339),
	}
}
