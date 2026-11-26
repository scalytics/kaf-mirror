# Getting started

How to run kaf-mirror locally with Docker, from a prebuilt image, or from binaries you build yourself. The 60 second version is in the [README](../README.md#quick-start). For the Helm chart see [kubernetes.md](kubernetes.md).

## Run locally with Docker

- Start everything (app + two local Kafka brokers) from the repo root:
  ```bash
  make dev-up   # uses docker compose
  ```
- The UI/API is at `http://localhost:8080`. The compose file uses `configs/docker.yml`, which binds `0.0.0.0` so the published port answers. Production requires TLS unless `server.allow_insecure` is set.
- Data and logs are in the Docker volumes `kaf-mirror_kaf-mirror-data` (`/app/data`) and `kaf-mirror_kaf-mirror-logs` (`/app/logs`). They are kept when the container restarts or is recreated.
- Stop and clean. This also deletes the data and log volumes:
  ```bash
  make dev-down
  ```

Admin bootstrap is automatic. On first start, the server seeds roles and permissions and creates `admin@localhost` with a random password. The password is not printed. The server writes it to a file named `kaf-mirror-bootstrap-<random>` in the database directory, readable only by the kaf-mirror user, and prints the file path in the log. In the container that directory is `/app/data`, so the file is on the data volume and survives a restart. Read the password, deliver it securely, then delete the file:
```bash
docker compose -p kaf-mirror logs kaf-mirror | grep "Password File"
docker compose -p kaf-mirror exec kaf-mirror sh -c 'cat /app/data/kaf-mirror-bootstrap-*'
docker compose -p kaf-mirror exec kaf-mirror sh -c 'rm /app/data/kaf-mirror-bootstrap-*'
```
If you need to recreate roles or bootstrap again when the DB is empty:
```bash
docker compose -p kaf-mirror exec kaf-mirror ./admin-cli repair
```
To reset the initial admin password. `admin-cli` writes the new password to `/tmp/kaf-mirror-admin-reset-<random>` in the container and prints that path:
```bash
docker compose -p kaf-mirror exec kaf-mirror ./admin-cli reset-admin-password admin@localhost
docker compose -p kaf-mirror exec kaf-mirror sh -c 'cat /tmp/kaf-mirror-admin-reset-*'
docker compose -p kaf-mirror exec kaf-mirror sh -c 'rm /tmp/kaf-mirror-admin-reset-*'
```

## Run from an image

The listen address, the `/app/data` and `/app/logs` layout, and the bootstrap password file next to the database are in the `v2.0.0` image and in the image built from this repository. `v1.3.1` and older images do not include them. To run the current `main` branch, build and run locally. Keep `/app/data` (SQLite and the bootstrap password file) and `/app/logs` on named volumes so they survive a restart or a new container:
```bash
docker build -t kaf-mirror:local .
docker run -d --name kaf-mirror -p 8080:8080 \
  -v kaf-mirror-data:/app/data \
  -v kaf-mirror-logs:/app/logs \
  kaf-mirror:local
```
`make docker-run` does the same, in the foreground, with the local `kaf-mirror:latest` tag. To run a published release instead, replace `kaf-mirror:local` with `ghcr.io/scalytics/kaf-mirror:v2.0.0`, or with `ghcr.io/scalytics/kaf-mirror:latest`, which points at the newest published release:
```bash
docker pull ghcr.io/scalytics/kaf-mirror:v2.0.0
```
The UI/API will be at `http://localhost:8080`:
```bash
curl -s http://localhost:8080/health
```
Stop the container with `docker stop -t 45 kaf-mirror`, so it has time to stop its jobs. See "Graceful Shutdown" in [resilience.md](resilience.md).

A bind mount also works when the container user, UID 100 or GID 101, can write to the host directory. The directory may belong to another user, for example group 101 with mode `0775`. A directory of another user that every user can write to, such as `root` with mode `0777`, stops the start unless it has the sticky bit (mode `1777`), because any user could replace the database files in it. The error names the directory, its mode and its owner. kaf-mirror sets the data directory to mode `0700` only when it owns it. Otherwise it leaves the mode alone and logs a warning when the directory is open to the group or others. The database, its `-wal` and `-shm` files and `kaf-mirror.db.lock` are always created with mode `0600`. An existing database file of another user that the group or others can read stops the start, because kaf-mirror cannot make it private. For a private data directory on the host, give it to the container user:
```bash
mkdir -p kaf-mirror-data
sudo chown 100:101 kaf-mirror-data
docker run -d --name kaf-mirror -p 8080:8080 \
  -v "$PWD/kaf-mirror-data:/app/data" \
  -v kaf-mirror-logs:/app/logs \
  kaf-mirror:local
```
If the data directory is not writable, kaf-mirror exits with `Refusing to start` and the UID and GID it needs. Only a lock held by a restore, an import or another kaf-mirror process gives the hint about waiting for a restore.

The image sets `KAF_MIRROR_SERVER_HOST=0.0.0.0`, so the server listens on all container interfaces. The bundled `configs/default.yml` keeps `host: "localhost"` for runs outside Docker. To listen on another address, pass `-e KAF_MIRROR_SERVER_HOST=<address>`. The image runs in `development` mode. If you mount a config with `mode: "production"`, give it TLS or set `server.allow_insecure`.

Read the bootstrap admin password, then delete the file:
```bash
docker logs kaf-mirror | grep "Password File"
docker exec kaf-mirror sh -c 'cat /app/data/kaf-mirror-bootstrap-*'
docker exec kaf-mirror sh -c 'rm /app/data/kaf-mirror-bootstrap-*'
```

## Build binaries on your host

```bash
make build
```
This regenerates the docs and produces `bin/kaf-mirror`, `bin/admin-cli`, and `bin/mirror-cli` for direct use outside Docker. To build only the binaries:
```bash
make binaries
```
`make release-binaries VERSION=v2.0.0` builds the release assets into `dist/` with the same commands the release workflow uses. It needs `x86_64-linux-gnu-gcc` for the Linux builds. Each binary is built from its package, for example `go build ./cmd/admin-cli`, not from a single `main.go`.
