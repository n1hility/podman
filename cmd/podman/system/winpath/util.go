//go:build windows
// +build windows

package winpath

import (
	"syscall"
	"unsafe"
)

const (
	HWND_BROADCAST   = 0xFFFF
	WM_SETTINGCHANGE = 0x001A
	SMTO_ABORTIFHUNG = 0x0002
)

func broadcastEnvironmentChange() {
	user32 := syscall.NewLazyDLL("user32")
	proc := user32.NewProc("SendMessageTimeoutW")
	_, _, _ = proc.Call(HWND_BROADCAST, WM_SETTINGCHANGE, 0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("Environment"))), SMTO_ABORTIFHUNG, 3000, 0)
}
