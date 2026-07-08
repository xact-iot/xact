# Stage 1 — K3s High Availability Cluster

## Prerequisites

This stage creates a three-node HA Kubernetes cluster using K3s running inside VirtualBox virtual machines. The same process is intended to be reusable later with physical machines replacing the VMs.

The following tools are required on the development host:

### Required software

| Tool                            | Purpose                                                 |
| ------------------------------- | ------------------------------------------------------- |
| Linux host (Ubuntu recommended) | Development environment                                 |
| VirtualBox                      | Virtual machine provider                                |
| Vagrant                         | Automated VM creation and lifecycle management          |
| Ansible                         | Automated operating system and Kubernetes configuration |
| Git                             | Source control and deployment repository management     |

### Installation verification

Confirm the tools are installed:

```bash
virtualbox --version
```

```bash
vagrant --version
```

```bash
ansible --version
```

```bash
git --version
```

### Recommended host resources

The development machine should have sufficient resources to run three Kubernetes nodes simultaneously.

Minimum recommended:

| Resource   | Requirement |
| ---------- | ----------: |
| CPU cores  |           6 |
| RAM        |       16 GB |
| Disk space |  50 GB free |

---

# Objective

At the end of this stage, a three-node highly available K3s cluster should be running.

The cluster architecture:

```
                    K3s HA Cluster

        ┌──────────────┼──────────────┐
        │              │              │
        ▼              ▼              ▼
      node1          node2          node3

   control-plane  control-plane  control-plane
       etcd           etcd           etcd
```

The cluster uses:

* K3s Kubernetes distribution
* Embedded etcd datastore
* Three server nodes
* VirtualBox virtual machines for development

---

# Directory Structure

The deployment repository:

```
ha/

├── Vagrantfile
├── Makefile
│
├── ansible/
│   ├── inventory.ini
│   ├── prepare.yml
│   └── install-k3s.yml
│
├── k3s/
│
├── postgres/
│
├── helm/
│
├── scripts/
│
└── docs/
    └── stage1-k3s-ha.md
```

---

# Virtual Machine Configuration

Three Ubuntu Server virtual machines are created using Vagrant.

| Node  | IP Address     | Purpose          |
| ----- | -------------- | ---------------- |
| node1 | 192.168.56.101 | First K3s server |
| node2 | 192.168.56.102 | K3s server       |
| node3 | 192.168.56.103 | K3s server       |

Each VM:

* Ubuntu 24.04 LTS
* 2 CPUs
* 4 GB RAM
* Private network interface

---

# Create the Virtual Machines

Start the cluster:

```bash
vagrant up
```

Verify status:

```bash
vagrant status
```

Expected:

```
node1 running
node2 running
node3 running
```

Test connectivity:

```bash
vagrant ssh node1
```

Inside the VM:

```bash
ping 192.168.56.102
ping 192.168.56.103
```

---

# Ansible Configuration

Ansible is used so that the same configuration can later be applied to physical machines.

Inventory:

```
ansible/inventory.ini
```

Example:

```ini
[k3s_nodes]
node1 ansible_host=192.168.56.101
node2 ansible_host=192.168.56.102
node3 ansible_host=192.168.56.103

[k3s_master]
node1
```

Test connectivity:

```bash
ansible k3s_nodes -i ansible/inventory.ini -m ping
```

Expected:

```
node1 | SUCCESS
node2 | SUCCESS
node3 | SUCCESS
```

---

# Prepare the Nodes

The preparation playbook:

```
ansible/prepare.yml
```

performs:

* Operating system updates
* Installation of required packages
* Swap disabling
* Hostname configuration

Run:

```bash
ansible-playbook \
-i ansible/inventory.ini \
ansible/prepare.yml
```

Expected:

```
PLAY RECAP

node1 ok
node2 ok
node3 ok
```

---

# Install K3s HA

The K3s installation playbook:

```
ansible/install-k3s.yml
```

performs:

1. Creates the initial K3s server on node1.
2. Initializes embedded etcd.
3. Retrieves the cluster token.
4. Joins node2 and node3 as additional control-plane nodes.

The nodes advertise their private network addresses:

```
node1 192.168.56.101
node2 192.168.56.102
node3 192.168.56.103
```

This ensures Kubernetes certificates include the correct addresses.

Run:

```bash
ansible-playbook \
-i ansible/inventory.ini \
ansible/install-k3s.yml
```

---

# Verify the Cluster

Connect to node1:

```bash
vagrant ssh node1
```

Check nodes:

```bash
sudo kubectl get nodes -o wide
```

Expected:

```
NAME    STATUS   ROLES
node1   Ready    control-plane,etcd
node2   Ready    control-plane,etcd
node3   Ready    control-plane,etcd
```

The cluster should show all nodes using the private IP addresses.

---

# HA Failure Test

The purpose of this stage is to verify that the Kubernetes control plane continues operating when one node fails.

Stop node1:

```bash
vagrant halt node1
```

Wait approximately 30 seconds.

Connect to node2:

```bash
vagrant ssh node2
```

Check cluster status:

```bash
sudo kubectl get nodes
```

Expected:

```
node1   NotReady
node2   Ready
node3   Ready
```

The Kubernetes API should still be available because etcd quorum remains:

```
node2 + node3 = quorum
```

Restore node1:

```bash
vagrant up node1
```

After recovery:

```bash
sudo kubectl get nodes
```

should show:

```
node1   Ready
node2   Ready
node3   Ready
```

---

# Stage 1 Completion Criteria

Stage 1 is complete when:

* [x] Three virtual machines can be recreated automatically.
* [x] Ansible can configure all nodes.
* [x] K3s is installed automatically.
* [x] Three control-plane nodes are running.
* [x] Embedded etcd is healthy.
* [x] The cluster survives failure of one node.

---

# Next Stage

Stage 2 will add highly available PostgreSQL with the TimescaleDB extension.

The next components introduced will be:

* Kubernetes persistent storage
* Distributed storage layer
* PostgreSQL HA deployment
* TimescaleDB extension verification
* Database failover testing

The goal is to reach a production-like three-node IoT platform where application state remains available during node failures.
