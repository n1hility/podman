// +build !windows

package specgen

func winPathExists(path string) bool {
	return false
}
