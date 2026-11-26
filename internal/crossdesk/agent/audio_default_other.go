//go:build !linux || android

package agent

// defaultAudioCommand 其他平台暂未内置系统音频采集，需要 --audio-cmd 指定命令。
func defaultAudioCommand() string { return "" }
