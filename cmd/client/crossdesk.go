package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

type crossDeskCLI struct {
	base   string
	client *http.Client
}

func newCrossDeskCLI(server, user, pass string, insecure bool) (*crossDeskCLI, error) {
	base := strings.TrimRight(strings.TrimSpace(server), "/")
	if base == "" {
		return nil, fmt.Errorf("--server 必填")
	}
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 用户显式选择
	}
	c := &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: tr}
	if user != "" || pass != "" {
		body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
		req, _ := http.NewRequest(http.MethodPost, base+"/api/auth/login", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("登录失败: %s", strings.TrimSpace(string(b)))
		}
	}
	return &crossDeskCLI{base: base, client: c}, nil
}

func (c *crossDeskCLI) get(path string) (any, error) {
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	return c.do(req)
}

func (c *crossDeskCLI) post(path, body string) (any, error) {
	req, _ := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *crossDeskCLI) do(req *http.Request) (any, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("%s", env.Message)
	}
	var out any
	_ = json.Unmarshal(env.Data, &out)
	return out, nil
}

func cmdCrossDesk(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: iflygo-client crossdesk <status|devices|connect|deskhub> [flags]")
		os.Exit(2)
	}
	action := args[0]
	fs := flag.NewFlagSet("crossdesk "+action, flag.ExitOnError)
	server := fs.String("server", "", "控制面地址，如 https://iflygo.example.com")
	user := fs.String("username", "", "登录用户名（管理员/授权用户）")
	pass := fs.String("password", "", "登录密码")
	insecure := fs.Bool("insecure", false, "跳过 TLS 证书校验（仅测试）")
	device := fs.String("device", "", "CrossDesk 设备 ID（connect）")
	devices := fs.String("devices", "", "逗号分隔设备 ID（deskhub）")
	fs.Parse(args[1:])

	cli, err := newCrossDeskCLI(*server, *user, *pass, *insecure)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	switch action {
	case "status":
		data, err := cli.get("/api/crossdesk/status")
		printResult(data, err)
	case "devices":
		data, err := cli.get("/api/crossdesk/devices")
		printResult(data, err)
	case "connect":
		if strings.TrimSpace(*device) == "" {
			fmt.Fprintln(os.Stderr, "❌ --device 必填")
			os.Exit(2)
		}
		data, err := cli.post("/api/crossdesk/devices/"+url.PathEscape(*device)+"/join-ticket", "{}")
		printResult(data, err)
	case "deskhub":
		ids := splitComma(*devices)
		if len(ids) == 0 {
			fmt.Fprintln(os.Stderr, "❌ --devices 必填")
			os.Exit(2)
		}
		body, _ := json.Marshal(map[string]any{"device_ids": ids})
		data, err := cli.post("/api/crossdesk/deskhub/tickets", string(body))
		printResult(data, err)
	default:
		fmt.Fprintf(os.Stderr, "❌ 未知子命令 %s\n", action)
		os.Exit(2)
	}
}

func printResult(data any, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
}

func splitComma(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
