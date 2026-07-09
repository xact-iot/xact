# TimescaleDB HA Deployment

The database deployment uses the K3s Helm controller. Helm does not need to be installed on the control host.

The deployment installs the upstream TimescaleDB HA chart:

* repository: `https://charts.timescale.com`
* chart: `timescaledb-single`
* version: `0.33.1`

The chart creates a three-pod StatefulSet managed by Patroni, with one primary service and one replica service.

The deployment downloads the pinned chart archive on the control host and embeds it into the K3s `HelmChart` object. This avoids requiring the Kubernetes cluster itself to have outbound internet access to the Helm repository.

The deployment also applies a small RBAC compatibility Role for Patroni service management. This is kept as a separate Ansible-managed manifest so the upstream chart remains pinned and unmodified.

## Configuration

Defaults live in:

```text
ha/ansible/group_vars/all.yml
```

Important settings:

```yaml
timescaledb_namespace: "database"
timescaledb_release_name: "xact-timescaledb"
timescaledb_storage_class: "local-path"
timescaledb_data_size: "10Gi"
timescaledb_wal_size: "2Gi"
timescaledb_replicas: 3
```

For production storage, replace `local-path` with a storage class appropriate for your environment.

The chart archive is cached locally under:

```text
ha/.cache/charts/
```

That cache directory is ignored by Git.

## Secrets

Database passwords are not committed to the repository.

During deployment, Ansible checks whether this Kubernetes Secret exists:

```text
database/xact-timescaledb-credentials
```

If it does not exist, the playbook creates it on the cluster with generated values for:

* `PATRONI_SUPERUSER_PASSWORD`
* `PATRONI_REPLICATION_PASSWORD`
* `PATRONI_admin_PASSWORD`

If you want to supply your own credentials, create that Secret before running `make db-deploy`.

## Deploy

From `ha`:

```bash
make db-deploy
```

Then verify:

```bash
make db-verify
```

For a quick status view:

```bash
make db-status
```

If the Helm install job is retrying or `make db-verify` times out:

```bash
make db-logs
```

## Services

Inside the cluster, applications should connect to:

```text
xact-timescaledb.database.svc.cluster.local:5432
```

Read-only replica traffic can use:

```text
xact-timescaledb-replica.database.svc.cluster.local:5432
```
