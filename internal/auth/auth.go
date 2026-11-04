// Package auth 实现会话 Cookie 认证与管理员引导。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// User 已认证用户。
type User struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// Auth 会话认证器。
type Auth struct {
	s         *store.Store
	secret    []byte
	ttl       time.Duration
	cookieSec string // auto | true | false
}

const cookieName = "iflygo_session"

type sessionPayload struct {
	ID          int64  `json:"id"`
	Username    string `json:"u"`
	DisplayName string `json:"d"`
	IsAdmin     bool   `json:"a"`
	Exp         int64  `json:"exp"`
}

// New 创建认证器。
func New(s *store.Store, secret string, ttl time.Duration, cookieSecure string) *Auth {
	if secret == "" {
		secret = randomHex(32)
		log.Printf("⚠️ SESSION_SECRET 未设置，已生成临时密钥（重启后会话失效），建议生产显式配置")
	}
	if ttl <= 0 {
		ttl = 168 * time.Hour
	}
	if cookieSecure == "" {
		cookieSecure = "auto"
	}
	return &Auth{s: s, secret: []byte(secret), ttl: ttl, cookieSec: cookieSecure}
}

// EnsureAdmin 首个启动时创建管理员账号。
func (a *Auth) EnsureAdmin(username, password string) error {
	if username == "" {
		username = "admin"
	}
	var n int
	if err := a.s.DB.QueryRow(a.s.Rebind(`SELECT COUNT(*) FROM users`)).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if password == "" {
		password = randomHex(16)
		log.Printf("🔐 已创建管理员账号 %s，初始密码: %s（请尽快修改）", username, password)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.s.DB.Exec(a.s.Rebind(`INSERT INTO users (username, password_hash, display_name, is_admin, created_at) VALUES ($1,$2,$3,$4,$5)`),
		username, string(hash), username, true, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Verify 校验用户名密码。
func (a *Auth) Verify(username, password string) (*User, error) {
	var u User
	var hash string
	q := a.s.Rebind(`SELECT id, username, password_hash, display_name, is_admin FROM users WHERE username = $1`)
	if err := a.s.DB.QueryRow(q, username).Scan(&u.ID, &u.Username, &hash, &u.DisplayName, &u.IsAdmin); err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, errors.New("用户名或密码错误")
	}
	return &u, nil
}

// Issue 下发会话 Cookie。
func (a *Auth) Issue(w http.ResponseWriter, r *http.Request, u *User) {
	payload := sessionPayload{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin,
		Exp: time.Now().Add(a.ttl).Unix(),
	}
	raw, _ := json.Marshal(payload)
	body := base64.RawURLEncoding.EncodeToString(raw)
	sig := a.sign(body)
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    body + "." + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.ttl.Seconds()),
	}
	http.SetCookie(w, cookie)
}

// User 从请求解析用户（未登录返回 nil）。
func (a *Auth) User(r *http.Request) (*User, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, nil
	}
	body, sig, ok := cutLast(c.Value, '.')
	if !ok {
		return nil, errors.New("会话非法")
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(a.sign(body))) != 1 {
		return nil, errors.New("会话签名错误")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Exp < time.Now().Unix() {
		return nil, errors.New("会话已过期")
	}
	return &User{ID: p.ID, Username: p.Username, DisplayName: p.DisplayName, IsAdmin: p.IsAdmin}, nil
}

// Clear 清除会话 Cookie。
func (a *Auth) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func (a *Auth) sign(body string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func (a *Auth) secure(r *http.Request) bool {
	switch a.cookieSec {
	case "true":
		return true
	case "false":
		return false
	default:
		return r.TLS != nil
	}
}

func cutLast(s string, sep byte) (string, string, bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
