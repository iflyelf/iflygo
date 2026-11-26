package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iflyelf/iflygo/internal/auth"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// installMonitorTokenKey 永久接入令牌（供一体化安装脚本 / 页面读取）。
const installMonitorTokenKey = "agent.install_monitor_token"

var installMonitorMu sync.Mutex

type installMonitorTokenFile struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"created_at"`
}

func readInstallMonitorToken(ctx context.Context, sc *svc.ServiceContext) (*installMonitorTokenFile, error) {
	if sc == nil || sc.Store == nil {
		return nil, errors.New("数据库未初始化")
	}
	var raw string
	err := sc.Store.DB.QueryRowContext(ctx, sc.Store.Rebind(
		`SELECT value FROM app_settings WHERE key=$1`), installMonitorTokenKey).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var file installMonitorTokenFile
	if err := json.Unmarshal([]byte(raw), &file); err != nil || file.Token == "" || file.ID == 0 {
		return nil, errors.New("安装令牌记录损坏")
	}
	var kind string
	if err := sc.Store.DB.QueryRowContext(ctx, sc.Store.Rebind(
		`SELECT token_type FROM enroll_tokens WHERE id=$1`), file.ID).Scan(&kind); err != nil {
		return nil, err
	}
	if kind != "automation" {
		return nil, errors.New("安装令牌类型已变化")
	}
	return &file, nil
}

func writeInstallMonitorToken(ctx context.Context, sc *svc.ServiceContext, file *installMonitorTokenFile) error {
	data, err := json.Marshal(file)
	if err != nil {
		return err
	}
	_, err = sc.Store.DB.ExecContext(ctx, sc.Store.Rebind(
		`INSERT INTO app_settings (key, value, updated_at) VALUES ($1,$2,CURRENT_TIMESTAMP)
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=CURRENT_TIMESTAMP`),
		installMonitorTokenKey, string(data))
	return err
}

func ensureInstallMonitorToken(ctx context.Context, sc *svc.ServiceContext, r *http.Request, rotate bool) (*installMonitorTokenFile, error) {
	installMonitorMu.Lock()
	defer installMonitorMu.Unlock()
	if !rotate {
		if file, err := readInstallMonitorToken(ctx, sc); err == nil {
			return file, nil
		}
	}
	old, _ := readInstallMonitorToken(ctx, sc)
	tenantID := int64(1)
	creator := "system"
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		if tid := u.TenantID; tid > 0 {
			tenantID = tid
		}
		if u.Username != "" {
			creator = u.Username
		}
	}
	token, prefix := newAutomationToken()
	id, createdAt, err := insertAutomationToken(ctx, sc.Store.DB, token, prefix,
		tenantID, "install-monitor", "install-monitor", creator, nil, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("创建接入令牌失败: %w", err)
	}
	if old != nil && old.ID != 0 && old.ID != id {
		_, _ = sc.Store.DB.ExecContext(ctx, sc.Store.Rebind(
			`DELETE FROM enroll_tokens WHERE id=$1 AND token_type='automation'`), old.ID)
	}
	file := &installMonitorTokenFile{ID: id, Token: token, Prefix: prefix, CreatedAt: createdAt}
	if err := writeInstallMonitorToken(ctx, sc, file); err != nil {
		return nil, fmt.Errorf("保存接入令牌失败: %w", err)
	}
	return file, nil
}

func installMonitorInfo(sc *svc.ServiceContext, r *http.Request, file *installMonitorTokenFile) map[string]any {
	base := strings.TrimRight(baseURL(sc, r), "/")
	return map[string]any{
		"supported":    true,
		"base_url":     base,
		"linux_url":    base + "/api/external/binary/install-monitor.sh",
		"windows_url":  base + "/api/external/binary/install-windows-monitor.exe",
		"token_prefix": file.Prefix,
	}
}

// InstallMonitorTokenGet 返回永久接入令牌（默认纯文本，供安装脚本读取）。
// 该接口与脚本同属内网自动化入口，不要求登录；请用防火墙限制来源。
func InstallMonitorTokenGet(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := ensureInstallMonitorToken(r.Context(), sc, r, false)
		if err != nil {
			http.Error(w, "获取接入令牌失败", http.StatusInternalServerError)
			return
		}
		if strings.EqualFold(r.URL.Query().Get("format"), "json") {
			httpx.OK(w, installMonitorInfo(sc, r, file))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(file.Token + "\n"))
	}
}

// InstallMonitorTokenCreate 轮换永久接入令牌（管理员）。
func InstallMonitorTokenCreate(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file, err := ensureInstallMonitorToken(r.Context(), sc, r, true)
		if err != nil {
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		httpx.OK(w, installMonitorInfo(sc, r, file))
	}
}
