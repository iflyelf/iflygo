// Package web 内嵌前端构建产物。
package web

import "embed"

// DistFS 内嵌前端产物（dist 目录）。
//
//go:embed all:dist
var DistFS embed.FS

// QuickstartJSON 快速上手结构化文案（由 upgrade_mgr 的 quickstart.yaml 生成）。
//
//go:embed quickstart.json
var QuickstartJSON []byte
