//go:build windows

package wsl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/pkg/machine/ocipull"
	"github.com/containers/podman/v5/pkg/machine/stdpull"

	gvproxy "github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/podman/v5/pkg/machine"
	"github.com/containers/podman/v5/pkg/machine/define"
	"github.com/containers/podman/v5/pkg/machine/ignition"
	"github.com/containers/podman/v5/pkg/machine/vmconfigs"
	"github.com/sirupsen/logrus"
)

type WSLStubber struct {
	vmconfigs.WSLConfig
}

func (w WSLStubber) CreateVM(opts define.CreateVMOpts, mc *vmconfigs.MachineConfig, _ *ignition.IgnitionBuilder) error {
	var (
		err error
	)
	// cleanup half-baked files if init fails at any point
	callbackFuncs := machine.InitCleanup()
	defer callbackFuncs.CleanIfErr(&err)
	go callbackFuncs.CleanOnSignal()
	mc.WSLHypervisor = new(vmconfigs.WSLConfig)
	// TODO
	// USB opts are unsupported in WSL.  Need to account for that here
	// or up the stack
	//	if len(opts.USBs) > 0 {
	//		return nil, fmt.Errorf("USB host passthrough is not supported for WSL machines")
	//	}

	if cont, err := checkAndInstallWSL(opts.ReExec); !cont {
		appendOutputIfError(opts.ReExec, err)
		return err
	}

	_ = setupWslProxyEnv()

	if opts.UserModeNetworking {
		if err = verifyWSLUserModeCompat(); err != nil {
			return err
		}
		mc.WSLHypervisor.UserModeNetworking = true
	}

	const prompt = "Importing operating system into WSL (this may take a few minutes on a new WSL install)..."
	dist, err := provisionWSLDist(mc.Name, mc.ImagePath.GetPath(), prompt)
	if err != nil {
		return err
	}

	unprovisionCallbackFunc := func() error {
		return unprovisionWSL(mc)
	}
	callbackFuncs.Add(unprovisionCallbackFunc)

	if mc.WSLHypervisor.UserModeNetworking {
		if err = installUserModeDist(dist, mc.ImagePath.GetPath()); err != nil {
			_ = unregisterDist(dist)
			return err
		}
	}

	fmt.Println("Configuring system...")
	if err = configureSystem(mc, dist); err != nil {
		return err
	}

	if err = installScripts(dist); err != nil {
		return err
	}

	if err = createKeys(mc, dist); err != nil {
		return err
	}

	// recycle vm
	return terminateDist(dist)
}

func (w WSLStubber) PrepareIgnition(_ *vmconfigs.MachineConfig, _ *ignition.IgnitionBuilder) (*ignition.ReadyUnitOpts, error) {
	return nil, nil
}

func (w WSLStubber) Exists(name string) (bool, error) {
	return isWSLExist(machine.ToDist(name))
}

func (w WSLStubber) MountType() vmconfigs.VolumeMountType {
	//TODO implement me
	panic("implement me")
}

func (w WSLStubber) MountVolumesToVM(mc *vmconfigs.MachineConfig, quiet bool) error {
	return nil
}

func (w WSLStubber) Remove(mc *vmconfigs.MachineConfig) ([]string, func() error, error) {
	// Note: we could consider swapping the two conditionals
	// below if we wanted to hard error on the wsl unregister
	// of the vm
	wslRemoveFunc := func() error {
		if err := runCmdPassThrough("wsl", "--unregister", machine.ToDist(mc.Name)); err != nil {
			logrus.Error(err)
		}
		return machine.ReleaseMachinePort(mc.SSH.Port)
	}

	return []string{}, wslRemoveFunc, nil
}

func (w WSLStubber) RemoveAndCleanMachines(_ *define.MachineDirs) error {
	return nil
}


func (w WSLStubber) SetProviderAttrs(mc *vmconfigs.MachineConfig, opts define.SetOptions) error {
	mc.Lock()
	defer mc.Unlock()

	// TODO the check for running when setting rootful is something I have not
	// seen in the other distributions.  I wonder if this is true everywhere or just
	// with WSL?
	// TODO maybe the "rule" for set is that it must be done when the machine is
	// stopped?
	// if opts.Rootful != nil && v.Rootful != *opts.Rootful {
	// 	err := v.setRootful(*opts.Rootful)
	// 	if err != nil {
	// 		setErrors = append(setErrors, fmt.Errorf("setting rootful option: %w", err))
	// 	} else {
	// 		if v.isRunning() {
	// 			logrus.Warn("restart is necessary for rootful change to go into effect")
	// 		}
	// 		v.Rootful = *opts.Rootful
	// 	}
	// }

	if opts.Rootful != nil && mc.HostUser.Rootful != *opts.Rootful {
		if err := mc.SetRootful(*opts.Rootful); err != nil {
			return err
		}
	}

	if opts.CPUs != nil {
		return errors.New("changing CPUs not supported for WSL machines")
	}

	if opts.Memory != nil {
		return errors.New("changing memory not supported for WSL machines")
	}

	// TODO USB still needs to be plumbed for all providers
	// if USBs != nil {
	// 	setErrors = append(setErrors, errors.New("changing USBs not supported for WSL machines"))
	// }

	if opts.DiskSize != nil {
		return errors.New("changing disk size not supported for WSL machines")
	}

	if opts.UserModeNetworking != nil && mc.WSLHypervisor.UserModeNetworking != *opts.UserModeNetworking {
		if running, _ := isRunning(mc.Name); running {
			return errors.New("user-mode networking can only be changed when the machine is not running")
		}

		dist := machine.ToDist(mc.Name)
		if err := changeDistUserModeNetworking(dist, mc.SSH.RemoteUsername, mc.ImagePath.GetPath(), *opts.UserModeNetworking); err != nil {
			return fmt.Errorf("failure changing state of user-mode networking setting", err)
		}

		mc.WSLHypervisor.UserModeNetworking = *opts.UserModeNetworking
	}

	return nil
}

func (w WSLStubber) StartNetworking(mc *vmconfigs.MachineConfig, cmd *gvproxy.GvproxyCommand) error {
	// Startup user-mode networking if enabled
	if mc.WSLHypervisor.UserModeNetworking {
		return startUserModeNetworking(mc)
	}
	return nil
}

func (w WSLStubber) UserModeNetworkEnabled(mc *vmconfigs.MachineConfig) bool {
	return mc.WSLHypervisor.UserModeNetworking
}

func (w WSLStubber) UseProviderNetworkSetup() bool {
	return true
}

func (w WSLStubber) RequireExclusiveActive() bool {
	return false
}

func (w WSLStubber) PostStartNetworking(mc *vmconfigs.MachineConfig, noInfo bool) error {
	winProxyOpts := machine.WinProxyOpts{
		Name:           mc.Name,
		IdentityPath:   mc.SSH.IdentityPath,
		Port:           mc.SSH.Port,
		RemoteUsername: mc.SSH.RemoteUsername,
		Rootful:        mc.HostUser.Rootful,
		VMType:         w.VMType(),
	}
	machine.LaunchWinProxy(winProxyOpts, noInfo)

	return nil
}

func (w WSLStubber) StartVM(mc *vmconfigs.MachineConfig) (func() error, func() error, error) {
	useProxy := setupWslProxyEnv()
	dist := machine.ToDist(mc.Name)

	// TODO Quiet is hard set to false: follow up
	if err := configureProxy(dist, useProxy, false); err != nil {
		return nil, nil, err
	}

	// TODO The original code checked to see if the SSH port was actually open and re-assigned if it was
	// we could consider this but it should be higher up the stack
	// if !machine.IsLocalPortAvailable(v.Port) {
	// logrus.Warnf("SSH port conflict detected, reassigning a new port")
	//	if err := v.reassignSshPort(); err != nil {
	//		return err
	//	}
	// }

	err := wslInvoke(dist, "/root/bootstrap")
	if err != nil {
		err = fmt.Errorf("the WSL bootstrap script failed: %w", err)
	}

	readyFunc := func() error {
		return nil
	}

	return nil, readyFunc, err
}

func (w WSLStubber) State(mc *vmconfigs.MachineConfig, bypass bool) (define.Status, error) {
	running, err := isRunning(mc.Name)
	if err != nil {
		return "", err
	}
	if running {
		return define.Running, nil
	}
	return define.Stopped, nil
}

func (w WSLStubber) StopVM(mc *vmconfigs.MachineConfig, hardStop bool) error {
	var (
		err error
	)
	mc.Lock()
	defer mc.Unlock()

	// recheck after lock
	if running, err := isRunning(mc.Name); !running {
		return err
	}

	dist := machine.ToDist(mc.Name)

	// Stop user-mode networking if enabled
	if err := stopUserModeNetworking(mc); err != nil {
		fmt.Fprintf(os.Stderr, "Could not cleanly stop user-mode networking: %s\n", err.Error())
	}

	if err := machine.StopWinProxy(mc.Name, vmtype); err != nil {
		fmt.Fprintf(os.Stderr, "Could not stop API forwarding service (win-sshproxy.exe): %s\n", err.Error())
	}

	cmd := exec.Command("wsl", "-u", "root", "-d", dist, "sh")
	cmd.Stdin = strings.NewReader(waitTerm)
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("executing wait command: %w", err)
	}

	exitCmd := exec.Command("wsl", "-u", "root", "-d", dist, "/usr/local/bin/enterns", "systemctl", "exit", "0")
	if err = exitCmd.Run(); err != nil {
		return fmt.Errorf("stopping sysd: %w", err)
	}

	if err = cmd.Wait(); err != nil {
		return err
	}

	return terminateDist(dist)
}

func (w WSLStubber) StopHostNetworking(mc *vmconfigs.MachineConfig, vmType define.VMType) error {
	return stopUserModeNetworking(mc)
}

func (w WSLStubber) VMType() define.VMType {
	return define.WSLVirt
}

func (w WSLStubber) GetDisk(_ string, dirs *define.MachineDirs, mc *vmconfigs.MachineConfig) error {
	var (
		myDisk ocipull.Disker
	)

	// check github for the latest version of the WSL dist
	downloadURL, downloadVersion, _, _, err := GetFedoraDownloadForWSL()
	if err != nil {
		return err
	}

	// we now save the "cached" rootfs in the form of "v<version-number>-rootfs.tar.xz"
	// i.e.v39.0.31-rootfs.tar.xz
	versionedBase := fmt.Sprintf("%s-%s", downloadVersion, filepath.Base(downloadURL.Path))

	// TODO we need a mechanism for "flushing" old cache files
	cachedFile, err := dirs.DataDir.AppendToNewVMFile(versionedBase, nil)
	if err != nil {
		return err
	}

	// if we find the same file cached (determined by filename only), then dont pull
	if _, err = os.Stat(cachedFile.GetPath()); err == nil {
		logrus.Debugf("%q already exists locally", cachedFile.GetPath())
		myDisk, err = stdpull.NewStdDiskPull(cachedFile.GetPath(), mc.ImagePath)
	} else {
		// no cached file
		myDisk, err = stdpull.NewDiskFromURL(downloadURL.String(), mc.ImagePath, dirs.DataDir, &versionedBase)
	}
	if err != nil {
		return err
	}
	// up until now, nothing has really happened
	// pull if needed and decompress to image location
	return myDisk.Get()
}
