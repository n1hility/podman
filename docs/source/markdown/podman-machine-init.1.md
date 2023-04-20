% podman-machine-init 1

## NAME
podman\-machine\-init - Initialize a new virtual machine

## SYNOPSIS
**podman machine init** [*options*] [*name*]

## DESCRIPTION

Initialize a new virtual machine for Podman.

Rootless only.

Podman on MacOS and Windows requires a virtual machine. This is because containers are Linux -
containers do not run on any other OS because containers' core functionality are
tied to the Linux kernel. Podman machine must be used to manage MacOS and Windows machines,
but can be optionally used on Linux.

**podman machine init** initializes a new Linux virtual machine where containers are run.
SSH keys are automatically generated to access the VM, and system connections to the root account
and a user account inside the VM are added.

By default, the VM distribution is [Fedora CoreOS](https://getfedora.org/en/coreos?stream=testing).
Fedora CoreOS upgrades come out every 14 days and are detected and installed automatically. The VM will be rebooted during the upgrade.
For more information on updates and advanced configuration, please see the FCOS update docs [here](https://docs.fedoraproject.org/en-US/fedora-coreos/auto-updates/) and [here](https://coreos.github.io/zincati/usage/updates-strategy/).

## OPTIONS

#### **--cpus**=*number*

Number of CPUs.

#### **--disk-size**=*number*

Size of the disk for the guest VM in GB.

#### **--help**

Print usage statement.

#### **--ignition-path**

Fully qualified path of the ignition file.

If an ignition file is provided, the file
will be copied into the user's CONF_DIR and renamed.  Additionally, no SSH keys will
be generated nor will a system connection be made.  It is assumed that the user will
do these things manually or handle otherwise.

#### **--image-path**

Fully qualified path or URL to the VM image.
Can also be set to `testing`, `next`, or `stable` to pull down default image.
Defaults to `testing`.

#### **--memory**, **-m**=*number*

Memory (in MB).

#### **--now**

Start the virtual machine immediately after it has been initialized.

#### **--rootful**

Whether this machine should prefer rootful (`true`) or rootless (`false`)
container execution. This option will also determine the remote connection default
if there is no existing remote connection configurations.

API forwarding, if available, will follow this setting.

#### **--timezone**

Set the timezone for the machine and containers.  Valid values are `local` or
a `timezone` such as `America/Chicago`.  A value of `local`, which is the default,
means to use the timezone of the machine host.

#### **--user-mode-networking**

Whether this machine should relay traffic from the guest through a user-space
process running on the host. In some VPN configurations the VPN may drop
traffic from alternate network interfaces, including VM network devices. By
enabling user-mode networking (a setting of `true`), VPNs will observe all
podman machine traffic as coming from the host, bypassing the problem.

When the qemu backend is used (Linux, Mac), user-mode networking is
mandatory and the only allowed value is `true`. In contrast, The Windows/WSL
backend defaults to `false`, and follows the standard WSL network setup.
Changing this setting to `true` on Windows/WSL will inform Podman to replace
the WSL networking setup on start of this machine instance with a user-mode
networking distribution. Since WSL shares the same kernal across
distributions, all other running distributions will reuse this network.
Likewise, when the last machine instance with a `true` setting stops, the
original networking setup will be restored

#### **--username**

Username to use for executing commands in remote VM. Default value is `core`
for FCOS and `user` for Fedora (default on Windows hosts). Should match the one
used inside the resulting VM image.

#### **--volume**, **-v**=*source:target[:options]*

Mounts a volume from source to target.

Create a mount. If /host-dir:/machine-dir is specified as the `*source:target*`,
Podman mounts _host-dir_ in the host to _machine-dir_ in the Podman machine.

Additional options may be specified as a comma-separated string. Recognized
options are:
* **ro**: mount volume read-only
* **rw**: mount volume read/write (default)
* **security_model=[model]**: specify 9p security model (see below)

The 9p security model [determines] https://wiki.qemu.org/Documentation/9psetup#Starting_the_Guest_directly
if and how the 9p filesystem translates some filesystem operations before
actual storage on the host.

In order to allow symlinks to work, on MacOS the default security model is
 *none*.

The value of *mapped-xattr* specifies that 9p store symlinks and some file
attributes as extended attributes on the host. This is suitable when the host
and the guest do not need to interoperate on the shared filesystem, but has
caveats for actual shared access; notably, symlinks on the host are not usable
on the guest and vice versa. If interoperability is required, then choose
*none* instead, but keep in mind that the guest will not be able to do things
that the user running the virtual machine cannot do, e.g. create files owned by
another user. Using *none* is almost certainly the best choice for read-only
volumes.

Example: `-v "$HOME/git:$HOME/git:ro,security_model=none"`

Default volume mounts are defined in *containers.conf*.  Unless changed, the default values
is `$HOME:$HOME`.

#### **--volume-driver**

Driver to use for mounting volumes from the host, such as `virtfs`.

## EXAMPLES

```
$ podman machine init
$ podman machine init myvm
$ podman machine init --rootful
$ podman machine init --disk-size 50
$ podman machine init --memory=1024 myvm
$ podman machine init -v /Users:/mnt/Users
```

## SEE ALSO
**[podman(1)](podman.1.md)**, **[podman-machine(1)](podman-machine.1.md)**

## HISTORY
March 2021, Originally compiled by Ashley Cui <acui@redhat.com>
