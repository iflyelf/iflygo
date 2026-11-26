package handler

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/iflyelf/iflygo/internal/pkg/httpx"
	"github.com/iflyelf/iflygo/internal/svc"
)

// CrossDeskAgentScript 生成 CrossDesk 主机端一键接入脚本（管理员）。
func CrossDeskAgentScript(sc *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		osName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("os")))
		if osName == "" {
			osName = runtime.GOOS
		}
		if osName != "linux" && osName != "darwin" && osName != "windows" {
			httpx.Fail(w, http.StatusBadRequest, "os 仅支持 linux/darwin/windows")
			return
		}
		arch := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("arch")))
		if arch == "" {
			arch = runtime.GOARCH
		}
		fileRoot := strings.TrimSpace(r.URL.Query().Get("file_root"))
		audio := r.URL.Query().Get("audio") == "1"
		video := r.URL.Query().Get("video") == "1"
		script := buildCrossDeskAgentScript(baseURL(sc, r), osName, arch, fileRoot, audio, video)
		httpx.OK(w, map[string]any{
			"os": osName, "arch": arch, "script": script,
			"file_root": fileRoot, "audio": audio, "video": video,
		})
	}
}
