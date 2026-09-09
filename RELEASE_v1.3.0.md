# kaf-mirror v1.3.0 Release Notes

Release date: 2026-09-09

## Highlights
- Experimental **replication halt**: an admin kill-switch so a compromised source cluster is not copied onto the replica. Off until you enable it (`mirror-cli protection enable`). See `docs/protection.md`.
- Replication commits source offsets only after the target produce is acknowledged.
- APIs no longer return Kafka, AI, or HEC secrets. Cluster and config GET/export use `***`; PUT with `***` or empty keeps the stored value.

## Security
- Removed committed TLS private keys. Generate local certs; do not check keys into git.
- Server binds `server.host` (default `localhost`), not `0.0.0.0`. Production requires TLS unless `server.allow_insecure` is set.
- CORS uses configured origins and includes `Authorization`.
- Login is rate-limited. Passwords must be at least 12 characters. Changing a password revokes tokens.
- Assigning a role replaces the previous role instead of stacking.
- Dashboard HTML-escapes job names, users, and insight text. `/ws` accepts `Authorization: Bearer` only (query tokens rejected).
- Outbound AI, Splunk, Loki, and Pushgateway URLs cannot target loopback, link-local, or cloud metadata. Optional `egress.allowed_hosts`.

## Replication halt (experimental)
- Off by default. Admin (`protection:manage`) enables it via CLI or API.
- Halt with `mirror-cli protection halt`, `POST /api/v1/protection/halt`, file `data/HALT`, or `KAF_MIRROR_HALT=1`.
- While enabled, jobs that write **into** a cluster with `role: prod` are refused. Label clusters `prod`, `dr`, or `other` on add/edit.
- Auto-halt (same switch): high-entropy rewrite of existing keys, tombstone storms, error bursts. Heuristics can false-positive.
- Resume does not restart jobs. File/env halt still applies until you remove them.
- This does not encrypt-proof Kafka. Already-written kafscale/S3 objects stayed readable in a live incident because they are read-only.

## Fixes & operations
- `DELETE /api/v1/clusters/purge` is reachable (registered before `/:name`).
- Helm SQLite path is `/app/data/kaf-mirror.db` on the PVC. `Open()` uses the `InitDB` path. Foreign keys, busy timeout, and WAL are on.
- Live `mirror_progress` is not pruned. Job delete stops the replicator first.
- SASL/`security_config` is passed into cluster clients. Regex mappings substitute capture groups at consume time.
- Job update persists batch size, parallelism, and compression. Only `active` jobs restart on boot.
- `CONFIG_PATH` is honored. Config import runs `Validate()`.
- `POST /api/v1/auth/reset-token` exists for the CLI.
- Mirror verify uses source consumer-group lag and topic presence, not cross-cluster high watermarks.
- Prometheus `/metrics`, Helm ServiceMonitor, chart image `ghcr.io/scalytics/kaf-mirror:v1.3.0`.

## CLI
```bash
mirror-cli login
mirror-cli protection enable
mirror-cli protection status
mirror-cli protection halt "source compromised"
mirror-cli protection resume
mirror-cli protection disable
mirror-cli clusters add    # prompts for role: other / prod / dr
```

## Configuration changes
- New: `protection.*` (enabled default `false`), `egress.allowed_hosts`, `server.allow_insecure`.
- Cluster records: `role` (`prod` | `dr` | `other`).
- Helm: `database.path` `/app/data/kaf-mirror.db`; image tag `v1.3.0`.

## Upgrade notes
- Generate new TLS material; old repo keys are gone and must be treated as compromised if you used them.
- If the process bound `0.0.0.0` before, set `server.host` explicitly (for example `0.0.0.0` behind a proxy, or keep `localhost` and put TLS on the ingress).
- Production without TLS will refuse to start unless `server.allow_insecure: true`.
- Clients and UIs that stored cluster secrets from GET must send `***` or omit them on PUT, or they will not rotate credentials.
- Password-change callers must use ≥12 characters and must log in again (tokens revoked).
- WebSocket clients must send `Authorization: Bearer`; `?token=` is rejected.
- Helm: confirm the PVC is mounted and the DB file is under `/app/data`, not `/tmp`.
- Protection stays **off** until an admin enables it.
