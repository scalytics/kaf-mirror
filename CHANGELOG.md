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
- Creating, updating or testing a GSSAPI cluster validates the Kerberos settings when `enabled` is omitted. Before, security was turned on only for an API key, a username or a password, so a GSSAPI config without `enabled: true` was saved and the connection test ran without SASL (#36).
- `POST /api/v1/clusters/test` uses the request context for its 15-second timeout, so a cancelled request does not keep the test running.
- When the Kafka client cannot be created after its SASL GSSAPI login was set up, for example because a broker address has no valid port, the consumer, producer, admin client and connection test now close the Kerberos client. Before, it was left open.
- A `confluent` cluster, or a `redpanda` cluster with a username, whose `protocol` is `SASL_SSL` no longer fails to create its consumer and producer with `cannot set both Dialer and DialTLSConfig`.
- The consumer and producer of an `azure` cluster use TLS and SASL PLAIN with the connection string. Before, only the admin client did, so jobs on Event Hubs could not connect.
- `POST /api/v1/jobs/{id}/mirror/resume-points` uses the full stored security config of both clusters. Before, it kept only `api_key` and `api_secret`.

- The inventory snapshot taken at job start is written to the database the server runs with. Before, it opened a second connection by path through a package global, so a job started from tests or embedded code created `data/kaf-mirror.db` in the working directory, and the snapshot failed with `FOREIGN KEY constraint failed` because that database had no such job. The consumer group part of the snapshot reuses the admin client of the start check instead of opening a new connection to the source cluster (#51).
- A job no longer waits for the target to acknowledge each record before it sends the next one and commits each record on its own. It sends all records of a fetch, waits for all acks and then commits the highest acked offset of each partition in one call. A failure commits its partition only up to the last acked record before it, retries the failed record and the records after it in order with the same 5 tries and waits, and fails the job when the tries run out. Stopping a job commits the records that were already acked. A partition that is revoked or lost is not committed. See "Batching" in the README (#50).
- The job fields `parallelism` and `preserve_partitions`, the config key `replication.parallelism`, the `mirror-cli config edit replication parallelism` command and the matching prompts were removed. kaf-mirror never used them: records are always written to their source partition, and the records of a fetch are now in flight together. Requests that still send the fields are accepted and the fields are ignored. The database columns stay, so no migration is needed (#50).
- On `SIGTERM` or `SIGINT`, kaf-mirror stops all running jobs, so they commit what the target acked, close their Kafka clients and leave the source consumer group, and then stops its background work. The jobs keep the status `active` and resume on the next start. Before, kaf-mirror only stopped the API server and exited, so the jobs never left the group and the next member waited for the session timeout. The API server and the jobs stop at the same time within the new `server.shutdown_timeout`, default `30s`, also `KAF_MIRROR_SERVER_SHUTDOWN_TIMEOUT`. A job that starts during shutdown is not started. Before, the API server shutdown had no time limit and could wait for open connections forever. The Helm chart sets `terminationGracePeriodSeconds` from a value (default 45) and `server.shutdown_timeout` from `config.server.shutdownTimeoutSeconds` (default 30), and the render fails if the grace period is not longer. `docker-compose.yml` sets `stop_grace_period: 45s`. See "Graceful Shutdown" in the README (#42).
- A dynamic throttling restart no longer waits while holding the job manager lock, so a SIGTERM shutdown is not delayed, and the job is not started again once shutdown has begun (#42).
- Job metrics no longer grow after a restart. Each run of a job reports its counters since it started, and kaf-mirror now stores the growth since the last report of the same run. Before, it compared the report with the stored lifetime total and, after a job or process restart, stored the whole in-memory count again on every 10-second report, so `messages_replicated` and the byte counts in `/metrics/current`, the dashboard, AI analysis and compliance reports grew without new records. See "Job Metrics" in the README (#43).
- `messages_replicated` and `bytes_transferred` count each source record once, when the target first acks it. Before, a record that was written again during a retry of an earlier record in the same partition was counted again (#43).
- The automatic halt checks compare the reports of one job only, and a job restart starts a new window. Before, the reports of all jobs went into one 60-second window, so the tombstone, error and payload checks compared the counters of different jobs and of an old and a new run (#43).
- `GET /api/v1/jobs/{id}/lag` returns the real lag of the job consumer group: for each partition the end offset minus the committed offset, a partition without a commit counted from its log start offset, and never below 0. It also works for a job that never committed. Before, it always returned 0 (#44).
- Topic configs are read when kaf-mirror lists a cluster, so the compression logged at job start, the compression mismatch warnings and the topic inventory show the real `compression.type`. Before, they always showed `NONE`. If the configs cannot be read, kaf-mirror logs a warning and reports `NONE` as before. The topic detail view uses the same code (#47).
- `tombstone_count` is stored and returned by `GET /api/v1/jobs/{id}/metrics/current`, and the job details in the web UI show it. `GET /api/v1/jobs/{id}/metrics/history` returns `tombstone_count_delta` for each report. The new column `aggregated_metrics.tombstone_count_delta` is added at startup when it is missing, with 0 for old rows. Before, it was always 0 (#48).
- `Error spike detected` is logged once when new produce errors come in, and again only after a report without new errors. `error_spike` in the metric sinks is true only for a report with new errors. Before, one error logged the line every 30 seconds for the life of the job (#49).
- `GET /api/v1/jobs/{id}/metrics/current` returns an error when the database cannot be read. Before, a failed read of the last report was ignored.
- The swagger docs list the response of `GET /api/v1/jobs/{id}/metrics/history` as `database.AggregatedMetric` and include `server.shutdownTimeout`.
- `GET /api/v1/jobs/{id}/lag` counts a partition from its log start when the committed offset is below the log start, for example after `DeleteRecords`. Before, it used the stale commit and overstated the lag (#44).
- `GET /api/v1/jobs/{id}/lag` uses the request context with a 15s timeout. Before, it used `context.Background()` with no deadline.
- `source_stalled` is true only when the job has lag and no new acks or commits for 60+ seconds. An idle job with lag 0 does not stall, and a stall can clear and fire again after traffic stops. Before, it fired for the life of the run once no record had been committed since start. `target_stalled` stays false, because consumed counts only rise after a target ack and a source commit, so a stuck target is reported as `source_stalled`. The `kaf_mirror_incident_target_stalled` gauge is kept and stays 0.
- The unused copies of the swagger spec at `web/docu/docs.go`, `web/docu/swagger.json` and `web/docu/swagger.yaml` were removed. The UI and the embedded spec use `web/docu/swagger/`.
- AI analysis is opt-in. Without `ai.provider`, kaf-mirror makes no AI requests and has no default endpoint, the AI analysis loops do not run, and it logs `AI analysis is off: ai.provider is not set` at start. An unknown provider, or `custom` without `ai.endpoint`, also leaves AI off. The AI endpoints that start an analysis answer HTTP 503 with `AI is not configured`. Before, a config without an `ai` section sent job names and metrics to `api.openai.com` every few minutes (#45).
- `ai.self_healing.automated_remediation` force-restarts a job with zero throughput even when no AI provider is set. `ai.self_healing.dynamic_throttling` also makes no AI call and still runs without one (#45).
- File and environment AI settings (`ai.provider`, `ai.token`, `ai.endpoint`) are used when they are set. A config saved in the database is used only when those file values are empty, so `KAF_MIRROR_AI_TOKEN` and a file provider still work after `PUT /api/v1/config` (#45).
- A self-hosted AI endpoint on a loopback or private address, such as Ollama or vLLM, is allowed. Link-local and cloud metadata addresses stay blocked (#45).
- The AI performance prompts no longer mention `parallelism`, which kaf-mirror does not have (#45).
- The Helm chart has `config.ai.provider`, `config.ai.endpoint` and `config.ai.model`, empty by default, so AI can be turned on in Kubernetes (#45).
- `clusters.<name>.disable_idempotent_writes` in the config file is applied to jobs whose stored target cluster has the same name. Before, jobs used only the clusters in the database, so the key was never applied and only `KAFMIRROR_DISABLE_IDEMPOTENT_WRITES` worked (#54).
- `admin-cli users add` asks for the password without echo when `--password` is left out, so it does not have to be on the command line (#54).
- The config keys `server.admin_email`, `logging.max_size`, `logging.max_backups` and `logging.max_age` were removed, because nothing read them. kaf-mirror does not rotate its log file. `replication.job_id`, `replication.batch_size`, `replication.compression`, `topic_mappings` and `protection.refuse_write_to_prod` are no longer read from the config file. Jobs always used their own stored settings, and refusing writes into a `prod` cluster is always on while protection is on. The keys were removed from `configs/default.yml`, `configs/docker.yml` and the Helm ConfigMap, together with the unused `topics` list in `configs/docker.yml`. An existing config file that still has those keys starts as before; the keys are ignored. This is not a breaking change to config loading (#54).
- The Helm chart can set `config.monitoring.enabled`, `config.monitoring.platform`, `config.monitoring.prometheus.pushGateway`, `config.protection.enabled`, `config.protection.autoHalt.enabled` and `config.egress.allowedHosts`. Those keys are written into the ConfigMap so `KAF_MIRROR_*` overrides work (#54).
- `admin-cli users add` reads the password from piped stdin when there is no TTY, so it works in scripts without putting the password on the command line (#54).
- The `k8s/` manifests were removed. They used config keys kaf-mirror does not read, did not start with their defaults and had no shutdown grace period. The Helm chart is the supported way to run kaf-mirror in Kubernetes (#54).
- `make build` no longer changes the committed `docs/cli-commands.md` and `web/docu/cli-commands.html`. The protection command help is the source of the docs text, and the Swagger link in the generated HTML no longer has a random query string. `make docs-check` regenerates the docs and fails on a difference, and CI runs it (#54).
### Docs
- New `docs/configuration.md` lists every config key and environment variable kaf-mirror reads, the `prod.yml` overlay, `CONFIG_PATH`, the `KAF_MIRROR_<KEY>` overrides, the date added to the log file name, the cluster API fields and how to turn on AI. A test checks the list against the config structs and the `os.Getenv` calls (#54).
- The README says where the bootstrap password file is and how to read it, uses the `mirror-cli login` and `admin-cli users add` password prompts instead of `--password`, shows how to read the password file of `admin-cli reset-admin-password` in Docker and Kubernetes, links Swagger at `/docu/swagger/`, creates SASL and Kerberos clusters with the API instead of the config file, explains that the config file clusters are only used by `/api/v1/topics/source` and `/target`, says that duplicates are possible when idempotent writes are off, points Kubernetes users to the Helm chart, and says that `/metrics` serves only the Go runtime metrics (#54).
- `docs/protection.md` links Swagger at `/docu/swagger/` and says that automatic halt also needs `protection.auto_halt.enabled` (#54).
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
