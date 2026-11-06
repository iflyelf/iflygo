// Package ddnsto 远程访问：给每个客户端自动分配子域名，一键穿透到内网服务。
//
// 定位：iflygo edge 服务（自定义域名 → overlay 目标）的「自动化」层。
// 用户无需手动填域名，只需指定客户端 + 目标端口，服务端自动：
//  1. 生成稳定子域名 <slug(client_name)>.<ddnsto_domain>；
//  2. 创建一条 edge_services 条目（host=子域名，target=overlay_ip:port）；
//  3. 由 edge 数据面按 Host 反代到 overlay 目标。
//
// 排除带宽套餐：所有访问同等对待，不做带宽/流量计费与限速。
package ddnsto

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/store"
	"github.com/iflyelf/iflygo/internal/svc"
)

// domain 返回 DDNSTO 根域名（环境变量 DDNSTO_DOMAIN，默认 ddnsto.local）。
func domain() string {
	if v := os.Getenv("DDNSTO_DOMAIN"); v != "" {
		return strings.TrimRight(v, ".")
	}
	return "ddnsto.local"
}

// Access 远程访问条目视图（关联客户端）。
type Access struct {
	ID         int64  `json:"id"`
	ClientID   int64  `json:"client_id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Target     string `json:"target"`
	Protocol   string `json:"protocol"`
	PublicPort int    `json:"public_port"`
	Enabled    bool   `json:"enabled"`
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

// slug 把客户端名转为子域名安全片段（小写、去非法字符）。
func slug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "host"
	}
	return s
}

// RegisterRoutes 注册 DDNSTO 远程访问路由。
func RegisterRoutes(mux *http.ServeMux, sc *svc.ServiceContext) {
	mux.HandleFunc("GET /api/ddnsto/access", ListAccess(sc))
	mux.HandleFunc("POST /api/ddnsto/access", CreateAccess(sc))
	mux.HandleFunc("DELETE /api/ddnsto/access/{id}", DeleteAccess(sc))
}

// ListAccess 列出远程访问条目（host 以 .<domain> 结尾的 edge_services）。
func ListAccess(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		suffix := "." + domain()
		rows, err := sc.Store.DB.Query(store.SQL(`
			SELECT e.id, e.name, e.host, e.target, e.protocol, e.public_port, e.enabled,
			       c.id AS client_id
			FROM edge_services e
			LEFT JOIN client_nodes c ON e.target LIKE c.ipv4 || ':%'
			WHERE e.host LIKE '%' || $1
			ORDER BY e.id`), suffix)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		out := []Access{}
		for rows.Next() {
			var a Access
			var clientID *int64
			var enabled int
			if err := rows.Scan(&a.ID, &a.Name, &a.Host, &a.Target, &a.Protocol, &a.PublicPort, &enabled, &clientID); err != nil {
				continue
			}
			if clientID != nil {
				a.ClientID = *clientID
			}
			a.Enabled = enabled != 0
			out = append(out, a)
		}
		httpx.OK(w, out)
	}
}

// CreateAccess 给客户端开远程访问：自动分配子域名 + 创建 edge_services。
func CreateAccess(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClientID int64  `json:"client_id"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		}
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.ClientID == 0 {
			httpx.Fail(w, http.StatusBadRequest, "client_id 必填")
			return
		}
		if req.Port == 0 {
			req.Port = 80
		}
		if req.Protocol == "" {
			req.Protocol = "http"
		}
		// 查询客户端 overlay IP
		var ipv4, name string
		var netID int64
		err := sc.Store.DB.QueryRow(store.SQL(`SELECT ipv4, name, network_id FROM client_nodes WHERE id=$1`), req.ClientID).
			Scan(&ipv4, &name, &netID)
		if err != nil || ipv4 == "" {
			httpx.Fail(w, http.StatusBadRequest, "客户端不存在或未分配 overlay IP")
			return
		}
		// 生成唯一子域名
		host, err := uniqueHost(sc, slug(name), netID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		target := fmt.Sprintf("%s:%d", ipv4, req.Port)
		now := nowStr()
		var id int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`
			INSERT INTO edge_services (network_id, name, host, target, protocol, tls_mode, public_port, enabled, created_at)
			VALUES ($1,$2,$3,$4,$5,'terminate',$6,1,$7) RETURNING id`),
			netID, name+" 远程访问", host, target, req.Protocol, req.Port, now).Scan(&id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, Access{ID: id, ClientID: req.ClientID, Name: name, Host: host, Target: target, Protocol: req.Protocol, PublicPort: req.Port, Enabled: true})
	}
}

// DeleteAccess 删除远程访问条目。
func DeleteAccess(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		_, _ = fmt.Sscanf(r.PathValue("id"), "%d", &id)
		if id == 0 {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		// 仅删除 DDNSTO 域下的条目，避免误删手工 edge 服务
		suffix := "." + domain()
		if _, err := sc.Store.DB.Exec(store.SQL(`DELETE FROM edge_services WHERE id=$1 AND host LIKE '%' || $2`), id, suffix); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// uniqueHost 生成唯一子域名：<slug>.<domain>，冲突则追加随机后缀。
func uniqueHost(sc *svc.ServiceContext, slug string, netID int64) (string, error) {
	dom := domain()
	base := slug
	for i := 0; i < 5; i++ {
		host := base + "." + dom
		var n int
		err := sc.Store.DB.QueryRow(store.SQL(`SELECT COUNT(*) FROM edge_services WHERE host=$1`), host).Scan(&n)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return host, nil
		}
		base = slug + "-" + randSuffix(4)
	}
	return "", fmt.Errorf("无法生成唯一子域名")
}

func randSuffix(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func nowStr() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
