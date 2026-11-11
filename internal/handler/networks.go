package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/iflyelf/iflygo/internal/svc"
)

type networkReq struct {
	Name     string `json:"name"`
	Cidr4    string `json:"cidr4"`
	Cidr6    string `json:"cidr6"`
	Gateway4 string `json:"gateway4"`
	Gateway6 string `json:"gateway6"`
	DNS      string `json:"dns"`
	MTU      int    `json:"mtu"`
	// DNSGateway 显式 DNS 网关（overlay IP，可选）。
	DNSGateway string `json:"dns_gateway"`
	// EgressMode 出口模式：off / dns / full。
	EgressMode string `json:"egress_mode"`
	// EgressExclude 全量出口排除网段（逗号分隔 CIDR）。
	EgressExclude string               `json:"egress_exclude"`
	Nebula        model.NebulaSettings `json:"nebula"`
}

// ListNetworks 列出网络。
func ListNetworks(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT id,name,cidr4,cidr6,gateway4,gateway6,dns,mtu,dns_gateway,egress_mode,egress_exclude,created_at FROM networks ORDER BY id`))
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		list := []model.Network{}
		for rows.Next() {
			var n model.Network
			if err := rows.Scan(&n.ID, &n.Name, &n.Cidr4, &n.Cidr6, &n.Gateway4, &n.Gateway6, &n.DNS, &n.MTU, &n.DNSGateway, &n.EgressMode, &n.EgressExclude, &n.CreatedAt); err != nil {
				httpx.FailErr(w, http.StatusInternalServerError, err)
				return
			}
			list = append(list, n)
		}
		httpx.OK(w, list)
	}
}

// CreateNetwork 创建网络并生成其 CA。
func CreateNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req networkReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.Name == "" || req.Cidr4 == "" {
			httpx.Fail(w, http.StatusBadRequest, "name 与 cidr4 必填")
			return
		}
		if req.MTU == 0 {
			req.MTU = 1300
			if sc.Settings != nil {
				if m := sc.Settings.Snapshot().Client.MTU; m > 0 {
					req.MTU = m
				}
			}
		}

		ca, err := pki.CreateCA("iFlyGo CA ("+req.Name+")", 10*365*24*time.Hour)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		now := nowStr()
		nebulaJSON, err := json.Marshal(req.Nebula)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		var nid int64
		err = sc.Store.DB.QueryRow(sc.Store.Rebind(`INSERT INTO networks (name,cidr4,cidr6,gateway4,gateway6,dns,mtu,dns_gateway,egress_mode,egress_exclude,nebula,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`),
			req.Name, req.Cidr4, req.Cidr6, req.Gateway4, req.Gateway6, req.DNS, req.MTU, req.DNSGateway, req.EgressMode, req.EgressExclude, string(nebulaJSON), now).Scan(&nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO cas (network_id,name,cert_pem,key_pem,fingerprint,expires_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`),
			nid, ca.Name, ca.CertPEM, ca.KeyPEM, ca.Fingerprint, ca.ExpiresAt.UTC().Format(time.RFC3339), now); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		httpx.OK(w, model.Network{ID: nid, Name: req.Name, Cidr4: req.Cidr4, Cidr6: req.Cidr6, Gateway4: req.Gateway4, Gateway6: req.Gateway6, DNS: req.DNS, MTU: req.MTU, DNSGateway: req.DNSGateway, EgressMode: req.EgressMode, EgressExclude: req.EgressExclude, CreatedAt: now})
	}
}

// DeleteNetwork 删除网络（级联删除 CA/节点/地址）。
func DeleteNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`DELETE FROM networks WHERE id = $1`), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, nil)
	}
}

// getNetwork 读取网络。
func getNetwork(sc *svc.ServiceContext, id int64) (*model.Network, error) {
	var n model.Network
	q := sc.Store.Rebind(`SELECT id,name,cidr4,cidr6,gateway4,gateway6,dns,mtu,dns_gateway,egress_mode,egress_exclude,created_at FROM networks WHERE id = $1`)
	if err := sc.Store.DB.QueryRow(q, id).Scan(&n.ID, &n.Name, &n.Cidr4, &n.Cidr6, &n.Gateway4, &n.Gateway6, &n.DNS, &n.MTU, &n.DNSGateway, &n.EgressMode, &n.EgressExclude, &n.CreatedAt); err != nil {
		return nil, err
	}
	return &n, nil
}

// getCA 读取网络的 CA。
func getCA(sc *svc.ServiceContext, networkID int64) (certPEM, keyPEM string, err error) {
	q := sc.Store.Rebind(`SELECT cert_pem, key_pem FROM cas WHERE network_id = $1 ORDER BY id DESC LIMIT 1`)
	err = sc.Store.DB.QueryRow(q, networkID).Scan(&certPEM, &keyPEM)
	return
}

// getNebulaSettings 读取网络的 Nebula 设置。
func getNebulaSettings(sc *svc.ServiceContext, networkID int64) (*model.NebulaSettings, error) {
	var raw string
	q := sc.Store.Rebind(`SELECT nebula FROM networks WHERE id = $1`)
	if err := sc.Store.DB.QueryRow(q, networkID).Scan(&raw); err != nil {
		return nil, err
	}
	s := &model.NebulaSettings{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func tunnelID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

// networkDetail 网络详情（含 Nebula 设置）。
type networkDetail struct {
	model.Network
	Nebula model.NebulaSettings `json:"nebula"`
}

// GetNetwork 返回网络详情（含 Nebula 设置），供页面编辑。
func GetNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		n, err := getNetwork(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusNotFound, err)
			return
		}
		neb, err := getNebulaSettings(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, networkDetail{Network: *n, Nebula: *neb})
	}
}

type networkSettingsReq struct {
	DNS           string               `json:"dns"`
	MTU           int                  `json:"mtu"`
	DNSGateway    string               `json:"dns_gateway"`
	EgressMode    string               `json:"egress_mode"`
	EgressExclude string               `json:"egress_exclude"`
	Nebula        model.NebulaSettings `json:"nebula"`
}

// UpdateNetwork 更新网络的 DNS/MTU/Nebula 设置，并递增该网络下客户端的配置版本，
// 使客户端在下一次心跳后自动拉取新配置。
func UpdateNetwork(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		existing, err := getNetwork(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusNotFound, err)
			return
		}
		var req networkSettingsReq
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求体非法")
			return
		}
		if req.MTU == 0 {
			req.MTU = existing.MTU
		}
		if req.MTU < 576 || req.MTU > 9000 {
			httpx.Fail(w, http.StatusBadRequest, "MTU 应在 576-9000 之间")
			return
		}
		switch strings.ToLower(strings.TrimSpace(req.Nebula.LogLevel)) {
		case "", "error", "warn", "warning", "info", "debug":
		default:
			httpx.Fail(w, http.StatusBadRequest, "log_level 只能是 error/warn/info/debug")
			return
		}
		switch strings.ToLower(strings.TrimSpace(req.Nebula.StatsType)) {
		case "", "prometheus", "graphite":
		default:
			httpx.Fail(w, http.StatusBadRequest, "stats_type 只能是 prometheus/graphite")
			return
		}
		switch strings.ToLower(strings.TrimSpace(req.Nebula.Cipher)) {
		case "", "aes-256-gcm", "chacha20-poly1305":
		default:
			httpx.Fail(w, http.StatusBadRequest, "cipher 只能是 aes-256-gcm / chacha20-poly1305（留空用默认）")
			return
		}
		req.DNSGateway = strings.TrimSpace(req.DNSGateway)
		if req.DNSGateway != "" {
			addr, aerr := netip.ParseAddr(req.DNSGateway)
			if aerr != nil {
				httpx.Fail(w, http.StatusBadRequest, "DNS 网关不是合法 IP: "+req.DNSGateway)
				return
			}
			if !prefixContains(existing.Cidr4, addr) && !prefixContains(existing.Cidr6, addr) {
				httpx.Fail(w, http.StatusBadRequest, "DNS 网关必须在该网络的 overlay 网段内")
				return
			}
		}
		req.EgressMode = strings.ToLower(strings.TrimSpace(req.EgressMode))
		switch req.EgressMode {
		case "", "off", "dns", "full":
		default:
			httpx.Fail(w, http.StatusBadRequest, "egress_mode 只能是 off / dns / full")
			return
		}
		req.EgressExclude = strings.TrimSpace(req.EgressExclude)
		for _, c := range splitList(req.EgressExclude) {
			if _, err := netip.ParsePrefix(c); err != nil {
				httpx.Fail(w, http.StatusBadRequest, "排除网段不是合法 CIDR: "+c)
				return
			}
		}
		switch strings.ToLower(strings.TrimSpace(req.Nebula.LogFormat)) {
		case "", "text", "json":
		default:
			httpx.Fail(w, http.StatusBadRequest, "log_format 只能是 text / json")
			return
		}
		if req.Nebula.ListenPort != 0 && (req.Nebula.ListenPort < 1 || req.Nebula.ListenPort > 65535) {
			httpx.Fail(w, http.StatusBadRequest, "listen_port 应在 1-65535 之间")
			return
		}
		if req.Nebula.LighthouseInterval < 0 || req.Nebula.HandshakeRetries < 0 {
			httpx.Fail(w, http.StatusBadRequest, "lighthouse_interval / handshake_retries 不能为负数")
			return
		}
		for _, d := range strings.Split(req.DNS, ",") {
			if d = strings.TrimSpace(d); d != "" && net.ParseIP(d) == nil {
				httpx.Fail(w, http.StatusBadRequest, "DNS 含非法 IP: "+d)
				return
			}
		}
		for _, r := range req.Nebula.PreferredRanges {
			if r = strings.TrimSpace(r); r == "" {
				continue
			}
			if _, err := netip.ParsePrefix(r); err != nil {
				httpx.Fail(w, http.StatusBadRequest, "preferred_ranges 含非法 CIDR: "+r)
				return
			}
		}
		for _, r := range req.Nebula.Relays {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			if _, _, err := net.SplitHostPort(r); err == nil || net.ParseIP(r) != nil {
				continue
			}
			httpx.Fail(w, http.StatusBadRequest, "relays 条目应为 host:port 或 overlay IP: "+r)
			return
		}
		if req.Nebula.PunchDelay != "" {
			if _, err := time.ParseDuration(req.Nebula.PunchDelay); err != nil {
				httpx.Fail(w, http.StatusBadRequest, "punch_delay 不是合法时长: "+req.Nebula.PunchDelay)
				return
			}
		}
		if req.Nebula.HandshakeTryInterval != "" {
			if _, err := time.ParseDuration(req.Nebula.HandshakeTryInterval); err != nil {
				httpx.Fail(w, http.StatusBadRequest, "handshake_try_interval 不是合法时长: "+req.Nebula.HandshakeTryInterval)
				return
			}
		}
		nebulaJSON, err := json.Marshal(req.Nebula)
		if err != nil {
			httpx.FailErr(w, http.StatusBadRequest, err)
			return
		}
		q := sc.Store.Rebind(`UPDATE networks SET dns = $1, mtu = $2, nebula = $3, dns_gateway = $4, egress_mode = $5, egress_exclude = $6 WHERE id = $7`)
		if _, err := sc.Store.DB.Exec(q, req.DNS, req.MTU, string(nebulaJSON), req.DNSGateway, req.EgressMode, req.EgressExclude, id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		// 配置已变化：让该网络下所有客户端下次心跳拉取新配置。
		_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET config_rev = config_rev + 1 WHERE network_id = $1`), id)

		n, err := getNetwork(sc, id)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, networkDetail{Network: *n, Nebula: req.Nebula})
	}
}
