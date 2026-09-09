# Changelog

All notable changes to this project are documented here.

## [1.3.0] - 2026-09-09
### Highlights
- Experimental admin replication halt so a compromised source is not copied onto the replica (`docs/protection.md`). Off until enabled.
- Source offsets commit only after the target produce is acknowledged.
- Cluster and config APIs mask secrets (`***`); PUT `***` or empty leaves stored credentials unchanged.

### Security
- Dropped committed TLS private keys. Bind `server.host` (not `0.0.0.0`); TLS required in production unless `allow_insecure`.
- CORS uses configured origins. Login rate-limited. Password minimum 12 characters; password change revokes tokens.
- Dashboard HTML-escapes user-controlled text. WebSocket auth is Bearer-only.
- SSRF guards on AI and metrics egress URLs; optional `egress.allowed_hosts`.

### Fixes & operations
- Cluster purge route, Helm SQLite on the PVC, foreign keys/WAL, SASL on job clients, regex substitution, job lifecycle/deadlock fixes.
- Mirror verify uses source consumer-group lag, not cross-cluster high watermarks.
- Prometheus `/metrics` and Helm ServiceMonitor.

### Upgrade notes
- See `RELEASE_v1.3.0.md`. Protection is off until `mirror-cli protection enable`. Generate new TLS certs. Secret GET/PUT clients must treat `***` as leave-unchanged.

## [1.2.0] - 2026-01-19
### Highlights
- Mirror-first topic handling: same-name mirroring by default, regex capture substitution when configured.
- Dynamic regex topic discovery with target topic creation and partition/replication alignment.
- Metrics and incidents now distinguish consumed vs produced throughput and source vs target stalls.
- Compliance report scheduling and multi-provider AI routing improvements.

### Fixes & stability
- UI logout now clears session tokens; `/login.html` is served directly.
- Password reset no longer prints secrets to stdout; it writes to a file instead.
- Lag calculation includes partitions with no consumed records.

### Configuration
- New `replication.topic_discovery_interval` (default `5m`, `0` to disable).
- Database retention is configurable via `database.retention_days` (1–30).

### Upgrade notes
- Review the new password reset behavior (`mirror-cli users reset-password --password-file`).
- If you rely on regex mappings, prefer capture substitution (e.g., `orders-(.*)` -> `backup-$1`).

## [1.1.3] - 2025-11-26
### Highlights
- First open-source release of `kaf-mirror`; source code, binaries, and container images are now published under the project license.
- Public GitHub Releases now include version-stamped CLI binaries and GHCR container images for easy adoption.

### Fixes & stability
- Resolved the dashboard crash when clusters are absent/null and improved Chart.js canvas reuse.
- Fixed login loops when stale or compromised tokens are present.
- Release workflow hardened to ensure GitHub Releases succeed with the correct permissions.

### Packaging & distribution
- Prebuilt binaries for Linux, macOS, and Windows (amd64) are published with each tag.
- Multi-arch container images (`linux/amd64`, `linux/arm64`) are pushed to GHCR with both versioned and `latest` tags.

### Upgrade notes
- No breaking configuration changes; rolling upgrade is safe. Restart `kaf-mirror` or pull the new GHCR image to adopt 1.1.3.

## [1.1.0] - 2025-08
- First GA feature-complete release. See `RELEASE_v1.1.0.md` for the full notes.
