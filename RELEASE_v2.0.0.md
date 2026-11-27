# kaf-mirror v2.0.0 Release Notes

Release date: 2026-11-27

This is a major release with breaking changes. Read the upgrade notes and the Breaking section of `CHANGELOG.md` before you upgrade.

## Highlights
- A job whose source topic is deleted and created again follows the new topic by its Kafka topic ID and keeps copying. A deleted topic that is not created again keeps the job active, counts the errors in `error_count` and is listed in `missing_source_topics`.
- Replication checks protection before every target send, including retries.
- The Docker image and the Helm chart start with their defaults. The chart has an optional `volumePermissions` init container for storage that does not honor `fsGroup`, such as the `local-path` provisioner of kind and k3s.
- `mirror-cli` exits with status 1 when a command fails, and job commands refuse to run without a selected job instead of calling the API with an empty job id.
- Docker images report the release tag in the API, UI and CLI; local image builds report `dev`.
- `admin-cli` rejects unknown subcommands, `mirror-cli jobs add` stops on cancelled prompts, and server `--help` and `--version` exit without starting the server.
- Release downloads include `mirror-cli` for Linux arm64 and Apple Silicon.

## Breaking
- Builds require Go 1.27.4 or newer.
- kaf-mirror refuses to start on a data directory of another user that every user can write to without the sticky bit. On kind and k3s, add this values file and pass it with `-f` after `km-values.yaml`:

  ```yaml
  volumePermissions:
    enabled: true
  ```
- Removed flags: `mirror-cli login --password`, `mirror-cli users add --password`, `admin-cli users add --password` and `admin-cli import database --merge`. Passwords are read from a prompt without echo, or from the first line of stdin.
- `admin-cli restore`, `import` and `tokens reset`, and the destructive `mirror-cli` commands, need `--yes` without a terminal and exit with a non-zero status when they are not confirmed.
- API: list endpoints answer `[]` instead of `null`, `DELETE /api/v1/jobs/{id}` answers 404 for a missing job, job and cluster create and update validate their input (400, 404 and 409), and `error_count` also counts source fetch errors.
- `server.cors.allowed_origins` ships `["http://localhost:8080"]` instead of `["*"]`.

## Security
- APIs and `mirror-cli` no longer print API tokens, keys or passwords. The token file is always mode `0600`.
- Dependency updates for fiber, gomarkdown, klauspost/compress and brotli. Built with Go 1.27.4.

## Docs
- New `docs/configuration.md` with every config key and environment variable.
- kaf-mirror with idempotent writes on is verified with [KafScale v2.1.0](https://github.com/KafScale/platform/releases/tag/v2.1.0) and later. `KAFMIRROR_DISABLE_IDEMPOTENT_WRITES=true` is needed only for KafScale before v2.1.0. With KafScale v2.1.0, pre-create every target topic with the source partition count regardless of `KAFSCALE_AUTO_CREATE_TOPICS`; see [the KafScale guide](docs/kafscale-backup.md).
- Install docs cover kind with Docker Engine 29 and its containerd image store.

## Upgrade notes
- Replace the binaries or pull `ghcr.io/scalytics/kaf-mirror:v2.0.0`. The Helm chart is version `0.3.0` and defaults to the `v2.0.0` image.
- Check the Breaking section of `CHANGELOG.md` for scripts and API clients that need changes.
