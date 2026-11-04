// Package version 保存构建版本信息，通过 -ldflags 注入。
package version

// 这些变量由 -ldflags "-X github.com/iflyelf/iflygo/internal/version.Version=..." 注入。
var (
	Version = "0.1.0" // 语义化版本号
	Build   = "dev"   // 构建标识（git commit / 构建编号）
	Commit  = "unknown"
)
