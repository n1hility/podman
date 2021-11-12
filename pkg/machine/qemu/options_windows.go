package qemu

import (
	"os"
)

var (
	QemuCommand = "unused"
)

func getRuntimeDir() (string, error) {
	return os.TempDir(), nil
}

func (v *MachineVM) addArchOptions() []string {
	return []string{}
}

func (v *MachineVM) prepare() error {
	return nil
}

func (v *MachineVM) archRemovalFiles() []string {
	return []string{}
}
