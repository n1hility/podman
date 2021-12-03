//go:build amd64 || arm64
// +build amd64 arm64

package machine

import (
	"fmt"

	"github.com/containers/podman/v3/cmd/podman/registry"
	"github.com/containers/podman/v3/pkg/machine"
	"github.com/containers/podman/v3/pkg/machine/qemu"
	"github.com/containers/podman/v3/pkg/machine/wsl"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	startCmd = &cobra.Command{
		Use:               "start [MACHINE]",
		Short:             "Start an existing machine",
		Long:              "Start a managed virtual machine ",
		RunE:              start,
		Args:              cobra.MaximumNArgs(1),
		Example:           `podman machine start myvm`,
		ValidArgsFunction: autocompleteMachine,
	}
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: startCmd,
		Parent:  machineCmd,
	})
}

func start(cmd *cobra.Command, args []string) error {
	var (
		err    error
		vm     machine.VM
		vmType string
	)
	vmName := defaultMachineName
	if len(args) > 0 && len(args[0]) > 0 {
		vmName = args[0]
	}

	vmType = getSystemDefaultVMType()

	switch vmType {
	case "wsl":
		vm, err = wsl.LoadVMByName(vmName)
	default:
		active, activeName, cerr := qemu.CheckActiveVM()
		if cerr != nil {
			return cerr
		}
		if active {
			if vmName == activeName {
				return errors.Wrapf(machine.ErrVMAlreadyRunning, "cannot start VM %s", vmName)
			}
			return errors.Wrapf(machine.ErrMultipleActiveVM, "cannot start VM %s. VM %s is currently running", vmName, activeName)
		}
		vm, err = qemu.LoadVMByName(vmName)
	}
	if err != nil {
		return err
	}
	if err := vm.Start(vmName, machine.StartOptions{}); err != nil {
		return err
	}
	fmt.Printf("Machine %q started successfully\n", vmName)
	return nil
}
