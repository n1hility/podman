//go:build darwin
// +build darwin

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/pkg/errors"
)

const (
	dockerSock = "/var/run/docker.sock"
	fail       = "NO"
	success    = "OK"
)

const launchConfig = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.github.containers.podman.helper-{{.User}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Program}}</string>
		<string>service</string>
		<string>{{.Target}}</string>
	</array>
	<key>inetdCompatibility</key>
	<dict>
		<key>Wait</key>
		<false/>
	</dict>
	<key>UserName</key>
	<string>root</string>
	<key>Sockets</key>
	<dict>
		<key>Listeners</key>
		<dict>
			<key>SockFamily</key>
			<string>Unix</string>
			<key>SockPathName</key>
			<string>/private/var/run/podman-helper-{{.User}}.socket</string>
			<key>SockPathOwner</key>
			<integer>{{.UID}}</integer>
			<key>SockPathMode</key>
			<!-- SockPathMode takes base 10 (384 = 0600) -->
			<integer>384</integer>
			<key>SockType</key>
			<string>stream</string>
		</dict>
	</dict>
</dict>
</plist>
`

type launchParams struct {
	Program string
	User    string
	UID     string
	Target  string
}

// Note, this code is security sensitive since it runs under privilege.
// Limit actions to what is strictly necessary, and take use appropriate
// safeguards
//
// After installation the service call is ran under launchd in a nowait
// inetd style fashion, so stdin, stdout, and stderr are all pointing to
// an accepted connection
//
// This service is installed once per user and will redirect
// /var/run/docker to the fixed user-assigned unix socket location.
//
// Control communication is restricted to each user specific service via
// unix file permissions

func main() {
	usageFmt := "Usage: %s [install | uninstall | service]\n"
	prog, err := getProgram()
	if err != nil {
		prog = "unknown"
	}

	if len(os.Args) < 2 {
		fmt.Printf(usageFmt, filepath.Base(prog))
		os.Exit(1)
	}

	if os.Geteuid() != 0 {
		fmt.Printf("Must be ran as root via sudo or osascript. Run the following:\nsudo %s %s\n", prog, os.Args[1])
		os.Exit(1)
	}

	switch os.Args[1] {
	case "install":
		err = install()
	case "uninstall":
		err = uninstall()
	case "service":
		os.Exit(service())
	default:
		fmt.Println(usageFmt, filepath.Base(prog))
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(2)
	}

	os.Exit(0)
}

func getProgram() (string, error) {
	exec, err := os.Executable()
	if err != nil {
		return "", err
	}

	exec, err = filepath.EvalSymlinks(exec)
	return exec, err
}

func getUserInfo(name string) (string, string, string, error) {
	// We exec id instead of using user.Lookup to remain compat
	// with CGO disabled.
	cmd := exec.Command("/usr/bin/id", "-P", name)
	output, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", "", err
	}

	if err := cmd.Start(); err != nil {
		return "", "", "", err
	}

	entry := readCapped(output)
	elements := strings.Split(entry, ":")
	if len(elements) < 9 || elements[0] != name {
		return "", "", "", errors.New("Could not lookup user")
	}

	return elements[0], elements[2], elements[8], nil
}

func getUser() (string, string, string, error) {
	name, found := os.LookupEnv("SUDO_USER")
	if !found {
		name, found = os.LookupEnv("USER")
		if !found {
			return "", "", "", errors.New("could not determine user")
		}
	}

	_, uid, home, err := getUserInfo(name)
	if err != nil {
		return "", "", "", fmt.Errorf("could not lookup user: %s", name)
	}
	id, err := strconv.Atoi(uid)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid uid for user: %s", name)
	}
	if id == 0 {
		return "", "", "", fmt.Errorf("unexpected root user")
	}

	return name, uid, home, nil
}

func install() error {
	// TODO - We need to either copy or ensure the referenced binary has a path
	//        fully owned by root
	prog, err := getProgram()
	if err != nil {
		return err
	}

	userName, uid, homeDir, err := getUser()
	if err != nil {
		return err
	}

	target := filepath.Join(homeDir, ".local", "share", "containers", "podman", "machine", "podman.sock")

	var buf bytes.Buffer
	t := template.Must(template.New("launchdConfig").Parse(launchConfig))
	err = t.Execute(&buf, launchParams{prog, userName, uid, target})
	if err != nil {
		return err
	}

	labelName := fmt.Sprintf("com.github.containers.podman.helper-%s.plist", userName)
	fileName := filepath.Join("/Library", "LaunchDaemons", labelName)
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("helper is already installed, uninstall first")
		}
		return errors.Wrap(err, "error creating helper plist file")
	}
	defer file.Close()
	_, err = buf.WriteTo(file)
	if err != nil {
		return err
	}

	if err = runDetectErr("launchctl", "load", fileName); err != nil {
		return errors.Wrap(err, "launchctl failed loading service")
	}

	return nil
}

func uninstall() error {
	userName, _, _, err := getUser()
	if err != nil {
		return err
	}

	labelName := fmt.Sprintf("com.github.containers.podman.helper-%s", userName)
	fileName := filepath.Join("/Library", "LaunchDaemons", labelName+".plist")

	if err = runDetectErr("launchctl", "unload", fileName); err != nil {
		// Try removing the service by label in case the service is half uninstalled
		if rerr := runDetectErr("launchctl", "remove", labelName); rerr != nil {
			// Exit code 3 = no service to remove
			if exitErr, ok := rerr.(*exec.ExitError); !ok || exitErr.ExitCode() != 3 {
				fmt.Fprintf(os.Stderr, "Warning: service unloading failed: %s\n", err.Error())
				fmt.Fprintf(os.Stderr, "Warning: remove also failed: %s\n", rerr.Error())
			}
		}
	}

	if err := os.Remove(fileName); err != nil {
		if !os.IsNotExist(err) {
			return errors.Errorf("could not remove plist file: %s", fileName)
		}
	}

	return nil
}

func service() int {
	defer os.Stdout.Close()
	defer os.Stdin.Close()
	defer os.Stderr.Close()
	if len(os.Args) < 3 {
		fmt.Print(fail)
		return 1
	}
	target := os.Args[2]

	request := make(chan bool)
	go func() {
		buf := make([]byte, 3)
		_, err := io.ReadFull(os.Stdin, buf)
		request <- err == nil && string(buf) == "GO\n"
	}()

	valid := false
	select {
	case valid = <-request:
	case <-time.After(5 * time.Second):
	}

	if !valid {
		fmt.Println(fail)
		return 2
	}

	err := os.Remove(dockerSock)
	if err == nil || os.IsNotExist(err) {
		err = os.Symlink(target, dockerSock)
	}

	if err != nil {
		fmt.Print(fail)
		return 3
	}

	fmt.Print(success)
	return 0
}

// Used for commands that don't return a proper exit code
func runDetectErr(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	errReader, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err == nil {
		errString := readCapped(errReader)
		if len(errString) > 0 {
			re := regexp.MustCompile(`\r?\n`)
			err = errors.New(re.ReplaceAllString(errString, ": "))
		}
	}

	if werr := cmd.Wait(); werr != nil {
		err = werr
	}

	return err
}

func readCapped(reader io.Reader) string {
	// Cap output
	buffer := make([]byte, 2048)
	n, _ := io.ReadFull(reader, buffer)
	_, _ = io.Copy(ioutil.Discard, reader)
	if n > 0 {
		return string(buffer[:n])
	}

	return ""
}
