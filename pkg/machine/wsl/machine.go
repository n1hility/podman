//go:build amd64 || arm64
// +build amd64 arm64

package wsl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containers/podman/v3/pkg/machine"
	"github.com/containers/podman/v3/utils"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

var (
	// vmtype refers to qemu (vs libvirt, krun, etc)
	vmtype               = "wsl"
	defaultRemoteUser    = "user"
	defaultFedoraRelease = "34"
)

const ERROR_SUCCESS_REBOOT_INITIATED = 1641
const ERROR_SUCCESS_REBOOT_REQUIRED = 3010

const containersConf = `[containers]
netns="slirp4netns"

[engine]
cgroup_manager = "cgroupfs"
events_logger = "file"
`

const appendPort = `grep -q Port\ %d /etc/ssh/sshd_config || echo Port %d >> /etc/ssh/sshd_config`

const configServices = `ln -fs /usr/lib/systemd/system/sshd.service /etc/systemd/system/multi-user.target.wants/sshd.service
ln -fs /usr/lib/systemd/system/podman.socket /etc/systemd/system/sockets.target.wants/podman.socket
rm -f /etc/systemd/system/getty.target.wants/console-getty.service
rm -f /etc/systemd/system/getty.target.wants/getty@tty1.service
rm -f /etc/systemd/system/multi-user.target.wants/systemd-resolved.service
rm -f /etc/systemd/system/dbus-org.freedesktop.resolve1.service
ln -fs /dev/null /etc/systemd/system/console-getty.service
mkdir -p /etc/systemd/system/systemd-sysusers.service.d/
adduser -m user -G wheel
mkdir -p /home/user/.config/systemd/user/
chown user:user /home/user/.config
`

const sudoers = `%wheel        ALL=(ALL)       NOPASSWD: ALL
`

const bootstrap = `#!/bin/bash
ps -ef | grep -v grep | grep -q systemd && exit 0
nohup unshare --kill-child --fork --pid --mount --mount-proc --propagation shared /lib/systemd/systemd >/dev/null 2>&1 &
sleep 0.1
`

const wslmotd = `
You will be automatically entered into a nested process namespace where 
systemd is running. If you need to access the root namespace, hit ctrl-d 
or type exit. This also means to log out you need to exit twice.

`

const sysdpid = "SYSDPID=`ps -eo cmd,pid | grep -m 1 ^/lib/systemd/systemd | awk '{print $2}'`"

const profile = sysdpid + `
if [ ! -z "$SYSDPID" ] && [ "$SYSDPID" != "1" ]; then
    cat /etc/wslmotd
	/usr/local/bin/podman-enterns
fi
`

const enterns = "#!/bin/bash\n" + sysdpid + `
if [ ! -z "$SYSDPID" ] && [ "$SYSDPID" != "1" ]; then
	nsenter -m -p -t $SYSDPID "$@"
fi
`

const waitTerm = sysdpid + `
if [ ! -z "$SYSDPID" ]; then
	timeout 60 tail -f /dev/null --pid $SYSDPID
fi
`

// WSL kernel does not have sg and crypto_user modules
const overrideSysusers = `[Service]
LoadCredential=
`

const lingerService = `[Unit]
Description=A systemd user unit demo
After=network-online.target
Wants=network-online.target podman.socket
[Service]
ExecStart=/usr/bin/sleep infinity
`

const lingerSetup = `mkdir -p /home/user/.config/systemd/user/default.target.wants
ln -fs /home/user/.config/systemd/user/linger-example.service \
       /home/user/.config/systemd/user/default.target.wants/linger-example.service
`

const wslInstallError = `Could not %s. See previous output for any potential failure details. 
If you can not resolve the issue, and rerunning fails, try the "wsl --install" process
outlined in the following article:

http://docs.microsoft.com/en-us/windows/wsl/install

`

const wslInstallKernel = "install the WSL Kernel"

type MachineVM struct {
	// IdentityPath is the fq path to the ssh priv key
	IdentityPath string
	// IgnitionFilePath is the fq path to the .ign file
	ImageStream string
	// ImagePath is the fq path to
	ImagePath string
	// Name of the vm
	Name string
	// SSH port for user networking
	Port int
	// RemoteUsername of the vm user
	RemoteUsername string
}

type ExitCodeError struct {
	code uint
}

func (e *ExitCodeError) Error() string {
	return fmt.Sprintf("Process failed with exit code: %d", e.code)
}

// NewMachine initializes an instance of a virtual machine based on the qemu
// virtualization.
func NewMachine(opts machine.InitOptions) (machine.VM, error) {
	vm := new(MachineVM)
	if len(opts.Name) > 0 {
		vm.Name = opts.Name
	}

	// An image was specified
	if len(opts.ImagePath) > 0 {
		vm.ImagePath = opts.ImagePath
	}

	// Assign remote user name. if not provided, use default
	vm.RemoteUsername = opts.Username
	if len(vm.RemoteUsername) < 1 {
		vm.RemoteUsername = defaultRemoteUser
	}

	// Add a random port for ssh
	port, err := utils.GetRandomPort()
	if err != nil {
		return nil, err
	}
	vm.Port = port

	return vm, nil
}

// LoadByName reads a json file that describes a known qemu vm
// and returns a vm instance
func LoadVMByName(name string) (machine.VM, error) {
	vm := new(MachineVM)
	vmConfigDir, err := machine.GetConfDir(vmtype)
	if err != nil {
		return nil, err
	}
	b, err := ioutil.ReadFile(filepath.Join(vmConfigDir, name+".json"))
	if os.IsNotExist(err) {
		return nil, errors.Wrap(machine.ErrNoSuchVM, name)
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(b, vm)
	return vm, err
}

// Init writes the json configuration file to the filesystem for
// other verbs (start, stop)
func (v *MachineVM) Init(opts machine.InitOptions) (bool, error) {
	var (
		key string
	)

	cont, err := checkAndInstallWSL(opts)
	if !cont {
		appendOutputIfError(opts.ReExec, err)
		return cont, err
	}

	homeDir, err := machine.GetUserHome()
	if err != nil {
		return false, err
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	// GetConfDir creates the directory so no need to check for
	// its existence
	vmConfigDir, err := machine.GetConfDir(vmtype)
	if err != nil {
		return false, err
	}
	vmDataDir, err := machine.GetDataDir(vmtype)
	if err != nil {
		return false, err
	}
	jsonFile := filepath.Join(vmConfigDir, v.Name) + ".json"
	v.IdentityPath = filepath.Join(sshDir, v.Name)

	var dd machine.DistributionDownload
	switch opts.ImagePath {
	// TODO remove testing from default common config
	case "testing", "":
		// Get image as usual
		v.ImageStream = defaultFedoraRelease
		dd, err = machine.NewFedoraDownloader(vmtype, v.Name, v.ImageStream)
		if err != nil {
			return false, err
		}
	default:
		if _, e := os.Stat(opts.ImagePath); e == nil {
			v.ImageStream = "custom"
			dd, err = machine.NewGenericDownloader(vmtype, v.Name, opts.ImagePath)
		} else if _, e := strconv.Atoi(opts.ImagePath); e == nil {
			v.ImageStream = opts.ImagePath
			dd, err = machine.NewFedoraDownloader(vmtype, v.Name, v.ImageStream)
		} else {
			return false, errors.Errorf("Image not found: %s", opts.ImagePath)
		}
		if err != nil {
			return false, err
		}
	}

	v.ImagePath = dd.Get().LocalUncompressedFile
	if err := machine.DownloadImage(dd); err != nil {
		return false, err
	}

	uri := machine.SSHRemoteConnection.MakeSSHURL("localhost", "/run/user/1000/podman/podman.sock", strconv.Itoa(v.Port), v.RemoteUsername)
	if err := machine.AddConnection(&uri, v.Name, filepath.Join(sshDir, v.Name), opts.IsDefault); err != nil {
		return false, err
	}

	uriRoot := machine.SSHRemoteConnection.MakeSSHURL("localhost", "/run/podman/podman.sock", strconv.Itoa(v.Port), "root")
	if err := machine.AddConnection(&uriRoot, v.Name+"-root", filepath.Join(sshDir, v.Name), opts.IsDefault); err != nil {
		return false, err
	}

	// Write the JSON file
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return false, err
	}
	if err := ioutil.WriteFile(jsonFile, b, 0644); err != nil {
		return false, errors.Wrap(err, "Could not write machine json config")
	}

	distDir := filepath.Join(vmDataDir, "wsldist")
	distTarget := filepath.Join(distDir, v.Name)
	if err := os.MkdirAll(distDir, 0755); err != nil {
		return false, errors.Wrap(err, "Could not create wsldist directory")
	}

	dist := toDist(v.Name)
	fmt.Println("Importing operating system into WSL (this may take 5+ minutes on a new WSL install)...")
	err = runCmdPassThrough("wsl", "--import", dist, distTarget, v.ImagePath)
	if err != nil {
		return false, errors.Wrap(err, "WSL import of guest OS failed")
	}

	fmt.Println("Installing packages (this will take awhile)...")
	err = runCmdPassThrough("wsl", "-d", dist, "dnf", "upgrade", "-y")
	if err != nil {
		return false, errors.Wrap(err, "Package upgrade on guest OS failed")
	}

	err = runCmdPassThrough("wsl", "-d", dist, "dnf", "install", "podman", "podman-docker", "openssh-server", "procps-ng", "-y")
	if err != nil {
		return false, errors.Wrap(err, "Package installation on guest OS failed")
	}

	// Fixes newuidmap
	err = runCmdPassThrough("wsl", "-d", dist, "dnf", "reinstall", "shadow-utils", "-y")
	if err != nil {
		return false, errors.Wrap(err, "Package reinstallation of shadow-utils on guest OS failed")
	}

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return false, errors.Wrap(err, "Could not create ssh directory")
	}

	key, err = machine.CreateSSHKeysPrefix(sshDir, v.Name, true, true, "wsl", "-d", dist)
	if err != nil {
		return false, errors.Wrap(err, "Could not create ssh keys")
	}

	fmt.Println("Configuring system...")
	err = runCmdPassThrough("wsl", "-d", dist, "sh", "-c", fmt.Sprintf(appendPort, v.Port, v.Port))
	if err != nil {
		return false, errors.Wrap(err, "Could not configure SSH port for guest OS")
	}

	err = pipeCmdPassThrough("wsl", configServices, "-d", dist, "sh")
	if err != nil {
		return false, errors.Wrap(err, "Could not configure systemd settomgs for guest OS")
	}

	err = pipeCmdPassThrough("wsl", sudoers, "-d", dist, "sh", "-c", "cat >> /etc/sudoers")
	if err != nil {
		return false, errors.Wrap(err, "Could not add wheel to sudoers")
	}

	err = pipeCmdPassThrough("wsl", overrideSysusers, "-d", dist, "sh", "-c",
		"cat > /etc/systemd/system/systemd-sysusers.service.d/override.conf")
	if err != nil {
		return false, errors.Wrap(err, "Could not generate systemd-sysusers override for guest OS")
	}

	err = pipeCmdPassThrough("wsl", lingerService, "-d", dist, "sh", "-c",
		"cat > /home/user/.config/systemd/user/linger-example.service")
	if err != nil {
		return false, errors.Wrap(err, "Could not generate linger service for guest OS")
	}

	err = pipeCmdPassThrough("wsl", lingerSetup, "-d", dist, "sh")
	if err != nil {
		return false, errors.Wrap(err, "Could not configure systemd settomgs for guest OS")
	}

	err = pipeCmdPassThrough("wsl", containersConf, "-d", dist, "sh", "-c", "cat > /etc/containers.conf")
	if err != nil {
		return false, errors.Wrap(err, "Could not create containers.conf for guest OS")
	}

	err = pipeCmdPassThrough("wsl", enterns, "-d", dist, "sh", "-c", "cat > /usr/local/bin/podman-enterns; chmod 755 /usr/local/bin/podman-enterns")
	if err != nil {
		return false, errors.Wrap(err, "Could not create enterns script for guest OS")
	}

	err = pipeCmdPassThrough("wsl", profile, "-d", dist, "sh", "-c", "cat > /etc/profile.d/enterns.sh")
	if err != nil {
		return false, errors.Wrap(err, "Could not create motd profile script for guest OS")
	}

	err = pipeCmdPassThrough("wsl", wslmotd, "-d", dist, "sh", "-c", "cat > /etc/wslmotd")
	if err != nil {
		return false, errors.Wrap(err, "Could not create a WSL MOTD for guest OS")
	}

	err = pipeCmdPassThrough("wsl", bootstrap, "-d", dist, "sh", "-c", "cat > /root/bootstrap; chmod 755 /root/bootstrap")
	if err != nil {
		return false, errors.Wrap(err, "Could not create bootstrap script for guest OS")
	}

	err = pipeCmdPassThrough("wsl", key+"\n", "-d", dist, "sh", "-c",
		"mkdir -p /root/.ssh; cat >> /root/.ssh/authorized_keys; chmod 600 /root/.ssh/authorized_keys")
	if err != nil {
		return false, errors.Wrap(err, "Could not create root authorized keys on guest OS")
	}

	err = pipeCmdPassThrough("wsl", key+"\n", "-d", dist, "sh", "-c",
		"mkdir -p /home/user/.ssh; cat >> /home/user/.ssh/authorized_keys; chown -R user:user /home/user/.ssh; chmod 600 /home/user/.ssh/authorized_keys")
	if err != nil {
		return false, errors.Wrap(err, "Could not create 'user' authorized keys on guest OS")
	}

	return true, nil
}

func checkAndInstallWSL(opts machine.InitOptions) (bool, error) {
	if !isWSLInstalled() {
		admin := hasAdminRights()

		if !isWSLFeatureEnabled() {
			if !winVersionAtLeast(10, 0, 18362) {
				errors.Errorf("Your version of Windows does not support WSL. Update to Windows 10 Build 19041 or later")
			} else if !winVersionAtLeast(10, 0, 19041) {
				fmt.Fprintf(os.Stderr, "Automatic installation of WSL can not be performed on this version of Windows.\n")
				fmt.Fprintf(os.Stderr, "Either update to Build 19041 (or later), or perform the manual installation steps\n")
				fmt.Fprintf(os.Stderr, "outlined in the following article:\n\n")
				fmt.Fprintf(os.Stderr, "http://docs.microsoft.com/en-us/windows/wsl/install\n\n")
				return false, errors.Errorf("WSL can not be automatically installed")
			}

			message := "WSL is not installed on this system, installing it.\n\n"

			if !admin {
				message += "Since you are not running as admin, a new window will open and " +
					"require you to approve administrator privileges.\n\n"
			}

			message += "NOTE: A system reboot will be required as part of this process. " +
				"If you prefer, you may abort now, and perform a manual installation using the \"wsl --install\" command."

			if !opts.ReExec && MessageBox(message, "Podman Machine", false) != 1 {
				return false, errors.Errorf("WSL installation aborted")
			}

			if !opts.ReExec && !admin {
				err := launchElevate("install the Windows WSL Features")
				return false, err
			}

			return false, installWsl()
		}

		skip := false
		if !opts.ReExec && !admin {
			if err := launchElevate(wslInstallKernel); err != nil {
				return false, err
			}

			skip = true
		}

		if !skip {
			err := installWslKernel()
			if err != nil {
				fmt.Fprintf(os.Stderr, wslInstallError, wslInstallKernel)
				return false, err
			}

			if opts.ReExec {
				return false, nil
			}
		}
	}

	return true, nil
}

func launchElevate(operation string) error {
	truncateElevatedOutputFile()
	err := relaunchElevatedWait()
	if err != nil {
		if eerr, ok := err.(*ExitCodeError); ok {
			if eerr.code == ERROR_SUCCESS_REBOOT_REQUIRED {
				fmt.Println("Reboot is required to continue installation, please reboot at your convenience")
				return nil
			}
		}

		fmt.Fprintf(os.Stderr, "Elevated process failed with error: %v\n\n", err)
		dumpOutputFile()
		fmt.Fprintf(os.Stderr, wslInstallError, operation)
	}
	return err
}

func installWsl() error {
	log, err := getElevatedOutputFileWrite()
	if err != nil {
		return err
	}
	defer log.Close()
	err = runCmdPassThroughTee(log, "dism", "/online", "/enable-feature", "/featurename:Microsoft-Windows-Subsystem-Linux", "/all", "/norestart")
	if isMsiError(err) {
		return errors.Wrap(err, "Could not enable WSL Feature")
	}

	err = runCmdPassThroughTee(log, "dism", "/online", "/enable-feature", "/featurename:VirtualMachinePlatform", "/all", "/norestart")
	if isMsiError(err) {
		return errors.Wrap(err, "Could not enable Virtual Machine Feature")
	}
	log.Close()

	return reboot()
}

func installWslKernel() error {
	log, err := getElevatedOutputFileWrite()
	if err != nil {
		return err
	}
	defer log.Close()

	message := "Installing WSL Kernel Update"
	fmt.Println(message)
	fmt.Fprintln(log, message)

	err = runCmdPassThroughTee(log, "wsl", "--update")
	if err != nil {
		return errors.Wrap(err, "Could not install WSL Kernel")
	}

	return nil
}

func getElevatedOutputFileName() (string, error) {
	dir, err := machine.GetDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "podman-elevated-output.log"), nil
}

func dumpOutputFile() {
	file, err := getElevatedOutputFileRead()
	if err != nil {
		logrus.Debug("Could not find elevated child output file")
		return
	}
	defer file.Close()
	_, _ = io.Copy(os.Stdout, file)
}

func getElevatedOutputFileRead() (*os.File, error) {
	return getElevatedOutputFile(os.O_RDONLY)

}

func getElevatedOutputFileWrite() (*os.File, error) {
	return getElevatedOutputFile(os.O_WRONLY | os.O_CREATE | os.O_APPEND)
}

func appendOutputIfError(write bool, err error) {
	if write && err == nil {
		return
	}

	file, oerr := getElevatedOutputFileWrite()
	if oerr == nil {
		defer file.Close()
		fmt.Fprintf(file, "Error: %v\n", err)
	}
}

func truncateElevatedOutputFile() error {
	name, err := getElevatedOutputFileName()
	if err != nil {
		return err
	}

	return os.Truncate(name, 0)
}

func getElevatedOutputFile(mode int) (*os.File, error) {
	name, err := getElevatedOutputFileName()
	if err != nil {
		return nil, err
	}

	dir, err := machine.GetDataHome()
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	return os.OpenFile(name, mode, 0644)
}

func isMsiError(err error) bool {
	if err == nil {
		return false
	}

	if eerr, ok := err.(*exec.ExitError); ok {
		switch eerr.ExitCode() {
		case 0:
			fallthrough
		case ERROR_SUCCESS_REBOOT_INITIATED:
			fallthrough
		case ERROR_SUCCESS_REBOOT_REQUIRED:
			return false
		}
	}

	return true
}
func toDist(name string) string {
	if !strings.HasPrefix(name, "podman") {
		name = "podman-" + name
	}
	return name
}

func runCmdPassThrough(name string, arg ...string) error {
	logrus.Debugf("Running command: %s %v", name, arg)
	cmd := exec.Command(name, arg...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCmdPassThroughTee(out io.Writer, name string, arg ...string) error {
	logrus.Debugf("Running command: %s %v", name, arg)

	// TODO - Perhaps improve this with a conpty pseudo console so that
	//        dism installer text bars mirror console behavior (redraw)
	cmd := exec.Command(name, arg...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, out)
	cmd.Stderr = io.MultiWriter(os.Stderr, out)
	return cmd.Run()
}

func pipeCmdPassThrough(name string, input string, arg ...string) error {
	logrus.Debugf("Running command: %s %v", name, arg)
	cmd := exec.Command(name, arg...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (v *MachineVM) Start(name string, _ machine.StartOptions) error {
	if v.isRunning() {
		return errors.Errorf("%q is already running", name)
	}

	fmt.Println("Starting machine...")

	dist := toDist(name)

	err := runCmdPassThrough("wsl", "-d", dist, "/root/bootstrap")
	if err != nil {
		return errors.Wrap(err, "WSL bootstrap script failed")
	}

	return markStart(name)
}

func isWSLInstalled() bool {
	cmd := exec.Command("wsl", "--status")
	success := cmd.Run() == nil
	if success {
		fmt.Println("WSL Status SUCCESS")
	}
	return success
}

func isWSLFeatureEnabled() bool {
	cmd := exec.Command("wsl", "--set-default-version", "2")
	return cmd.Run() == nil
}

func isWSLRunning(dist string) (bool, error) {
	cmd := exec.Command("wsl", "-l", "--running")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err = cmd.Start(); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(transform.NewReader(out, unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()))
	result := false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && dist == fields[0] {
			result = true
			break
		}
	}

	_ = cmd.Wait()

	return result, nil
}

func isSystemdRunning(dist string) (bool, error) {
	cmd := exec.Command("wsl", "-d", dist, "sh")
	cmd.Stdin = strings.NewReader(sysdpid + "\necho $SYSDPID\n")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err = cmd.Start(); err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(out)
	result := false
	if scanner.Scan() {
		text := scanner.Text()
		i, err := strconv.Atoi(text)
		if err == nil && i > 0 {
			result = true
		}
	}

	_ = cmd.Wait()

	return result, nil
}

// Stop uses the qmp monitor to call a system_powerdown
func (v *MachineVM) Stop(name string, _ machine.StopOptions) error {
	dist := toDist(v.Name)

	wsl, err := isWSLRunning(dist)
	if err != nil {
		return err
	}

	sysd := false
	if wsl {
		sysd, err = isSystemdRunning(dist)
		if err != nil {
			return err
		}
	}

	if !wsl || !sysd {
		return errors.Errorf("%q is not running", v.Name)
	}

	cmd := exec.Command("wsl", "-d", dist, "sh")
	cmd.Stdin = strings.NewReader(waitTerm)
	if err = cmd.Start(); err != nil {
		return errors.Wrap(err, "Error executing wait command")
	}

	exitCmd := exec.Command("wsl", "-d", dist, "/usr/local/bin/podman-enterns", "systemctl", "exit", "0")
	if err = exitCmd.Run(); err != nil {
		return errors.Wrap(err, "Error stopping sysd")
	}

	if err = cmd.Wait(); err != nil {
		return err
	}

	cmd = exec.Command("wsl", "--terminate", dist)
	if err = cmd.Run(); err != nil {
		return err
	}

	fmt.Printf("%q stopped successfully\n", v.Name)
	return nil
}

func (v *MachineVM) Remove(name string, opts machine.RemoveOptions) (string, func() error, error) {
	var files []string

	if v.isRunning() {
		return "", nil, errors.Errorf("running vm %q cannot be destroyed", v.Name)
	}

	// Collect all the files that need to be destroyed
	if !opts.SaveKeys {
		files = append(files, v.IdentityPath, v.IdentityPath+".pub")
	}
	if !opts.SaveImage {
		files = append(files, v.ImagePath)
	}

	vmConfigDir, err := machine.GetConfDir(vmtype)
	if err != nil {
		return "", nil, err
	}
	files = append(files, filepath.Join(vmConfigDir, v.Name+".json"))

	vmDataDir, err := machine.GetDataDir(vmtype)
	if err != nil {
		return "", nil, err
	}
	files = append(files, filepath.Join(vmDataDir, "wsldist", v.Name))

	confirmationMessage := "\nThe following files will be deleted:\n\n"
	for _, msg := range files {
		confirmationMessage += msg + "\n"
	}

	confirmationMessage += "\n"
	return confirmationMessage, func() error {
		if err := machine.RemoveConnection(v.Name); err != nil {
			logrus.Error(err)
		}
		if err := machine.RemoveConnection(v.Name + "-root"); err != nil {
			logrus.Error(err)
		}
		if err := runCmdPassThrough("wsl", "--unregister", toDist(v.Name)); err != nil {
			logrus.Error(err)
		}
		for _, f := range files {
			if err := os.RemoveAll(f); err != nil {
				logrus.Error(err)
			}
		}
		return nil
	}, nil
}

func (v *MachineVM) isRunning() bool {
	dist := toDist(v.Name)

	wsl, err := isWSLRunning(dist)
	if err != nil {
		return false
	}

	sysd := false
	if wsl {
		sysd, err = isSystemdRunning(dist)

		if err != nil {
			return false
		}
	}

	return sysd
}

// SSH opens an interactive SSH session to the vm specified.
// Added ssh function to VM interface: pkg/machine/config/go : line 58
func (v *MachineVM) SSH(name string, opts machine.SSHOptions) error {
	if !v.isRunning() {
		return errors.Errorf("vm %q is not running.", v.Name)
	}

	username := opts.Username
	if username == "" {
		username = v.RemoteUsername
	}

	sshDestination := username + "@localhost"
	port := strconv.Itoa(v.Port)

	args := []string{"-i", v.IdentityPath, "-p", port, sshDestination, "-o", "UserKnownHostsFile /dev/null", "-o", "StrictHostKeyChecking no"}
	if len(opts.Args) > 0 {
		args = append(args, opts.Args...)
	} else {
		fmt.Printf("Connecting to vm %s. To close connection, use `~.` or `exit`\n", v.Name)
	}

	cmd := exec.Command("ssh", args...)
	logrus.Debugf("Executing: ssh %v\n", args)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// List lists all vm's that use qemu virtualization
func List(_ machine.ListOptions) ([]*machine.ListResponse, error) {
	return GetVMInfos()
}

func GetVMInfos() ([]*machine.ListResponse, error) {
	vmConfigDir, err := machine.GetConfDir(vmtype)
	if err != nil {
		return nil, err
	}

	var listed []*machine.ListResponse

	if err = filepath.Walk(vmConfigDir, func(path string, info os.FileInfo, err error) error {
		vm := new(MachineVM)
		if strings.HasSuffix(info.Name(), ".json") {
			fullPath := filepath.Join(vmConfigDir, info.Name())
			b, err := ioutil.ReadFile(fullPath)
			if err != nil {
				return err
			}
			err = json.Unmarshal(b, vm)
			if err != nil {
				return err
			}
			listEntry := new(machine.ListResponse)

			listEntry.Name = vm.Name
			listEntry.Stream = vm.ImageStream
			listEntry.VMType = "wsl"
			listEntry.CPUs, _ = getCPUs(vm)
			listEntry.Memory, _ = getMem(vm)
			listEntry.DiskSize = getDiskSize(vm)
			fi, err := os.Stat(fullPath)
			if err != nil {
				return err
			}
			listEntry.CreatedAt = fi.ModTime()
			listEntry.LastUp = getLastStart(vm, fi.ModTime())
			if vm.isRunning() {
				listEntry.Running = true
			}

			listed = append(listed, listEntry)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return listed, err
}

func getDiskSize(vm *MachineVM) uint64 {
	vmDataDir, err := machine.GetDataDir(vmtype)
	if err != nil {
		return 0
	}
	distDir := filepath.Join(vmDataDir, "wsldist")
	disk := filepath.Join(distDir, vm.Name, "ext4.vhdx")
	info, err := os.Stat(disk)
	if err != nil {
		return 0
	}
	return uint64(info.Size())
}

func markStart(name string) error {
	vmDataDir, err := machine.GetDataDir(vmtype)
	if err != nil {
		return err
	}
	distDir := filepath.Join(vmDataDir, "wsldist")
	start := filepath.Join(distDir, name, "laststart")
	file, err := os.Create(start)
	if err != nil {
		return err
	}
	file.Close()

	return nil
}

func getLastStart(vm *MachineVM, created time.Time) time.Time {
	vmDataDir, err := machine.GetDataDir(vmtype)
	if err != nil {
		return created
	}
	distDir := filepath.Join(vmDataDir, "wsldist")
	start := filepath.Join(distDir, vm.Name, "laststart")
	info, err := os.Stat(start)
	if err != nil {
		return created
	}
	return info.ModTime()
}

func getCPUs(vm *MachineVM) (uint64, error) {
	dist := toDist(vm.Name)
	if run, _ := isWSLRunning(dist); !run {
		return 0, nil
	}
	cmd := exec.Command("wsl", "-d", dist, "nproc")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err = cmd.Start(); err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(out)
	var result string
	for scanner.Scan() {
		result = scanner.Text()
	}
	_ = cmd.Wait()

	ret, err := strconv.Atoi(result)
	return uint64(ret), err
}

func getMem(vm *MachineVM) (uint64, error) {
	dist := toDist(vm.Name)
	if run, _ := isWSLRunning(dist); !run {
		return 0, nil
	}
	cmd := exec.Command("wsl", "-d", dist, "cat", "/proc/meminfo")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err = cmd.Start(); err != nil {
		return 0, err
	}
	scanner := bufio.NewScanner(out)
	var (
		total, available uint64
		t, a             int
	)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if strings.HasPrefix(fields[0], "MemTotal") && len(fields) >= 2 {
			t, err = strconv.Atoi(fields[1])
			total = uint64(t) * 1024
		} else if strings.HasPrefix(fields[0], "MemAvailable") && len(fields) >= 2 {
			a, err = strconv.Atoi(fields[1])
			available = uint64(a) * 1024
		}
		if err != nil {
			break
		}
	}
	_ = cmd.Wait()

	return total - available, err
}

func IsValidVMName(name string) (bool, error) {
	infos, err := GetVMInfos()
	if err != nil {
		return false, err
	}
	for _, vm := range infos {
		if vm.Name == name {
			return true, nil
		}
	}
	return false, nil
}
