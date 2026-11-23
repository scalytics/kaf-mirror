# Changelog

All notable changes to this project are documented here.

## [Unreleased]
### Fixes
- Replicated records keep the source timestamp. Before, the target record got the time of the copy (#27).
- Replicated records keep the source partition. The producer now writes each record to the partition it came from. Before, the producer default partitioner chose the target partition (#28).
- A record whose produce fails is no longer skipped with its offset committed. kaf-mirror tries it up to 5 times, waiting 1s, 2s, 4s and 8s between the attempts. If the fifth attempt fails, the job stops with status `failed` and the source offset stays at that record. Stopping a job while the target is down no longer hangs and does not commit the waiting record. See "Resilience and Buffering" in the README (#32).
- A job that hits the producer error limit, a regex mapping that topic discovery cannot apply, or a panic no longer hangs as `active` and keeps its partitions. It stops, leaves the source consumer group and is marked `failed` with the reason. A failure of an instance that was already stopped or restarted is ignored, so it no longer stops the new instance or marks the job `failed`. Restarting a failed job clears `failed_reason`. Before, the job showed `active` with the old reason. See "Failed Jobs" in the README (#33).
- A topic discovery run that fails because the source or target cannot be reached, or because a new target topic cannot be created, no longer stops the job. kaf-mirror logs a warning, keeps copying the topics it already has and tries again at the next discovery run. Stopping a job while discovery waits to retry a topic creation no longer blocks for up to 15 seconds. See "Topic Discovery During an Outage" in the README.
- The Docker image starts with the documented `docker run`. It creates `/app/data` and `/app/logs` owned by the kaf-mirror user, declares both as volumes and sets `KAF_MIRROR_SERVER_HOST=0.0.0.0`, so the published port answers. `configs/default.yml` keeps `localhost` for runs outside Docker. `make dev-up` uses named volumes, because bind mounts created by Docker were owned by root and the server could not write to them (#34).
- The bootstrap admin password file is written next to the database, for example `/app/data` in the container, instead of the system temp directory. It now survives a container restart. The README shows how to read it with `docker exec` or `kubectl exec` (#34).
- The default Helm install starts. The chart sets `server.allow_insecure` from `config.server.allowInsecure` (default `true`) and can take the TLS certificate and key from a Secret with `config.server.tls.existingSecret`. Before, the pod crashlooped because production mode requires TLS (#35).
- The Helm chart uses the `Recreate` strategy, so two pods never open the SQLite database at the same time, and the render fails if `replicaCount` is not 1. It drops values from another project (the `qip.*` topics, the `job_id`, the `ops-foundation` label and the unused `source_cluster`, `target_cluster`, `topics` and `metrics.port` keys), adds `secretEnv` for tokens from a Secret, and now reads `tolerations` and `affinity`. When TLS is on, the ServiceMonitor scrapes HTTPS. CI lints and renders the chart (#52).
- An exact topic name with a dot, such as `pay.v1`, is no longer treated as a regex, so it no longer also copies `pay.v1.dlq`, `my-pay.v1` or `payXv1`. A pattern is a regex only when it has a regex character other than the dot (`\ ^ $ * + ? ( ) [ ] { } |`), and a regex must match the whole topic name. One shared resolver applies this rule at job start, in topic discovery, when routing records, in the mirror state, verify, lag and topic health views, and when mappings are saved. An exact mapping now wins over a regex for the same topic. See "Topic Mappings" in the README (#37).
- Missing target topics are created again. A topic counted as existing when the broker listed it with an error such as `UNKNOWN_TOPIC_OR_PARTITION`, so job start and discovery skipped the creation and then failed. Now only topics listed without an error exist, `TOPIC_ALREADY_EXISTS` from the create call counts as success, and after a create kaf-mirror waits until the topic is visible in metadata before the start check re-reads the cluster (#38).
- A regex mapping without a target keeps the topic names (`$0`). Before, the target became the pattern itself and the job failed with `target topic app-.* is mapped from multiple sources` (#39).
- `replication.topic_discovery_interval` is used. Before, every job discovered new topics every 5 minutes whatever the config said. A failed discovery run is retried at the next configured interval (#40).
- The mirror state, the start check of a force restart, mirror verify, resume points and the topic health checks resolve regex mappings to the matching topics. Before, they asked the broker for a topic named like the pattern, for example `app-.*`, and got `INVALID_TOPIC_EXCEPTION` (#46).
- `GET /api/v1/jobs/{id}/lag` finds the source cluster of the job in the database and reads the lag of the job consumer group `kaf-mirror-job-<id>` for each matched topic. Before, it looked for the cluster in the config file and answered 404, used the wrong group name and passed the group and topic in the wrong order.
- Creating a job or saving mappings with a regex that does not compile is rejected with HTTP 400 instead of failing at job start.
- If the target topic has fewer partitions than the source while a job runs, records from the missing partitions fall back to key hash placement and kaf-mirror logs one warning per partition. See "What a Copied Record Keeps" in the README.
- SASL from `security_config` works for every Kafka client. The consumer, the producer, the admin client, `POST /api/v1/clusters/test`, the topic list and detail views, the cluster status check, resume points and the inventory snapshot now build their options with one function, `kafka.ClientOptions`, that handles TLS, SASL PLAIN, SCRAM-SHA-256, SCRAM-SHA-512, GSSAPI and the `confluent`, `azure` and `redpanda` presets. Before, the admin client and the connection test only knew the provider presets, so a cluster with generic SASL failed the test and every admin call while its jobs could still connect (#36).
- `POST /api/v1/clusters/test` passes the full security config: the fields of `security_config` and of an optional `security` object. Before, it kept only `api_key` and `api_secret`. The test now gives up after 15 seconds (#36).
- SASL GSSAPI logs in with a keytab. New keys `security.kerberos.principal`, `security.kerberos.keytab_path` and `security.kerberos.krb5_conf_path` are file paths, never inline secrets, and `service_name` defaults to `kafka`. Before, GSSAPI had no Kerberos client and could not authenticate. The README removes the `jaas_config` example, which kaf-mirror never read. The Helm chart can mount the keytab from a Secret and `krb5.conf` from a ConfigMap (`kerberos.*`) and takes `config.sourceSecurity` and `config.targetSecurity`. See "Kafka Cluster Authentication" in the README (#36).
- Creating or updating a cluster answers HTTP 400 when `security_config` is not a JSON object, the SASL mechanism is unknown, a username or password is missing, or a GSSAPI cluster lacks the principal, keytab path or `krb5.conf` path (#36).
- A `confluent` cluster, or a `redpanda` cluster with a username, whose `protocol` is `SASL_SSL` no longer fails to create its consumer and producer with `cannot set both Dialer and DialTLSConfig`.
- The consumer and producer of an `azure` cluster use TLS and SASL PLAIN with the connection string. Before, only the admin client did, so jobs on Event Hubs could not connect.
- `POST /api/v1/jobs/{id}/mirror/resume-points` uses the full stored security config of both clusters. Before, it kept only `api_key` and `api_secret`.

### Security
- github.com/gofiber/fiber/v2 2.52.12 to 2.52.16 (GHSA-gcfq-8gqf-4876, GHSA-qjv7-627w-8qjv).
- github.com/gomarkdown/markdown to v0.0.0-20260724235538-61af6e1e6971 (GHSA-cv23-7vc5-jfh7, GHSA-77fj-vx54-gvh7).
- github.com/klauspost/compress 1.18.0 to 1.20.5 (GHSA-259r-337f-4rfw, GO-2026-6865).
- github.com/andybalholm/brotli 1.1.0 to 1.2.7 (GO-2026-6888) (#53).
- Built with Go 1.26.11, which fixes GO-2026-6859 in net/http. `toolchain` in go.mod, the Docker builder image `golang:1.26.11-alpine` and the release workflow use Go 1.26.11. CI and CodeQL read the version from go.mod. `govulncheck ./...` reports no reachable vulnerabilities (#53).

## [1.3.1] - 2026-10-22
### Security
- Built with Go 1.26.10, which fixes CVE-2026-111943 in crypto/x509. Release binaries, the Docker image and CI all use Go 1.26.10.

### Docs
- README: Kafka-to-S3 backup quickstart, use cases, support links and related projects.

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
