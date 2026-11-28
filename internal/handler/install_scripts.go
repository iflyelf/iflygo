package handler

import (
	"fmt"
	"strings"
)

type clientInstallScripts struct {
	Linux   string `json:"linux"`
	Darwin  string `json:"darwin"`
	Windows string `json:"windows"`
}

// buildClientInstallScripts 生成带 enroll token 的一键接入脚本。
func buildClientInstallScripts(base, token, name string) clientInstallScripts {
	base = strings.TrimRight(base, "/")
	qBase := shellQuote(base)
	qToken := shellQuote(token)
	qName := shellQuote(name)
	linux := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "请使用 root 或 sudo 运行"; exit 1; }
BASE=%s
TOKEN=%s
NAME=%s
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|arm) ARCH=arm ;;
  *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BASE/api/client/download?os=linux&arch=$ARCH&format=raw" -o "$TMP"
else
  wget -qO "$TMP" "$BASE/api/client/download?os=linux&arch=$ARCH&format=raw"
fi
install -m 0755 "$TMP" /usr/local/bin/iflygo-client
iflygo-client enroll --server "$BASE" --token "$TOKEN" --name "$NAME" --state-dir /var/lib/iflygo-client
iflygo-client service install --state-dir /var/lib/iflygo-client --auto-update

# 内置主机监控（独立服务 node_exporter:16666，与 iflygo-client 互不影响）
WITH_MONITOR="${WITH_MONITOR:-1}"
if [ "$WITH_MONITOR" = "1" ]; then
  MON_TMP=$(mktemp)
  if curl -fsSL "$BASE/api/external/binary/node-exporter-install.sh" -o "$MON_TMP"; then
    if UPGRADE_MGR_EXTERNAL_URL="$BASE" NODE_EXPORTER_VERSION=latest \
       NODE_EXPORTER_BASE=127.0.0.1:9 NODE_EXPORTER_PROXY=http:// \
       bash "$MON_TMP" install; then
      echo "✅ 主机监控已安装（node_exporter :16666）"
    else
      echo "⚠️ 主机监控安装失败；iflygo-client 已安装且不受影响"
    fi
  else
    echo "⚠️ 无法获取监控安装脚本；iflygo-client 已安装且不受影响"
  fi
  rm -f "$MON_TMP"
fi

echo "✅ iFlyGo 客户端已安装并启动"
`, qBase, qToken, qName)

	darwin := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "请使用 root 或 sudo 运行"; exit 1; }
BASE=%s
TOKEN=%s
NAME=%s
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  arm64) ARCH=arm64 ;;
  *) echo "不支持的架构: $ARCH"; exit 1 ;;
esac
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$BASE/api/client/download?os=darwin&arch=$ARCH&format=raw" -o "$TMP"
else
  wget -qO "$TMP" "$BASE/api/client/download?os=darwin&arch=$ARCH&format=raw"
fi
install -m 0755 "$TMP" /usr/local/bin/iflygo-client
iflygo-client enroll --server "$BASE" --token "$TOKEN" --name "$NAME" --state-dir /var/lib/iflygo-client
mkdir -p /Library/LaunchDaemons /var/log
cat > /Library/LaunchDaemons/com.iflyelf.iflygo-client.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.iflyelf.iflygo-client</string>
  <key>ProgramArguments</key><array>
    <string>/usr/local/bin/iflygo-client</string><string>run</string>
    <string>--state-dir</string><string>/var/lib/iflygo-client</string>
    <string>--auto-update</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/iflygo-client.log</string>
  <key>StandardErrorPath</key><string>/var/log/iflygo-client.log</string>
</dict></plist>
PLIST
launchctl bootout system /Library/LaunchDaemons/com.iflyelf.iflygo-client.plist >/dev/null 2>&1 || true
launchctl bootstrap system /Library/LaunchDaemons/com.iflyelf.iflygo-client.plist
# macOS 暂未内置监控二进制；如需监控请单独部署，和客户端互不影响。
echo "✅ iFlyGo 客户端已安装并启动"
`, qBase, qToken, qName)

	windows := fmt.Sprintf(`# 以管理员 PowerShell 运行
$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$base = %s
$token = %s
$name = %s
$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "X64" { "amd64" }
  "Arm64" { "arm64" }
  default { throw "不支持的架构" }
}
$dest = Join-Path $env:ProgramFiles "iFlyGo"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$zip = Join-Path $env:TEMP "iflygo-client.zip"
Invoke-WebRequest -UseBasicParsing "$base/api/client/download?os=windows&arch=$arch" -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dest -Force
$exe = Get-ChildItem $dest -Filter "iflygo-client-windows-*.exe" | Select-Object -First 1
if (-not $exe) { throw "未找到 iflygo-client.exe" }
& $exe.FullName enroll --server $base --token $token --name $name --state-dir "$env:ProgramData\iFlyGo"
& $exe.FullName service install --state-dir "$env:ProgramData\iFlyGo" --auto-update

# 内置主机监控（独立服务 windows_exporter:16666，与 iflygo-client 互不影响）
$withMonitor = $true
if ($env:IFLYGO_WITH_MONITOR -eq "0") { $withMonitor = $false }
if ($withMonitor) {
  $mon = Join-Path $env:TEMP "windows-exporter-install.ps1"
  try {
    Invoke-WebRequest -UseBasicParsing "$base/api/external/binary/windows-exporter-install.ps1" -OutFile $mon
    $env:UPGRADE_MGR_EXTERNAL_URL = $base
    & powershell -NoProfile -ExecutionPolicy Bypass -File $mon install
    if ($LASTEXITCODE -ne 0) { throw "监控安装器退出码 $LASTEXITCODE" }
    Write-Host "✅ 主机监控已安装（windows_exporter :16666）"
  } catch {
    Write-Warning "主机监控安装失败；iflygo-client 已安装且不受影响: $_"
  } finally {
    Remove-Item $mon -Force -ErrorAction SilentlyContinue
    Remove-Item Env:UPGRADE_MGR_EXTERNAL_URL -ErrorAction SilentlyContinue
  }
}

Write-Host "✅ iFlyGo 客户端已安装并启动"
`, psQuoteStr(base), psQuoteStr(token), psQuoteStr(name))

	return clientInstallScripts{Linux: linux, Darwin: darwin, Windows: windows}
}

// buildCrossDeskAgentScript 生成 CrossDesk 主机端一键接入脚本。
func buildCrossDeskAgentScript(base, osName, arch, fileRoot string, audio, video bool) string {
	base = strings.TrimRight(base, "/")
	if arch == "" {
		arch = "amd64"
	}
	posixArgs := []string{"--server=" + base, "--credentials=/var/lib/iflygo-crossdesk/device.json", "--auto-update"}
	if fileRoot != "" {
		posixArgs = append(posixArgs, "--file-root="+systemdQuote(fileRoot))
	}
	if audio {
		posixArgs = append(posixArgs, "--audio")
	}
	if video {
		posixArgs = append(posixArgs, "--video-codec", "vp8")
	}
	execLine := "/usr/local/bin/iflygo-client crossdesk host " + strings.Join(posixArgs, " ")

	switch osName {
	case "windows":
		args := []string{"crossdesk", "host", "--server=" + base,
			`--credentials=C:\ProgramData\iFlyGo-CrossDesk\device.json`, "--auto-update"}
		if fileRoot != "" {
			args = append(args, `--file-root="`+strings.ReplaceAll(fileRoot, `"`, `\"`)+`"`)
		}
		if audio {
			args = append(args, "--audio")
		}
		if video {
			args = append(args, "--video-codec", "vp8")
		}
		return fmt.Sprintf(`# 以管理员 PowerShell 运行
$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$arch = %s
$dest = Join-Path $env:ProgramFiles "iFlyGo"
New-Item -ItemType Directory -Force -Path $dest | Out-Null
$exe = Join-Path $dest "iflygo-client.exe"
Invoke-WebRequest -UseBasicParsing "%s/api/client/download?os=windows&arch=$arch&format=raw" -OutFile $exe
$data = Join-Path $env:ProgramData "iFlyGo-CrossDesk"
New-Item -ItemType Directory -Force -Path $data | Out-Null
$arguments = %s
$action = New-ScheduledTaskAction -Execute $exe -Argument $arguments
$trigger = New-ScheduledTaskTrigger -AtStartup
$settings = New-ScheduledTaskSettingsSet -RestartCount 10 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "iFlyGo CrossDesk" -Action $action -Trigger $trigger -Settings $settings -RunLevel Highest -User "SYSTEM" -Force
Start-ScheduledTask -TaskName "iFlyGo CrossDesk"
Write-Host "✅ CrossDesk 主机端已安装并启动"
`, psQuoteStr(arch), base, psQuoteStr(strings.Join(args, " ")))

	case "darwin":
		plist := []string{
			"<string>/usr/local/bin/iflygo-client</string>",
			"<string>crossdesk</string>",
			"<string>host</string>",
		}
		for _, arg := range posixArgs {
			plist = append(plist, "<string>"+xmlEscape(arg)+"</string>")
		}
		return fmt.Sprintf(`#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "请使用 root 或 sudo 运行"; exit 1; }
ARCH=%s
case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  arm64) ARCH=arm64 ;;
  *) echo "不支持的架构"; exit 1 ;;
esac
TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
curl -fsSL "%s/api/client/download?os=darwin&arch=$ARCH&format=raw" -o "$TMP"
install -m 0755 "$TMP" /usr/local/bin/iflygo-client
mkdir -p /var/lib/iflygo-crossdesk
cat > /Library/LaunchDaemons/com.iflyelf.crossdesk.plist <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.iflyelf.crossdesk</string>
  <key>ProgramArguments</key><array>
%s
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/iflygo-crossdesk.log</string>
  <key>StandardErrorPath</key><string>/var/log/iflygo-crossdesk.log</string>
</dict></plist>
PLIST
launchctl bootout system /Library/LaunchDaemons/com.iflyelf.crossdesk.plist >/dev/null 2>&1 || true
launchctl bootstrap system /Library/LaunchDaemons/com.iflyelf.crossdesk.plist
echo "✅ CrossDesk 主机端已安装并启动"
`, psQuoteStr(arch), base, strings.Join(plist, "\n"))
	default:
		return fmt.Sprintf(`#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "请使用 root 或 sudo 运行"; exit 1; }
ARCH=%s
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|arm) ARCH=arm ;;
  *) echo "不支持的架构"; exit 1 ;;
esac
TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "%s/api/client/download?os=linux&arch=$ARCH&format=raw" -o "$TMP"
else
  wget -qO "$TMP" "%s/api/client/download?os=linux&arch=$ARCH&format=raw"
fi
install -m 0755 "$TMP" /usr/local/bin/iflygo-client
mkdir -p /var/lib/iflygo-crossdesk
cat > /etc/systemd/system/iflygo-crossdesk.service <<'UNIT'
[Unit]
Description=iFlyGo CrossDesk Host Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now iflygo-crossdesk
echo "✅ CrossDesk 主机端已安装并启动"
`, psQuoteStr(arch), base, base, execLine)
	}
}

func systemdQuote(v string) string {
	if strings.ContainsAny(v, " \t\"'\\") {
		return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
	}
	return v
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
