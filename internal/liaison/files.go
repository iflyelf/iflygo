package liaison

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
)

// 文件管理：默认针对服务端本机文件系统（后续可扩展为 SSH SFTP 远程主机）。
// 根目录受限于可配置的 root；路径统一做 Clean + 前缀校验，防目录穿越。

// fileRoot 返回文件管理根目录（可通过环境变量 LIAISON_FS_ROOT 覆盖，默认 /）。
func fileRoot() string {
	if v := os.Getenv("LIAISON_FS_ROOT"); v != "" {
		return filepath.Clean(v)
	}
	return "/"
}

// safePath 将请求的 path 解析到根目录内，返回绝对路径；越界返回错误。
func safePath(p string) (string, error) {
	root := fileRoot()
	clean := filepath.Clean("/" + strings.TrimPrefix(p, "/"))
	abs := filepath.Join(root, clean)
	// 用相对路径判断越界：rel 以 .. 开头即越界（对 root="/" 也正确）
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errPathEscape
	}
	return abs, nil
}

var errPathEscape = &liErr{"路径越界"}

type liErr struct{ msg string }

func (e *liErr) Error() string { return e.msg }

// handleFilesList 列目录。
func (s *Service) handleFilesList(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "files:*", ActRead) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	p := r.URL.Query().Get("path")
	abs, err := safePath(p)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	type item struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Dir   bool   `json:"dir"`
		Size  int64  `json:"size"`
		MTime int64  `json:"mtime"`
	}
	out := make([]item, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("/", strings.TrimPrefix(p, "/"), e.Name()))
		out = append(out, item{Name: e.Name(), Path: rel, Dir: e.IsDir(), Size: info.Size(), MTime: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	httpx.OK(w, out)
}

// handleFileContent 读取文件内容（文本）。
func (s *Service) handleFileContent(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "files:*", ActRead) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	p := r.URL.Query().Get("path")
	abs, err := safePath(p)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.OK(w, string(b))
}

// handleFileWrite 写文件内容（文本）。
func (s *Service) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "files:*", ActWrite) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "请求体非法")
		return
	}
	abs, err := safePath(req.Path)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.WriteFile(abs, []byte(req.Content), 0o644); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}

// handleFileUpload 上传文件（二进制流）。
func (s *Service) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "files:*", ActWrite) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	p := r.URL.Query().Get("path")
	abs, err := safePath(p)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	dst, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, r.Body); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}

// handleFileDelete 删除文件/目录。
func (s *Service) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	_, u := subjectOf(r)
	if u == nil || !s.authorizeUser(u, "files:*", ActWrite) {
		httpx.Fail(w, http.StatusForbidden, "无权限")
		return
	}
	p := r.URL.Query().Get("path")
	abs, err := safePath(p)
	if err != nil {
		httpx.Fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.OK(w, nil)
}
