package handler

import (
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/pki"
	"github.com/iflyelf/iflygo/internal/svc"
)

// RenewCert 用相同密钥重新签发客户端证书。
func RenewCert(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		net, err := getNetwork(sc, row.NetworkID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		caCert, caKey, err := getCA(sc, net.ID)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}

		certPEM, err := pki.IssueNodeCert(
			&pki.CA{CertPEM: caCert, KeyPEM: caKey},
			&pki.NodeKey{PublicKeyPEM: row.PublicKey, PrivateKeyPEM: row.KeyPEM},
			row.Name, nodePrefixes(net, row.IPv4, row.IPv6), parsePrefixes(row.Subnets), splitList(row.Groups),
			3*365*24*time.Hour)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET cert_pem = $1, updated_at = $2 WHERE id = $3`), certPEM, nowStr(), id); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		bumpConfigRev(sc, row.NetworkID)
		httpx.OK(w, nil)
	}
}

// RevokeCert 吊销客户端证书（加入 blocklist）。
func RevokeCert(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		row, err := getClientRow(sc, id)
		if err != nil {
			httpx.Fail(w, http.StatusNotFound, "客户端不存在")
			return
		}
		fp, _, err := pki.ParseCert(row.CertPEM)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := sc.Store.DB.Exec(sc.Store.Rebind(`INSERT INTO cert_revocations (network_id,fingerprint,reason,created_at) VALUES ($1,$2,$3,$4)`),
			row.NetworkID, fp, "revoked", nowStr()); err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		bumpConfigRev(sc, row.NetworkID)
		httpx.OK(w, nil)
	}
}

// ListRevocations 列出网络的吊销指纹。
func ListRevocations(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nid, err := tunnelID(r)
		if err != nil {
			httpx.Fail(w, http.StatusBadRequest, "id 非法")
			return
		}
		fps, err := loadRevocations(sc, nid)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, fps)
	}
}

// loadRevocations 读取网络的吊销指纹列表。
func loadRevocations(sc *svc.ServiceContext, networkID int64) ([]string, error) {
	rows, err := sc.Store.DB.Query(sc.Store.Rebind(`SELECT fingerprint FROM cert_revocations WHERE network_id = $1 ORDER BY id`), networkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return nil, err
		}
		out = append(out, fp)
	}
	return out, rows.Err()
}

// bumpConfigRev 递增网络下所有客户端的配置版本，触发重新拉取。
func bumpConfigRev(sc *svc.ServiceContext, networkID int64) {
	_, _ = sc.Store.DB.Exec(sc.Store.Rebind(`UPDATE client_nodes SET config_rev = config_rev + 1 WHERE network_id = $1`), networkID)
}
