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

// 文件管理的大小上限：防止误读超大文件/上传占满磁盘。
const (
	maxFileRead   = 8 << 20 // 8 MiB，文本编辑
	maxFileWrite  = 8 << 20 // 8 MiB，文本保存
	maxFileUpload = 2 << 30 // 2 GiB，二进制上传
)

// 文件管理：操作 iflygo-server 所在主机的文件系统（LIAISON_FS_ROOT 可限定根目录）。
// 远程主机的文件请通过 Web SSH 终端或独立的 SFTP 客户端访问。
// 路径统一做 Clean + 前缀校验，防目录穿越。

// fileRoot 返回文件管理根目录（可通过环境变量 LIAISON_FS_ROOT 覆盖，默认 /）。
func fileRoot() string {
	if v := os.Getenv("LIAISON_FS_ROOT"); v != "" {
		return filepath.Clean(v)
	}
	return "/"
}

// safePath 将请求的 path 解析到根目录内，返回绝对路径；越界返回错误。
//
// 除字符串层面的 .. 校验外，还会解析路径上已存在部分的符号链接，
// 防止通过 root 内的软链逃逸到根目录之外。
func safePath(p string) (string, error) {
	root := fileRoot()
	// 根目录本身可能是软链（如 /var/www -> /data/www），先解析为真实路径，
	// 否则后续“已存在部分”校验会误判为越界。
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	clean := filepath.Clean("/" + strings.TrimPrefix(p, "/"))
	abs := filepath.Join(root, clean)
	if !pathWithin(root, abs) {
		return "", errPathEscape
	}
	// 解析最近的已存在祖先，确认其真实路径仍在 root 内。
	probe := abs
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !pathWithin(root, resolved) {
				return "", errPathEscape
			}
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return abs, nil
}

func pathWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
	f, err := os.Open(abs)
	if err != nil {
		httpx.Fail(w, http.StatusNotFound, err.Error())
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxFileRead+1))
	if err != nil {
		httpx.Fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(b) > maxFileRead {
		httpx.Fail(w, http.StatusBadRequest, "文件过大，请使用 SSH/SFTP 下载")
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
	r.Body = http.MaxBytesReader(w, r.Body, maxFileWrite)
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
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUpload)
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
