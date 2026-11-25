//go:build windows

package agent

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	procOpenClipboard              = modUser32.NewProc("OpenClipboard")
	procCloseClipboard             = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard             = modUser32.NewProc("EmptyClipboard")
	procGetClipboardData           = modUser32.NewProc("GetClipboardData")
	procSetClipboardData           = modUser32.NewProc("SetClipboardData")
	procGlobalSize                 = modKernel32.NewProc("GlobalSize")
	procGlobalAlloc                = modKernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock               = modKernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = modKernel32.NewProc("GlobalFree")
	procIsClipboardFormatAvailable = modUser32.NewProc("IsClipboardFormatAvailable")
)

type windowsClipboard struct{}

func newClipboard() (Clipboard, error) { return &windowsClipboard{}, nil }

func (c *windowsClipboard) ReadText() (string, error) {
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfUnicodeText); r == 0 {
		return "", fmt.Errorf("剪贴板中没有文本")
	}
	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return "", fmt.Errorf("打开剪贴板失败: %v", err)
	}
	defer procCloseClipboard.Call()
	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", fmt.Errorf("读取剪贴板文本失败")
	}
	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return "", fmt.Errorf("剪贴板文本为空")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		return "", fmt.Errorf("锁定剪贴板内存失败")
	}
	defer procGlobalUnlock.Call(h)
	raw := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), size/2)
	for i, v := range raw {
		if v == 0 {
			return syscall.UTF16ToString(raw[:i]), nil
		}
	}
	return syscall.UTF16ToString(raw), nil
}

func (c *windowsClipboard) WriteText(text string) error {
	if len(text) > clipTextLimit {
		return fmt.Errorf("剪贴板文本超过 %d 字节", clipTextLimit)
	}
	utf16 := syscall.StringToUTF16(text)
	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return fmt.Errorf("打开剪贴板失败: %v", err)
	}
	defer procCloseClipboard.Call()
	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return fmt.Errorf("清空剪贴板失败")
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(utf16)*2))
	if h == 0 {
		return fmt.Errorf("分配剪贴板内存失败: %v", err)
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("锁定剪贴板内存失败")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16)), utf16)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("写入剪贴板失败: %v", err)
	}
	return nil
}

func (c *windowsClipboard) Close() error { return nil }
