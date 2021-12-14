//go:build windows
// +build windows

package winpath

import (
	"errors"
	"io/fs"
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
	removeCmd = &cobra.Command{
		Use:               "remove [options]",
		Args:              validate.NoArgs,
		Short:             "Remove podman from the windows path",
		Long:              "Removes podman from the windows path",
		RunE:              remove,
		ValidArgsFunction: completion.AutocompleteNone,
		Example:           "something example",
	}
)

func init() {
	registry.Commands = append(registry.Commands, registry.CliCommand{
		Command: removeCmd,
		Parent:  system.WinPathCmd,
	})
}

func remove(cmd *cobra.Command, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	target := filepath.Dir(exe)

	return removePathFromRegistry(target)
}

func removePathFromRegistry(path string) error {
    k, err := winreg.OpenKey(winreg.CURRENT_USER, `Environment`, winreg.READ | winreg.WRITE)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Nothing to do
			return nil
		}
		return err
	}

	defer k.Close()

	existing, typ, err := k.GetStringValue("Path")
	if err != nil {
		return err
	}

	var elements []string
	for _, element := range strings.Split(existing, ";") {
		if strings.EqualFold(element, path) {
			continue
		}
		elements = append(elements, element)
	}

	newPath := strings.Join(elements, ";")
	if typ == winreg.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", newPath)
	} else {
		err = k.SetStringValue("Path", newPath)
	}

	if (err == nil) {
		broadcastEnvironmentChange()
	}

	return err
}
