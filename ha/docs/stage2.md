# Stage 2 - Guided K3s Bring-Up

This stage turns the Stage 1 files into a repeatable workflow.

The goal is to create the three VMs, install a three-server K3s cluster, verify that Kubernetes is healthy, and copy a kubeconfig back to the host.

Run all commands in this directory:

```bash
cd ha
```

## 1. Start the virtual machines

```bash
make up
```

This uses `Vagrantfile` to create:

| Node  | IP Address     |
| ----- | -------------- |
| node1 | 192.168.57.101 |
| node2 | 192.168.57.102 |
| node3 | 192.168.57.103 |

Check their state:

```bash
make status
```

### Host-side VMware checks

If `make status` or `make up` fails before it reaches the VMs, first confirm Vagrant can see the VMware provider:

```bash
vagrant plugin list
```

The local lab expects the `vmware_desktop` provider. VMware Workstation or Fusion must also be installed and usable on the host.

If Vagrant reports that the VMware provider is missing, install or repair the Vagrant VMware provider before continuing. If VMware itself cannot start VMs, repair VMware on the host first; rebuilding the K3s cluster will not fix a broken hypervisor.

For a completely clean lab rebuild, destroy the old Vagrant machines first:

```bash
make destroy
```

Then start fresh:

```bash
make up
```

## 2. Test Ansible connectivity

The default Makefile inventory is `ansible/inventory.vagrant.ini`, which is
only for the local VMware lab. Production deployments should pass
`INVENTORY=inventory.ini` after creating a production inventory.

```bash
make ping
```

Ansible should report `SUCCESS` for all three nodes.

If this fails, the usual causes are:

* the VMs are not running
* VMware private networking is not available
* Vagrant has not created the SSH keys yet

## 3. Prepare the operating systems

```bash
make prepare
```

This playbook updates apt metadata, installs basic packages, disables swap, sets hostnames, and makes the node names resolvable through `/etc/hosts`.

Kubernetes expects swap to be disabled because the scheduler makes memory decisions assuming the node has predictable memory pressure behavior.

## 4. Install K3s HA

```bash
make install
```

The playbook:

1. starts `node1` as the first K3s server with embedded etcd
2. reads the cluster join token
3. joins `node2` and `node3` as additional K3s servers

All three nodes are control-plane nodes. That is what makes this a small HA cluster rather than one control plane plus two workers.

## 5. Verify from inside the cluster

```bash
make verify
```

This runs `k3s kubectl get nodes -o wide` and `k3s kubectl get pods -A -o wide` on `node1`.

You want to see all three nodes in `Ready` state.

## 6. Fetch kubeconfig to the host

```bash
make kubeconfig
```

This writes:

```text
ha/k3s/kubeconfig
```

The playbook rewrites the default K3s API address from `127.0.0.1` to `192.168.57.101`, so host-side `kubectl` can talk to the cluster.

Then test through SSH on `node1`:

```bash
make nodes
```

```bash
make pods
```

These targets use Ansible to run `k3s kubectl` on `node1`. This keeps the deployment procedure independent of whether the host has `kubectl` installed.

For convenience on a development workstation, you can also use host-side `kubectl`.

First fetch a kubeconfig:

```bash
make kubeconfig
```

Then, if `kubectl` is installed on the host:

```bash
make local-nodes
```

```bash
make local-pods
```

If the host does not have `kubectl` installed, you can optionally fetch the K3s binary from `node1`:

```bash
make k3s-bin
```

The K3s binary includes a `kubectl` subcommand. This is only a development convenience, not a deployment requirement.

## What You Have Built

At the end of this stage:

* VMware is running three Ubuntu nodes
* each node is a K3s server
* K3s uses embedded etcd
* the host has a kubeconfig for the cluster
* you can inspect the cluster with `kubectl`

The next stage is storage and database HA. For TimescaleDB, the important decision is whether to use a full operator-based PostgreSQL lifecycle or continue with the local chart scaffold under `charts/timescaledb-ha`.
