package liaison

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/ssh"

	"nhooyr.io/websocket"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

// wsHandshake 客户端在 WebSocket 建立后发送的第一条消息（文本 JSON）。
type wsHandshake struct {
	Kind       string `json:"kind"`                  // ssh / desktop(rdp|vnc)
	Host       string `json:"host"`                  // 目标 overlay IP 或主机名
	Port       int    `json:"port"`                  // 目标端口
	User       string `json:"user"`                  // SSH 用户名
	Password   string `json:"password,omitempty"`    // SSH 密码（可选，与 Key 二选一）
	PrivateKey string `json:"private_key,omitempty"` // SSH 私钥（PEM）
	Cols       int    `json:"cols"`                  // 终端列数
	Rows       int    `json:"rows"`                  // 终端行数
}

// handleWS 统一 WebSocket 入口：升级连接 → 读取握手 → 按 kind 分发。
func (s *Service) handleWS(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil {
		httpx.Fail(w, http.StatusUnauthorized, "未认证")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // 会话 Cookie 已鉴权；Origin 校验由 CSRF 中间件兜底
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "internal error")

	// 握手超时：10 秒内必须收到第一条消息。
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	_, data, err := conn.Read(ctx)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "握手超时或无效")
		return
	}
	var hs wsHandshake
	if err := json.Unmarshal(data, &hs); err != nil {
		conn.Close(websocket.StatusInvalidFramePayloadData, "握手消息非法")
		return
	}

	switch hs.Kind {
	case "ssh":
		s.handleSSHSession(conn, u, hs)
	case "desktop":
		s.handleDesktopSession(conn, u, hs)
	default:
		conn.Close(websocket.StatusUnsupportedData, "未知 kind: "+hs.Kind)
	}
}

// handleSSHSession 建立 SSH 会话并双向桥接 WebSocket。
func (s *Service) handleSSHSession(conn *websocket.Conn, u *auth.User, hs wsHandshake) {
	// 1. Casbin 鉴权：用户角色对 ssh:<host> 执行 connect
	if !s.authorizeUser(u, "ssh:"+hs.Host, ActConnect) {
		conn.Close(websocket.StatusPolicyViolation, "无权连接该主机")
		return
	}

	// 2. 构造 SSH 配置
	cfg, err := sshConfig(hs)
	if err != nil {
		sendWSError(conn, "SSH 配置错误: "+err.Error())
		return
	}
	addr := sshAddr(hs)
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		sendWSError(conn, "SSH 连接失败: "+err.Error())
		return
	}
	defer client.Close()

	// 3. 打开会话 + 请求 pty
	sess, err := client.NewSession()
	if err != nil {
		sendWSError(conn, "创建会话失败: "+err.Error())
		return
	}
	defer sess.Close()

	cols, rows := hs.Cols, hs.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, ssh.TerminalModes{
		ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		sendWSError(conn, "请求终端失败: "+err.Error())
		return
	}
	stdin, _ := sess.StdinPipe()
	stdout, _ := sess.StdoutPipe()
	stderr, _ := sess.StderrPipe()
	if err := sess.Shell(); err != nil {
		sendWSError(conn, "启动 shell 失败: "+err.Error())
		return
	}

	// 4. 通知就绪
	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"ready"}`)); err != nil {
		return
	}

	// 5. 双向桥接
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// stdout/stderr → WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				if we := conn.Write(ctx, websocket.MessageBinary, buf[:n]); we != nil {
					cancel()
					return
				}
			}
			if err != nil {
				cancel()
				return
			}
		}
	}()
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				if we := conn.Write(ctx, websocket.MessageBinary, buf[:n]); we != nil {
					cancel()
					return
				}
			}
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// WebSocket → stdin
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			if _, we := stdin.Write(data); we != nil {
				cancel()
				return
			}
		}
	}()

	// 等待会话结束或任一方向出错
	_ = sess.Wait()
}

// sshAddr 计算 SSH 目标地址。
func sshAddr(hs wsHandshake) string {
	port := hs.Port
	if port <= 0 {
		port = 22
	}
	return hs.Host + ":" + itoa(port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// sshConfig 从握手构造 SSH 客户端配置。
func sshConfig(hs wsHandshake) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod
	if hs.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(hs.PrivateKey))
		if err != nil {
			return nil, err
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if hs.Password != "" {
		auths = append(auths, ssh.Password(hs.Password))
	}
	if len(auths) == 0 {
		return nil, errNoAuth
	}
	return &ssh.ClientConfig{
		User:            hs.User,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // overlay 内网信任模型：忽略主机密钥（可后续改为 known_hosts）
		Timeout:         10 * time.Second,
	}, nil
}

var errNoAuth = &sshError{"未提供 SSH 凭据（密码或私钥）"}

type sshError struct{ msg string }

func (e *sshError) Error() string { return e.msg }

func sendWSError(conn *websocket.Conn, msg string) {
	b, _ := json.Marshal(map[string]string{"type": "error", "message": msg})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, b)
	// close reason 受 WebSocket 协议限制（≤123 字节），超长会截断
	reason := msg
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = conn.Close(websocket.StatusNormalClosure, reason)
}

// handleDesktopSession RDP/VNC（guacamole 通道）。先占位：返回错误提示未实现。
func (s *Service) handleDesktopSession(conn *websocket.Conn, u *auth.User, hs wsHandshake) {
	if !s.authorizeUser(u, "desktop:"+hs.Host, ActConnect) {
		conn.Close(websocket.StatusPolicyViolation, "无权连接该主机")
		return
	}
	sendWSError(conn, "远程桌面（RDP/VNC）尚未实现")
}
