//go:build (!linux && !windows && !darwin) || android

package agent

import "fmt"

func newScreen() (Screen, error) {
	return nil, fmt.Errorf("当前平台暂不支持原生屏幕采集")
}

func newInput() (Input, error) {
	return nil, fmt.Errorf("当前平台暂不支持原生输入注入")
}
