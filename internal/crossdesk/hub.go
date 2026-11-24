package crossdesk

import (
	"context"
	"encoding/json"
	"log"
	"net"
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
	attempts      map[string]*attemptWindow
	// passwordResults 记录 change_password 的 request_id -> 新密码，保证重试幂等。
	passwordResults map[string]string
}

type attemptWindow struct {
	start time.Time
	count int
}

type transmission struct {
	host   string
	guests map[string]bool
}

type client struct {
	hub    *Hub
	ws     *websocket.Conn
	remote string
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
		attempts: map[string]*attemptWindow{}, passwordResults: map[string]string{},
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
		hub: h, ws: conn, remote: remoteIP(r), send: make(chan []byte, 64), done: make(chan struct{}),
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
	case "client_info":
		// 客户端版本/平台仅用于统计，Phase 1 不持久化。
	case "disconnect_peer":
		h.handleDisconnectPeer(c, msg)
	case "revoke_reconnect_credential":
		h.handleRevokeReconnect(c)
	case "change_password":
		h.handleChangePassword(c, msg)
	default:
		// 未知类型保持兼容：忽略而不是断开连接。
	}
}

func (h *Hub) handleLogin(c *client, msg map[string]any) {
	if !h.allowAttempt(c.remote) {
		c.sendJSON(map[string]any{
			"type": "login", "user_id": "", "status": "fail", "reason": "Too many authentication attempts",
		})
		return
	}
	id, _ := msg["user_id"].(string)
	id = strings.TrimSpace(id)

	// 重连令牌优先：不依赖密码，令牌本身即凭据。
	if token, _ := msg["reconnect_token"].(string); token != "" {
		version, _ := msg["reconnect_version"].(float64)
		rawID := id
		if i := strings.Index(rawID, "@"); i >= 0 {
			rawID = rawID[:i]
		}
		if int(version) == 1 {
			if expires, ok := h.store.VerifyReconnect(rawID, token); ok {
				h.completeLogin(c, rawID, rawID, nil, true, expires)
				return
			}
			c.sendJSON(map[string]any{
				"type": "login", "user_id": "", "status": "fail",
				"reason": "Reconnect expired or invalid", "reconnect_rejected": true,
			})
			return
		}
	}

	dev, ret, _, ok := h.resolveLogin(id)
	if !ok {
		c.sendJSON(map[string]any{
			"type": "login", "user_id": "", "status": "fail", "reason": "Authentication failed",
		})
		return
	}
	// 客户端请求重连令牌时，成功登录后签发（临时控制器不签发）。
	var reconnect map[string]any
	if v, _ := msg["reconnect_version"].(float64); int(v) == 1 && persistentDevice(dev.ID) {
		if token, expires, err := h.store.IssueReconnect(dev.ID); err == nil {
			reconnect = map[string]any{"reconnect_version": 1, "reconnect_token": token, "reconnect_expires_at": expires}
		}
	}
	h.completeLogin(c, dev.ID, ret, reconnect, false, 0)
}

func (h *Hub) completeLogin(c *client, id, ret string, extra map[string]any, reconnected bool, expires int64) {
	c.userID = id
	h.bind(c)
	_ = h.store.MarkOnline(id, true)
	resp := map[string]any{"type": "login", "user_id": ret, "status": "success"}
	if t := h.turn.turnFor(id); t != nil {
		resp["turn"] = t
	}
	if reconnected {
		resp["reconnected"] = true
		resp["reconnect_version"] = 1
		resp["reconnect_expires_at"] = expires
	}
	for k, v := range extra {
		resp[k] = v
	}
	c.sendJSON(resp)
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
	userID, _ := msg["user_id"].(string)
	if userID == "" || userID != c.userID {
		return
	}
	h.mu.Lock()
	tr := h.transmissions[tid]
	if tr == nil {
		h.mu.Unlock()
		return
	}
	if tr.host == c.userID {
		// host 离开：通知所有 guest 并释放传输。
		for g := range tr.guests {
			if gc := h.clients[g]; gc != nil {
				gc.sendJSON(map[string]any{"type": "user_leave_transmission", "transmission_id": tid, "user_id": c.userID})
			}
		}
		delete(h.transmissions, tid)
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

func (h *Hub) handleDisconnectPeer(c *client, msg map[string]any) {
	tid, _ := msg["transmission_id"].(string)
	remote, _ := msg["remote_user_id"].(string)
	h.mu.Lock()
	tr := h.transmissions[tid]
	if tr == nil || tr.host != c.userID || !tr.guests[remote] {
		h.mu.Unlock()
		return
	}
	delete(tr.guests, remote)
	guest := h.clients[remote]
	h.mu.Unlock()
	if guest != nil {
		guest.sendJSON(map[string]any{"type": "user_leave_transmission", "transmission_id": tid, "user_id": c.userID})
	}
}

func (h *Hub) handleRevokeReconnect(c *client) {
	if c.userID == "" {
		c.sendJSON(map[string]any{"type": "revoke_reconnect_credential", "user_id": "", "status": "fail"})
		return
	}
	if err := h.store.RevokeReconnect(c.userID); err != nil {
		c.sendJSON(map[string]any{"type": "revoke_reconnect_credential", "user_id": c.userID, "status": "fail", "reason": "Unable to revoke reconnect credential"})
		return
	}
	c.sendJSON(map[string]any{"type": "revoke_reconnect_credential", "user_id": c.userID, "status": "success"})
}

func (h *Hub) handleChangePassword(c *client, msg map[string]any) {
	requestID, _ := msg["request_id"].(string)
	newPassword, _ := msg["new_password"].(string)
	out := map[string]any{"type": "change_password", "user_id": c.userID, "request_id": requestID}
	if strings.TrimSpace(requestID) == "" || len(requestID) > 128 || strings.ContainsRune(requestID, 0) {
		out["status"] = "fail"
		out["reason"] = "Missing or invalid request ID"
		c.sendJSON(out)
		return
	}
	if c.userID == "" {
		out["status"] = "fail"
		out["reason"] = "Not authenticated"
		c.sendJSON(out)
		return
	}
	if isTemporaryID(c.userID) || strings.HasPrefix(c.userID, "C-") {
		out["status"] = "fail"
		out["reason"] = "Password changes are not allowed for this identity"
		c.sendJSON(out)
		return
	}
	if len(newPassword) != 6 {
		out["status"] = "fail"
		out["reason"] = "Password must contain exactly 6 characters"
		c.sendJSON(out)
		return
	}
	key := c.userID + "\n" + requestID
	h.mu.Lock()
	prev, exists := h.passwordResults[key]
	if len(h.passwordResults) > 4096 && !exists {
		for k := range h.passwordResults {
			delete(h.passwordResults, k)
			break
		}
	}
	h.mu.Unlock()
	if exists {
		if prev == newPassword {
			out["status"] = "success"
		} else {
			out["status"] = "fail"
			out["reason"] = "Request ID was already used"
		}
		c.sendJSON(out)
		return
	}
	if err := h.store.ChangePassword(c.userID, newPassword); err != nil {
		out["status"] = "fail"
		out["reason"] = "Failed to update password"
		c.sendJSON(out)
		return
	}
	_ = h.store.RevokeReconnect(c.userID)
	h.mu.Lock()
	h.passwordResults[key] = newPassword
	h.mu.Unlock()
	out["status"] = "success"
	c.sendJSON(out)
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

func remoteIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	// 反向代理场景优先使用真实客户端 IP；生产环境应由代理覆盖这些头。
	for _, h := range []string{"X-Real-IP", "X-Forwarded-For"} {
		v := strings.TrimSpace(r.Header.Get(h))
		if v == "" {
			continue
		}
		if i := strings.Index(v, ","); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		if net.ParseIP(v) != nil {
			return v
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// allowAttempt 每个来源每分钟最多 60 次登录/注册尝试，避免设备 ID 暴力枚举与批量注册。
func (h *Hub) allowAttempt(ip string) bool {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.attempts) > 20000 {
		for k, v := range h.attempts {
			if now.Sub(v.start) > 2*time.Minute {
				delete(h.attempts, k)
			}
		}
	}
	w := h.attempts[ip]
	if w == nil || now.Sub(w.start) > time.Minute {
		h.attempts[ip] = &attemptWindow{start: now, count: 1}
		return true
	}
	if w.count >= 60 {
		return false
	}
	w.count++
	return true
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

// SessionView 原生信令活动会话视图。
type SessionView struct {
	TransmissionID string   `json:"transmission_id"`
	Host           string   `json:"host"`
	Guests         []string `json:"guests"`
}

// Sessions 返回活动传输与 guest 列表。
func (h *Hub) Sessions() []SessionView {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]SessionView, 0, len(h.transmissions))
	for tid, tr := range h.transmissions {
		guests := make([]string, 0, len(tr.guests))
		for g := range tr.guests {
			guests = append(guests, g)
		}
		out = append(out, SessionView{TransmissionID: tid, Host: tr.host, Guests: guests})
	}
	return out
}

// Kick 断开指定用户连接；host 断开时会由 unregister 通知其 guests。
func (h *Hub) Kick(id string) bool {
	h.mu.Lock()
	c := h.clients[id]
	h.mu.Unlock()
	if c == nil {
		return false
	}
	c.close()
	return true
}

// DisconnectGuest 由 host 或管理员断开指定 guest。
func (h *Hub) DisconnectGuest(transmissionID, guest string) bool {
	h.mu.Lock()
	tr := h.transmissions[transmissionID]
	if tr == nil || !tr.guests[guest] {
		h.mu.Unlock()
		return false
	}
	delete(tr.guests, guest)
	gc := h.clients[guest]
	host := h.clients[tr.host]
	h.mu.Unlock()
	if gc != nil {
		gc.sendJSON(map[string]any{"type": "user_leave_transmission", "transmission_id": transmissionID, "user_id": tr.host})
	}
	if host != nil {
		host.sendJSON(map[string]any{"type": "user_leave_transmission", "transmission_id": transmissionID, "user_id": guest})
	}
	return true
}
