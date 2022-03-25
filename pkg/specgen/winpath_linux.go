package specgen

import (
	"os"

	"github.com/containers/podman/v4/pkg/util"
)

func shouldResolveWinPaths() bool {
	return util.MachineHostType() == "wsl"
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
