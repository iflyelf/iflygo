package liaison

import (
	"net/http"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

// 策略管理（仅 superadmin 通过 middleware.RequireSuperAdmin 到达）。

// handlePoliciesList 列出全部策略（p + g）。
func (s *Service) handlePoliciesList(w http.ResponseWriter, r *http.Request) {
	policies, err := s.rbac.enforcer.GetPolicy()
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	groupings, _ := s.rbac.enforcer.GetGroupingPolicy()
	httpx.OK(w, map[string]any{"policies": policies, "groupings": groupings})
}

// handlePolicyAdd 添加一条策略 {sub, obj, act}。
func (s *Service) handlePolicyAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sub string `json:"sub"`
		Obj string `json:"obj"`
		Act string `json:"act"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Sub == "" || req.Obj == "" || req.Act == "" {
		httpx.Fail(w, http.StatusBadRequest, "sub/obj/act 必填")
		return
	}
	if _, err := s.rbac.enforcer.AddPolicy(req.Sub, req.Obj, req.Act); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}

// handlePolicyRemove 删除一条策略 {sub, obj, act}。
func (s *Service) handlePolicyRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sub string `json:"sub"`
		Obj string `json:"obj"`
		Act string `json:"act"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Sub == "" || req.Obj == "" || req.Act == "" {
		httpx.Fail(w, http.StatusBadRequest, "sub/obj/act 必填")
		return
	}
	if _, err := s.rbac.enforcer.RemovePolicy(req.Sub, req.Obj, req.Act); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}
