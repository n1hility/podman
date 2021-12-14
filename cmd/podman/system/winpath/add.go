//go:build windows
// +build windows

package winpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/common/pkg/completion"
	"github.com/containers/podman/v3/cmd/podman/registry"
	"github.com/containers/podman/v3/cmd/podman/system"
	"github.com/containers/podman/v3/cmd/podman/validate"

	"github.com/spf13/cobra"
	winreg "golang.org/x/sys/windows/registry"
)

var (
	addCmd = &cobra.Command{
		Use:               "add [options]",
		Args:              validate.NoArgs,
		Short:             "Add podman to the windows path",
		Long:              "Adds podman to the windows path",
		RunE:              add,
		ValidArgsFunction: completion.AutocompleteNone,
		Example:           "something example",
	}
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: addCmd,
		Parent:  system.WinPathCmd,
	})
}

func add(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	target := filepath.Dir(exe)

	fmt.Println("Adding target = " + target)

	return addPathToRegistry(target)
}

func addPathToRegistry(path string) error {
	k, _, err := winreg.CreateKey(winreg.CURRENT_USER, `Environment`, winreg.WRITE|winreg.READ)
	if err != nil {
		return err
	}

	defer k.Close()

	existing, typ, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}

	for _, element := range strings.Split(existing, ";") {
		if strings.EqualFold(element, path) {
			// Path already added
			return nil
		}
	}

	if len(existing) > 1 {
		existing += ";"
	}

	existing += path

	if typ == winreg.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", existing)
	} else {
		err = k.SetStringValue("Path", existing)
	}

	if err == nil {
		broadcastEnvironmentChange()
	}

	return err
}
