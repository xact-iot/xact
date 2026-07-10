# HA Edge and XACT Deployment

The HA kit supports three deployment profiles:

* VM lab: VMware/Vagrant nodes with a floating lab VIP.
* Small on-prem: the same Keepalived and HAProxy edge pattern on real servers.
* Datacenter/cloud: provider load balancers in front of the K3s API and ingress ports.

The VM lab and small on-prem profiles intentionally share the same edge shape so
contributors can test node failure through the same stable user-facing address
that a small production cluster uses.

## Edge Contract

The default lab VIP is:

```text
192.168.57.100
```

It fronts these ports:

| Public port | Purpose | Backend |
| ---: | --- | --- |
| 6443 | K3s API | K3s server API on every node |
| 80 | HTTP redirect / ingress | Traefik NodePort |
| 443 | XACT HTTPS ingress | Traefik NodePort |
| 8883 | XACT MQTT TLS | XACT MQTT NodePort |
| 4222 | XACT NATS client TLS | HAProxy TLS termination to XACT NATS NodePort |

SSH is intentionally not placed behind the public edge. Use direct private node
IPs, a VPN, or a bastion host for administrative SSH.

## Why ServiceLB Is Disabled

K3s ships ServiceLB, and the default Traefik service can bind host ports 80 and
443 on every node. In this HA edge profile, HAProxy owns the VIP-facing ports on
the same nodes, so ServiceLB is disabled through:

```yaml
k3s_extra_server_args:
  - "--disable=servicelb"
```

Traefik remains installed. The edge playbook creates a stable NodePort service
named `kube-system/traefik-edge`:

```text
30080 -> Traefik web
30443 -> Traefik websecure
```

HAProxy listens on the VIP and forwards HTTP/HTTPS to those NodePorts. MQTT is
passed through to XACT's TLS MQTT listener. NATS client traffic terminates TLS
at HAProxy by default because the current embedded XACT NATS listener is plain
TCP inside the cluster.

## Deployment Order

For a fresh VM lab:

```bash
cd ha
make up
make ping
make prepare
make edge-deploy
make install
make edge-deploy
make configure-network
make verify
make edge-verify
make db-deploy
make db-verify
make xact-deploy
make xact-verify
```

The first `make edge-deploy` installs Keepalived and HAProxy before K3s so
node2 and node3 can join through the stable API VIP. The second run happens
after K3s is installed and creates the Traefik NodePort service.

For the VM lab defaults:

```yaml
edge_vip: "192.168.57.100"
edge_interface: "eth1"
k3s_api_endpoint: "{{ edge_vip }}"
k3s_tls_sans:
  - "{{ edge_vip }}"
```

For small on-prem, override these values in your production inventory or group
vars:

```yaml
edge_vip: "10.0.0.10"
edge_interface: "eno1"
k3s_api_endpoint: "10.0.0.10"
k3s_tls_sans:
  - "10.0.0.10"
  - "k3s.example.com"
```

For datacenter or cloud deployments, use the provider load balancer instead of
Keepalived/HAProxy and point `k3s_api_endpoint` plus public DNS at that
external load balancer.

## Local XACT Image Workflow

Published deployments can use:

```yaml
xact_image: "ghcr.io/xact-iot/xact:latest"
xact_image_pull_policy: "IfNotPresent"
```

For development, build and load a local image into every K3s node:

```bash
cd ha
make xact-build-local-image
make xact-load-local-image
make xact-deploy-local
make xact-verify
```

`make xact-deploy-local` sets:

```yaml
xact_image: "xact:local"
xact_image_pull_policy: "Never"
```

This avoids needing a registry while developing the application.

## XACT Replicas

The default XACT deployment uses three replicas:

```yaml
xact_replicas: 3
```

XACT runs as a StatefulSet so each pod has a stable network identity:

```text
xact-0.xact-headless.xact.svc.cluster.local
xact-1.xact-headless.xact.svc.cluster.local
xact-2.xact-headless.xact.svc.cluster.local
```

The embedded NATS servers form a route mesh on port `6222` using those stable
DNS names. This is the main reason the VM cluster mirrors the on-prem edge: it
lets contributors test XACT-level data distribution and failover, not only
Kubernetes scheduling.

Each XACT pod gets a small persistent NATS store volume. The VM lab still uses
`local-path` storage, so pod rebuild behavior is not identical to a production
storage design.

## Failure Test

After XACT is deployed, use the VIP for user-facing checks:

```text
https://192.168.57.100/xact/
mqtts://192.168.57.100:8883
NATS client with TLS enabled to 192.168.57.100:4222
```

Then stop or power off `node1` and verify that:

* the VIP moves to another node,
* HTTPS still reaches XACT,
* MQTT/NATS clients reconnect through the VIP,
* XACT pods maintain or reform the embedded NATS route mesh,
* Kubernetes still reports the surviving nodes and database pods.

The lab still uses local-path storage, so database member rebuild behavior is
not identical to a production storage design. The ingress and user-facing
failover path is intentionally close to the small on-prem profile.
