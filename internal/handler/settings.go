package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/iflyelf/iflygo/internal/pkg/audit"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/setting"
	"github.com/iflyelf/iflygo/internal/svc"
)

// ListSettings 返回全部可配置项（secret 脱敏）。
func ListSettings(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.OK(w, s.Settings.View())
	}
}

// UpdateSettings 保存设置并即时生效；涉及 Consul 的变更触发热重载。
func UpdateSettings(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var kv map[string]string
		if err := httpx.DecodeJSON(r, &kv); err != nil {
			httpx.Fail(w, http.StatusBadRequest, "请求格式错误")
			return
		}
		before := s.Settings.Snapshot()
		applied, err := s.Settings.Apply(r.Context(), kv)
		if err != nil {
			auditOp(s, r, audit.ActionSettingUpdate, "setting", "", "", err)
			httpx.FailErr(w, http.StatusInternalServerError, err)
			return
		}
		after := s.Settings.Snapshot()
		auditOp(s, r, audit.ActionSettingUpdate, "setting", "", strings.Join(applied, ","), nil)
		// 开启自动同步后，external_url 变化（或刚开启开关）时自动写入 Agent 连接地址覆盖，
		// 下一次心跳切换并触发主机侧自检；手工覆盖优先。
		maybeSyncExternalURLToAgents(r.Context(), s,
			before.Server.AutoSyncAgentURL, after.Server.AutoSyncAgentURL,
			before.Server.ExternalURL, after.Server.ExternalURL)
		if setting.NeedsConsulReload(applied) {
			s.ReloadConsul()
		}
		if setting.NeedsCacheReload(applied) {
			s.ReloadCache()
		}
		if setting.NeedsArtifactReload(applied) {
			s.ReloadArtifactSettings()
		}
		if setting.NeedsTCPRelayReload(applied) {
			s.ReloadTCPRelay(context.WithoutCancel(r.Context()))
		}
		// 客户端下发参数变化：递增所有客户端配置版本，下一次心跳自动拉取。
		if settingAffectsClients(applied) {
			_, _ = s.Store.DB.Exec(s.Store.Rebind(`UPDATE client_nodes SET config_rev = config_rev + 1`))
		}
		httpx.OK(w, map[string]any{"applied": applied})
	}
}

// settingAffectsClients 判断设置变更是否需要让客户端重新拉取配置。
func settingAffectsClients(keys []string) bool {
	for _, k := range keys {
		if strings.HasPrefix(k, "client.") || k == "tcp_relay.mode" {
			return true
		}
	}
	return false
}

// RevealSetting 返回密钥项的完整值（仅 superadmin；每次查看写审计）。
//
// 列表接口默认脱敏为 ******，避免密钥出现在页面快照/日志；需要明文时由
// 用户显式点击「显示完整值」，服务端记审计后返回。
func RevealSetting(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.PathValue("key"))
		v, err := s.Settings.RevealSecret(key)
		if err != nil {
			auditOp(s, r, audit.ActionSettingReveal, "setting", key, key, err)
			httpx.Fail(w, http.StatusBadRequest, err.Error())
			return
		}
		auditOp(s, r, audit.ActionSettingReveal, "setting", key, key, nil)
		httpx.OK(w, map[string]any{"key": key, "value": v})
	}
}

// ResetSetting 删除页面保存的值，恢复为配置文件/环境变量/默认值：DELETE /api/settings/{key}。
//
// 页面设置优先级最高；不提供恢复入口时，一旦在页面保存过，之后修改配置文件将不再生效。
func ResetSetting(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.PathValue("key"))
		before := s.Settings.Snapshot()
		if err := s.Settings.Reset(r.Context(), key); err != nil {
			auditOp(s, r, audit.ActionSettingReset, "setting", key, key, err)
			httpx.FailErr(w, http.StatusBadRequest, err)
			return
		}
		after := s.Settings.Snapshot()
		auditOp(s, r, audit.ActionSettingReset, "setting", key, key, nil)
		maybeSyncExternalURLToAgents(r.Context(), s,
			before.Server.AutoSyncAgentURL, after.Server.AutoSyncAgentURL,
			before.Server.ExternalURL, after.Server.ExternalURL)
		applied := []string{key}
		if setting.NeedsConsulReload(applied) {
			s.ReloadConsul()
		}
		if setting.NeedsCacheReload(applied) {
			s.ReloadCache()
		}
		if setting.NeedsArtifactReload(applied) {
			s.ReloadArtifactSettings()
		}
		if setting.NeedsTCPRelayReload(applied) {
			s.ReloadTCPRelay(context.WithoutCancel(r.Context()))
		}
		base, _ := s.Settings.BaseValue(key)
		httpx.OK(w, map[string]any{"key": key, "value": base})
	}
}
