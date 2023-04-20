% podman-machine-set 1

## NAME
podman\-machine\-set - Sets a virtual machine setting

## SYNOPSIS
**podman machine set** [*options*] [*name*]

## DESCRIPTION

Change a machine setting.

Rootless only.

## OPTIONS

#### **--cpus**=*number*

Number of CPUs.
Only supported for QEMU machines.

#### **--disk-size**=*number*

Size of the disk for the guest VM in GB.
Can only be increased. Only supported for QEMU machines.

#### **--help**

Print usage statement.

#### **--memory**, **-m**=*number*

Memory (in MB).
Only supported for QEMU machines.

#### **--rootful**

Whether this machine should prefer rootful (`true`) or rootless (`false`)
container execution. This option will also update the current podman
remote connection default if it is currently pointing at the specified
machine name (or `podman-machine-default` if no name is specified).

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
original networking setup will be restored.

Unlike [**podman system connection default**](podman-system-connection-default.1.md)
this option will also make the API socket, if available, forward to the rootful/rootless
socket in the VM.

## EXAMPLES

To switch the default VM `podman-machine-default` from rootless to rootful:

```
$ podman machine set --rootful
```

or more explicitly:

```
$ podman machine set --rootful=true
```

To switch the default VM `podman-machine-default` from rootful to rootless:
```
$ podman machine set --rootful=false
```

To switch the VM `myvm` from rootless to rootful:
```
$ podman machine set --rootful myvm
```

## SEE ALSO
**[podman(1)](podman.1.md)**, **[podman-machine(1)](podman-machine.1.md)**

## HISTORY
February 2022, Originally compiled by Jason Greene <jason.greene@redhat.com>
