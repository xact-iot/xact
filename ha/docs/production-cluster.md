# Production Cluster Deployment

This directory is intended to be usable as an open-source deployment kit.

The Vagrant files create a local demonstration cluster, but the deployment path is SSH plus Ansible. That means the same playbooks can be run from Linux, macOS, or Windows with WSL against three real Linux servers.

## Target Architecture

The baseline cluster is:

* three Linux servers
* K3s server on each node
* embedded etcd on each node
* Kubernetes workloads scheduled on the same nodes

For production, place a stable TCP load balancer or virtual IP in front of the K3s API on port `6443`.

```text
admin host
  |
  | SSH
  v
node1  node2  node3
  \      |      /
   embedded etcd
```

## Control Host Requirements

Install these on the machine that runs the deployment:

* Git
* Ansible
* SSH client

For Windows, use WSL and install the tools inside WSL.

Host-side `kubectl` and Helm are optional. The scripts use SSH to run `k3s kubectl` on `node1`.

## Server Requirements

Each target server should have:

* Ubuntu Server 24.04 LTS or a compatible Linux distribution
* static IP address
* SSH access from the control host
* passwordless sudo for the Ansible user, or use Ansible become password prompting
* inbound node-to-node access for K3s and etcd ports

Minimum starting point:

| Resource | Per Node |
| -------- | -------: |
| CPU      | 2 vCPU   |
| RAM      | 4 GB     |
| Disk     | 30 GB    |

## Inventory

Copy the example inventory:

```bash
cd ha/ansible
cp inventory.example.ini inventory.ini
```

Edit `inventory.ini`:

```ini
[k3s_nodes]
node1 ansible_host=10.0.0.11 ansible_user=ubuntu
node2 ansible_host=10.0.0.12 ansible_user=ubuntu
node3 ansible_host=10.0.0.13 ansible_user=ubuntu

[k3s_master]
node1
```

## K3s Settings

Cluster defaults live in:

```text
ha/ansible/group_vars/all.yml
```

For repeatability, `k3s_version` is pinned. Change it deliberately during an upgrade.

For production, set `k3s_api_endpoint` to a stable load balancer or virtual IP:

```yaml
k3s_api_endpoint: "10.0.0.10"
k3s_tls_sans:
  - "10.0.0.10"
  - "k3s.example.com"
```

If you do not set `k3s_api_endpoint`, node2 and node3 join through node1. That is acceptable for the Vagrant demo, but a stable API endpoint is better for production operations.

## Deploy

From `ha`:

```bash
make ping
```

```bash
make prepare
```

```bash
make install
```

```bash
make verify
```

## Inspect

Use the SSH-based targets:

```bash
make nodes
```

```bash
make pods
```

These do not require `kubectl` on the control host.

## Local Vagrant Demo

For local testing:

```bash
cd ha
cp ansible/inventory.vagrant.ini ansible/inventory.ini
make up
make ping
make prepare
make install
make verify
```

The Vagrant environment is only a convenience for development and documentation.
