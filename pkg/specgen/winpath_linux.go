package specgen

import "os"

func shouldResolveWinPaths() bool {
	return containerConfig.Engine.MachineType == "wsl"
}

func shouldResolveUnixWinVariant(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}

func resolveRelativeOnWindows(path string) string {
	return path
}

func winPathExists(path string) bool {
	return false
}