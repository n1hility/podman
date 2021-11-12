package qemu

import (
	"os"
)

func getRuntimeDir() (string, error) {
	tmpDir, ok := os.LookupEnv("TMPDIR")
	if !ok {
		tmpDir = "/tmp"
	} 
	os.TempDir()
	return tmpDir, nil
}
