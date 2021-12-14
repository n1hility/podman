// +build windows
package system

import (
	"github.com/containers/podman/v3/cmd/podman/registry"
	"github.com/containers/podman/v3/cmd/podman/validate"
	"github.com/spf13/cobra"
)

var (
	// Skip creating engines
	winNoOp = func(cmd *cobra.Command, args []string) error {
		return nil
	}

	WinPathCmd = &cobra.Command{
		Use:                "win-path",
		Short:              "Manage podman in windows path",
		Long:               `Add or remove podman in the windows path`,
		PersistentPreRunE:  winNoOp,
		RunE:               validate.SubCommandExists,
		PersistentPostRunE: winNoOp,
		TraverseChildren:   false,
	}
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: WinPathCmd,
		Parent:  systemCmd,
	})
}
