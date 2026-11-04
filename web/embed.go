// Package web 内嵌前端构建产物。
package web

import "embed"

// DistFS 内嵌前端产物（dist 目录）。
//
//go:embed all:dist
var DistFS embed.FS
