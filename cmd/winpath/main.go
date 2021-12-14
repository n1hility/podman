// +build windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

type operation int

const (
	HWND_BROADCAST             = 0xFFFF
	WM_SETTINGCHANGE           = 0x001A
	SMTO_ABORTIFHUNG           = 0x0002
	ERR_BAD_ARGS               = 0x000A
	OPERATION_FAILED           = 0x06AC
	Environment                = "Environment"
	Add              operation = iota
	Remove
	NotSpecified
)

func main() {
	op := NotSpecified
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "add":
			op = Add
		case "remove":
			op = Remove
		}
	}

	// Stay silent since ran from an installer
	if op == NotSpecified {
		os.Exit(ERR_BAD_ARGS)
	}

	if err := modify(op); err != nil {
		os.Exit(OPERATION_FAILED)
	}
}

func modify(op operation) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	target := filepath.Dir(exe)

	if op == Remove {
		return removePathFromRegistry(target)
	}

	return addPathToRegistry(target)
}

func addPathToRegistry(path string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, Environment, registry.WRITE|registry.READ)
	if err != nil {
		return err
	}

	defer k.Close()

	existing, typ, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}

	for _, element := range strings.Split(existing, ";") {
		if strings.EqualFold(element, path) {
			// Path already added
			return nil
		}
	}

	if len(existing) > 1 {
		existing += ";"
	}

	existing += path

	if typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", existing)
	} else {
		err = k.SetStringValue("Path", existing)
	}

	if err == nil {
		broadcastEnvironmentChange()
	}

	return err
}

func removePathFromRegistry(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, Environment, registry.READ|registry.WRITE)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing to do
			return nil
		}
		return err
	}

	defer k.Close()

	existing, typ, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}

	var elements []string
	for _, element := range strings.Split(existing, ";") {
		if strings.EqualFold(element, path) {
			continue
		}
		elements = append(elements, element)
	}

	newPath := strings.Join(elements, ";")
	if typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", newPath)
	} else {
		err = k.SetStringValue("Path", newPath)
	}

	if err == nil {
		broadcastEnvironmentChange()
	}

	return err
}

func broadcastEnvironmentChange() {
	env, _ := syscall.UTF16PtrFromString(Environment)
	user32 := syscall.NewLazyDLL("user32")
	proc := user32.NewProc("SendMessageTimeoutW")
	_, _, _ = proc.Call(HWND_BROADCAST, WM_SETTINGCHANGE, 0, 
		uintptr(unsafe.Pointer(env)), SMTO_ABORTIFHUNG, 3000, 0)
}
