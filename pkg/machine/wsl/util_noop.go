//go:build !windows
// +build !windows

package wsl

import (
	"reflect"
	"unicode/utf16"
	"unsafe"
)

func winVersionAtLeast(major, minor, build uint32) bool {
	return false
}

func reboot() error {
	return nil
}

func relaunchElevated() error {
	return nil
}

func relaunchElevatedWait() error {
	return nil
}

func hasAdminRights() bool {
	return false
}

func MessageBox(caption, title string, fail bool) int {
	return 0
}


func getCommandLine() string {
	var blah *uint16
	cmd := uintptr(unsafe.Pointer(blah))
	size := unsafe.Sizeof(uint16(0))
	
	len := 0
	for p := cmd; u16Deref(p) != 0; p += size {
		len++	
	}


	var runes []uint16
	assignSlice(unsafe.Pointer(&runes), cmd, len)
	return string(utf16.Decode(runes))
}

func u16Deref(p uintptr) uint16 {
	//machine.GetDataDir()
	return *(*uint16)(unsafe.Pointer(p))
}

func assignSlice(slice unsafe.Pointer, data uintptr, len int) {
	header := (*reflect.SliceHeader)(slice)
	header.Data = data
	header.Cap = len
	header.Len = len
}