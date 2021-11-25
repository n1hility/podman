//go:build amd64 || arm64
// +build amd64 arm64

package machine

import (
	"fmt"
	"runtime"

	"github.com/containers/common/pkg/completion"
	"github.com/containers/podman/v3/cmd/podman/registry"
	"github.com/containers/podman/v3/pkg/machine"
	"github.com/containers/podman/v3/pkg/machine/qemu"
	"github.com/containers/podman/v3/pkg/machine/wsl"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	initCmd = &cobra.Command{
		Use:               "init [options] [NAME]",
		Short:             "Initialize a virtual machine",
		Long:              "initialize a virtual machine ",
		RunE:              initMachine,
		Args:              cobra.MaximumNArgs(1),
		Example:           `podman machine init myvm`,
		ValidArgsFunction: completion.AutocompleteNone,
	}
)

var (
	initOpts           = machine.InitOptions{}
	defaultMachineName = "podman-machine-default"
	now                bool
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: initCmd,
		Parent:  machineCmd,
	})
	flags := initCmd.Flags()
	cfg := registry.PodmanConfig()

	cpusFlagName := "cpus"
	flags.Uint64Var(
		&initOpts.CPUS,
		cpusFlagName, cfg.Machine.CPUs,
		"Number of CPUs",
	)
	_ = initCmd.RegisterFlagCompletionFunc(cpusFlagName, completion.AutocompleteNone)

	diskSizeFlagName := "disk-size"
	flags.Uint64Var(
		&initOpts.DiskSize,
		diskSizeFlagName, cfg.Machine.DiskSize,
		"Disk size in GB",
	)

	_ = initCmd.RegisterFlagCompletionFunc(diskSizeFlagName, completion.AutocompleteNone)

	memoryFlagName := "memory"
	flags.Uint64VarP(
		&initOpts.Memory,
		memoryFlagName, "m", cfg.Machine.Memory,
		"Memory in MB",
	)
	_ = initCmd.RegisterFlagCompletionFunc(memoryFlagName, completion.AutocompleteNone)

	flags.BoolVar(
		&now,
		"now", false,
		"Start machine now",
	)

	flags.BoolVar(
		&initOpts.ReExec,
		"reexec", false,
		"process was rexeced",
	)
	flags.MarkHidden("reexec")

	ImagePathFlagName := "image-path"
	flags.StringVar(&initOpts.ImagePath, ImagePathFlagName, cfg.Machine.Image, "Path to qcow image")
	_ = initCmd.RegisterFlagCompletionFunc(ImagePathFlagName, completion.AutocompleteDefault)

	IgnitionPathFlagName := "ignition-path"
	flags.StringVar(&initOpts.IgnitionPath, IgnitionPathFlagName, "", "Path to ignition file")
	_ = initCmd.RegisterFlagCompletionFunc(IgnitionPathFlagName, completion.AutocompleteDefault)
}

func getSystemDefaultVmType() string {
	if runtime.GOOS == "windows" {
		return "wsl"
	}

	return "qemu"
}

// TODO should we allow for a users to append to the qemu cmdline?
func initMachine(cmd *cobra.Command, args []string) error {
	var (
		vm     machine.VM
		vmType string
		err    error
	)

	vmType = getSystemDefaultVmType()
	initOpts.Name = defaultMachineName
	if len(args) > 0 {
		initOpts.Name = args[0]
	}
	switch vmType {
	case "wsl":
		if _, err := wsl.LoadVMByName(initOpts.Name); err == nil {
			return errors.Wrap(machine.ErrVMAlreadyExists, initOpts.Name)
		}
		vm, err = wsl.NewMachine(initOpts)
	default: // qemu is the default
		if _, err := qemu.LoadVMByName(initOpts.Name); err == nil {
			return errors.Wrap(machine.ErrVMAlreadyExists, initOpts.Name)
		}
		vm, err = qemu.NewMachine(initOpts)
	}
	if err != nil {
		return err
	}
	finished, err := vm.Init(initOpts)
	if initOpts.ReExec && err != nil {
		wsl.MessageBox(fmt.Sprintf("Error: %v", err), "WSL Operation Failed", true)
	}
	if err != nil || !finished {
		return err
	}
	fmt.Println("Machine init complete")
	if now {
		err = vm.Start(initOpts.Name, machine.StartOptions{})
		if err == nil {
			fmt.Printf("Machine %q started successfully\n", initOpts.Name)
		}
	} else {
		extra := ""
		if initOpts.Name != defaultMachineName {
			extra = " " + initOpts.Name
		}
		fmt.Printf("To start your machine run:\n\n\tpodman machine start%s\n\n", extra)
	}
	return err
}
