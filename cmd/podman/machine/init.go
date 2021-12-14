// +build amd64 arm64

package machine

import (
	"fmt"

	"github.com/containers/common/pkg/completion"
	"github.com/containers/podman/v3/cmd/podman/registry"
	"github.com/containers/podman/v3/pkg/machine"
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
	initOpts.Username = cfg.Config.Machine.User

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

// TODO should we allow for a users to append to the qemu cmdline?
func initMachine(cmd *cobra.Command, args []string) error {
	var (
		vm  machine.VM
		err error
	)

	provider := getSystemDefaultProvider()
	initOpts.Name = defaultMachineName
	if len(args) > 0 {
		initOpts.Name = args[0]
	}
	if _, err := provider.LoadVMByName(initOpts.Name); err == nil {
		return errors.Wrap(machine.ErrVMAlreadyExists, initOpts.Name)
	}

	vm, err = provider.NewMachine(initOpts)
	if err != nil {
		return err
	}

	if finished, err := vm.Init(initOpts); err != nil || !finished {
		// Finished = true,  err  = nil  -  Success! Log a message with further instructions
		// Finished = false, err  = nil  -  The installation is partially complete and podman should
		//                                  exit gracefully with no error and no success message.
		//                                  Examples:
		//                                  - a user has chosen to perform their own reboot
		//                                  - reexec for limited admin operations, returning to parent
		// Finished = *,     err != nil  -  Exit with an error message

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
