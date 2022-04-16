// +build !windows

package specgenutil

func resolveRelativeOnWindows(path string) (string, error) {
	return path, nil
}

func shouldResolveUnixWinVariant(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}