//go:build !windows
// +build !windows

package wsl

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
