package liaison

import (
	"context"
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/ai/llm"
	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

// LLM 网关：OpenAI 兼容的对话代理，复用 upgrade_mgr 的 LLM 配置（设置 → AI 助手）。
// 供第三方应用经 iflygo 统一访问 LLM，受 Casbin 鉴权（sub 对 llm:chat 执行 exec）。

// handleLLMModels 返回当前配置的模型。
func (s *Service) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "llm:*", ActRead) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	if s.settings == nil {
		httpx.Fail(w, http.StatusServiceUnavailable, "LLM 未配置")
		return
	}
	ai := s.settings().AI
	httpx.OK(w, map[string]any{"model": ai.LLMModel, "base_url": ai.LLMBaseURL})
}

// handleLLMChat 转发对话请求。
func (s *Service) handleLLMChat(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "llm:chat", ActExec) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	if s.settings == nil {
		httpx.Fail(w, http.StatusServiceUnavailable, "LLM 未配置")
		return
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || len(req.Messages) == 0 {
		httpx.Fail(w, http.StatusBadRequest, "messages 必填")
		return
	}
	ai := s.settings().AI
	if ai.LLMBaseURL == "" || ai.LLMModel == "" {
		httpx.Fail(w, http.StatusServiceUnavailable, "未配置 LLM 地址或模型")
		return
	}
	msgs := make([]llm.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	client := llm.New(llm.Config{
		BaseURL:      ai.LLMBaseURL,
		APIKey:       ai.LLMAPIKey,
		Model:        ai.LLMModel,
		AuthHeader:   ai.LLMAuthHeader,
		AuthScheme:   ai.LLMAuthScheme,
		Temperature:  ai.LLMTemperature,
		MaxTokens:    ai.LLMMaxTokens,
		Timeout:      parseDurDefault(ai.LLMTimeout, 120*time.Second),
		MaxRespBytes: ai.LLMMaxResponseBytes,
	})
	ctx, cancel := context.WithTimeout(r.Context(), parseDurDefault(ai.LLMTimeout, 120*time.Second))
	defer cancel()
	resp, err := client.Chat(ctx, msgs, nil)
	if err != nil {
		httpx.Fail(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.OK(w, map[string]any{
		"content": resp.Message.Content, "finish_reason": resp.FinishReason,
		"usage": resp.Usage,
	})
}

func parseDurDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
