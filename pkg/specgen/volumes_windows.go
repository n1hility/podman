package specgen

import (
	"os"
)

func winPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
