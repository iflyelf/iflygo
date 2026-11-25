//go:build (!linux && !windows && !darwin) || android

package agent

import "fmt"

func newClipboard() (Clipboard, error) {
	return nil, fmt.Errorf("当前平台暂不支持剪贴板")
}
