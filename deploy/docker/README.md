# XACT Docker Deployment

This directory contains the basic Docker deployment described in `architecture/DOCKER_REDUNDANCY.md`.

## Services

- `xact`: the Alpine-based XACT image. It contains the compiled server, restore tool, and built UI files under `/opt/xact/web`.
- `postgres`: `timescale/timescaledb-ha:pg18`, initialized with the `xact` database via `POSTGRES_DB`.
- `caddy`: reverse proxy and certificate manager for `/xact/` and `/xact/ws`.

PostgreSQL is published as `127.0.0.1:${POSTGRES_PORT:-5432}:5432`, so it is reachable from the Docker host only, not from external interfaces.
PostgreSQL data is stored in the host directory configured by `POSTGRES_DATA_DIR`, defaulting to `./postgres-data` beside `docker-compose.yml` in the extracted Docker deployment package.

## First Run

1. If you are deploying from a release package, extract it first:

   ```sh
   mkdir -p ~/xact-docker
   cp xact-docker-<arch>-<version>.tar.gz ~/xact-docker/
   cd ~/xact-docker
   tar -xzf xact-docker-<arch>-<version>.tar.gz
   ```

   Extract the package as the user who will run Docker Compose, not with `sudo`. The package contains `.env.example`, `docker-compose.yml`, and the writable `plugins/` and `postgres-data/` host directories.

2. Copy the example environment file:

   ```sh
   cp .env.example .env
   ```

3. Replace every `change-this...` value in `.env` with a long random secret.

4. Start the stack:

   ```sh
   docker compose up -d
   ```

5. Open XACT through Caddy:

   ```text
   http://localhost/xact/
   ```

For a public deployment, set `XACT_SITE_ADDRESS` to the public hostname before starting the stack. Caddy will manage certificates automatically.

## PostgreSQL Data and Passwords

The `POSTGRES_PASSWORD` value is used when PostgreSQL initializes an empty `POSTGRES_DATA_DIR`. After the database already exists, changing `.env` does not change the password stored inside PostgreSQL.

To change or recover the XACT database password on an existing deployment:

1. Choose a new password and put it in `.env` as `POSTGRES_PASSWORD`.
2. Reset the PostgreSQL role password inside the running database:

   ```sh
   docker compose exec postgres psql -U xact -d xact -c "ALTER USER xact WITH PASSWORD '<new-password>';"
   ```

3. Restart XACT so it reconnects using the updated `.env` value:

   ```sh
   docker compose up -d
   ```

If you do not need to keep any existing database data, stop the stack and remove `POSTGRES_DATA_DIR`; PostgreSQL will initialize a fresh database using the current `.env` values on the next `docker compose up -d`.

## Plugins

Plugins are loaded from the host path configured by `XACT_PLUGIN_DIR`:

```yaml
${XACT_PLUGIN_DIR:-./plugins}:/opt/xact/plugins
```

When left as `./plugins`, the path is beside `docker-compose.yml` in the extracted Docker deployment package. XACT creates the standard plugin category subdirectories there on first start. Put custom plugin files under those subdirectories and restart `xact` if needed.

## Building Locally

```sh
./server/build_deploy.sh --docker --docker-image xact:local
```

This builds the image and creates `server/deploy/xact-docker-<arch>-<version>.tar.gz`. Extract that package, edit `.env`, then point Compose at the local image:

```sh
XACT_IMAGE=xact:local docker compose up -d
```

The Dockerfile is intentionally runtime-only. It copies the `xact` binary, `restore` tool, and `web/` static files staged by `build_deploy.sh`; it does not install Node.js or Go.

## GitHub Container Registry

The workflow at `.github/workflows/docker-image.yml` publishes the image to:

```text
ghcr.io/<owner>/<repo>
```

It pushes on `main`, version tags such as `v1.2.3`, and manual workflow dispatch.
