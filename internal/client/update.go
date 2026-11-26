package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/iflyelf/iflygo/internal/agent/binswap"
	"github.com/iflyelf/iflygo/internal/agent/downloader"
	"github.com/iflyelf/iflygo/internal/version"
)

// UpdateInfo 服务端返回的客户端升级信息。
type UpdateInfo struct {
	Enabled bool   `json:"enabled"`
	Update  bool   `json:"update"`
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

// ErrRestartRequired Windows 无法原地重启时返回，由服务管理器拉起新进程。
var ErrRestartRequired = errors.New("客户端已更新，需要重启")

// UpdateExitCode 自升级后的退出码：Windows SCM / 计划任务据此拉起新版本。
const UpdateExitCode = 75

// CheckUpdate 向控制面查询当前平台是否有新版本。
func CheckUpdate(ctx context.Context, server, current, goos, goarch string) (*UpdateInfo, error) {
	return checkUpdate(ctx, server, current, goos, goarch, false)
}

// CheckUpdateForce 强制查询（即使版本相同也返回下载信息）。
func CheckUpdateForce(ctx context.Context, server, current, goos, goarch string) (*UpdateInfo, error) {
	return checkUpdate(ctx, server, current, goos, goarch, true)
}

func checkUpdate(ctx context.Context, server, current, goos, goarch string, force bool) (*UpdateInfo, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("控制面地址为空")
	}
	q := url.Values{}
	q.Set("os", goos)
	q.Set("arch", goarch)
	if current != "" {
		q.Set("version", current)
	}
	if force {
		q.Set("force", "1")
	}
	var info UpdateInfo
	if err := doJSON(ctx, "GET", server+"/api/client/update?"+q.Encode(), "", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ApplyUpdate 下载并原子替换当前二进制，不负责重启。
func ApplyUpdate(ctx context.Context, info *UpdateInfo, binPath, stateDir string) error {
	if info == nil || !info.Update || info.URL == "" {
		return fmt.Errorf("没有可用的更新")
	}
	if len(info.SHA256) != 64 {
		return fmt.Errorf("升级清单缺少有效 SHA256")
	}
	if u, err := url.Parse(info.URL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("升级下载地址无效")
	}
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = resolved
	}
	infoStat, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("当前二进制不可用: %w", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(binPath), ".iflygo-client-update-")
	if err != nil {
		return fmt.Errorf("创建升级临时目录失败: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	dest := filepath.Join(tmpDir, "iflygo-client.new")
	if err := downloader.Download(ctx, downloader.Spec{
		URL: info.URL, Dest: dest, SHA256: info.SHA256,
		MaxRetries: 3, Timeout: 10 * time.Minute,
	}); err != nil {
		return fmt.Errorf("下载新版本失败: %w", err)
	}
	if err := os.Chmod(dest, infoStat.Mode().Perm()|0o111); err != nil {
		return err
	}
	backupDir := filepath.Join(stateDir, "backup")
	if _, err := binswap.Backup(binPath, backupDir); err != nil {
		return fmt.Errorf("备份当前版本失败: %w", err)
	}
	if err := binswap.Replace(dest, binPath); err != nil {
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	binswap.CleanupOld(binPath)
	return nil
}

// AutoUpdateLoop 周期性检查并应用升级；Unix 上成功后 exec 重启，Windows 返回 ErrRestartRequired。
func AutoUpdateLoop(ctx context.Context, server, stateDir string, interval time.Duration, logger *log.Logger) error {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	if logger == nil {
		logger = log.Default()
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		binPath, err := os.Executable()
		if err != nil {
			logger.Printf("⚠️ 自动升级: 无法定位当前二进制: %v", err)
		} else if info, err := CheckUpdate(ctx, server, version.Version, runtime.GOOS, runtime.GOARCH); err != nil {
			logger.Printf("⚠️ 自动升级检查失败: %v", err)
		} else if info.Update {
			logger.Printf("⬆️ 发现新版本 %s，开始升级", info.Version)
			if err := ApplyUpdate(ctx, info, binPath, stateDir); err != nil {
				logger.Printf("⚠️ 自动升级失败: %v", err)
			} else {
				logger.Printf("✅ 客户端已升级到 %s，准备重启", info.Version)
				if err := RestartForUpdate(binPath); err != nil {
					if errors.Is(err, ErrRestartRequired) {
						os.Exit(UpdateExitCode)
					}
					return err
				}
				return nil
			}
		}
		timer.Reset(interval)
	}
}
