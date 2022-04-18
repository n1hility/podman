package specgen

import (
	"os"
)

func ShouldResolveWinPaths() bool {
	return containerConfig.Engine.MachineEnabled && containerConfig.Engine.MachineType == "wsl"
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
