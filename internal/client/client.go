package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/iflyelf/iflygo/internal/model"
)

// Client 控制面 API 客户端。
type Client struct {
	Server   string
	ClientID int64
	Secret   string
	HTTP     *http.Client
}

// RegInfo Agent 注册信息。
type RegInfo struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Kernel  string `json:"kernel"`
	Version string `json:"version"`
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// New 创建客户端。
func New(server string, id int64, secret string) *Client {
	return &Client{
		Server:   server,
		ClientID: id,
		Secret:   secret,
		HTTP:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) bearer() string {
	return fmt.Sprintf("%d:%s", c.ClientID, c.Secret)
}

// Register 用一次性 token 注册。
func Register(ctx context.Context, server, token string, info RegInfo) (id int64, secret string, bundle *model.Bundle, err error) {
	var out struct {
		ClientID int64        `json:"client_id"`
		Secret   string       `json:"secret"`
		Bundle   model.Bundle `json:"bundle"`
	}
	err = doJSON(ctx, http.MethodPost, server+"/api/agent/register", "", map[string]string{
		"enroll_token": token,
		"os":           info.OS,
		"arch":         info.Arch,
		"kernel":       info.Kernel,
		"version":      info.Version,
	}, &out)
	return out.ClientID, out.Secret, &out.Bundle, err
}

// Heartbeat 心跳，返回服务端配置版本。
func (c *Client) Heartbeat(ctx context.Context) (int64, error) {
	var out struct {
		ConfigRev int64  `json:"config_rev"`
		ServerURL string `json:"server_url"`
	}
	if err := doJSON(ctx, http.MethodPost, c.Server+"/api/agent/heartbeat", c.bearer(), nil, &out); err != nil {
		return 0, err
	}
	return out.ConfigRev, nil
}

// Config 拉取配置包。
func (c *Client) Config(ctx context.Context) (*model.Bundle, error) {
	var out model.Bundle
	if err := doJSON(ctx, http.MethodGet, c.Server+"/api/agent/config", c.bearer(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// doJSON 通用 JSON 请求。
func doJSON(ctx context.Context, method, url, bearer string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("%s", env.Message)
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}
