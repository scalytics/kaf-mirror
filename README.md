# kaf-mirror
[![CI](https://github.com/scalytics/kaf-mirror/actions/workflows/ci.yml/badge.svg)](https://github.com/scalytics/kaf-mirror/actions/workflows/ci.yml)
[![Release](https://github.com/scalytics/kaf-mirror/actions/workflows/release.yml/badge.svg)](https://github.com/scalytics/kaf-mirror/actions/workflows/release.yml)
[![CodeQL](https://github.com/scalytics/kaf-mirror/actions/workflows/codeql.yml/badge.svg)](https://github.com/scalytics/kaf-mirror/actions/workflows/codeql.yml)
[![GitHub Pages](https://img.shields.io/badge/docs-GitHub%20Pages-blue)](https://scalytics.github.io/kaf-mirror/)

**Copy your Kafka topics to any other Kafka cluster, or into S3 through KafScale, with a dashboard, audit trail and compliance reports. Apache 2.0.**

**kaf-mirror** is a high-performance, AI-enhanced Kafka replication tool designed for robust and intelligent data mirroring between Kafka clusters.

## Features

- **Core Replication Engine:** Built with `franz-go`, it handles consuming from a source Kafka cluster and producing to a target cluster with same-name mirroring by default, plus optional exact/regex mappings.
- **Enterprise Web Dashboard:** Professional tab-based interface with Operations, Health, Executive, and Compliance sections.
- **Hybrid Configuration:** The application loads its base configuration from a `default.yml` file and stores runtime changes (like replication jobs) in a SQLite database.
- **Comprehensive REST API:** A full-featured API built with Fiber provides endpoints for managing the application.
- **Job Management:** A `JobManager` controls the lifecycle of replication jobs (start, stop, pause).
- **AI-Powered Insights:**
    - **Anomaly Detection:** Automatically detects anomalies in replication metrics.
    - **Performance Tuning:** Provides recommendations for optimizing replication performance.
    - **Incident Analysis:** Offers detailed analysis of operational incidents.
- **Compliance Reporting:** Automated daily/weekly/monthly compliance reports with audit trails.
- **Role-Based Security:** Token-based authentication with admin/operator/monitoring/compliance roles.
- **Admin CLI:** A command-line tool for bootstrapping the application and for emergency maintenance.
- **Unified Management CLI:** A single, powerful CLI for all ongoing management tasks.
- **Dashboard updates:** The web UI polls the API every 30 seconds. `/ws` accepts authenticated WebSocket clients via an `Authorization: Bearer` header.
- **Offset commit:** Source offsets are committed only after the target produce is acknowledged. A failed produce is retried, and the offset is never committed past a record that was not copied. See [Resilience and Buffering](#resilience-and-buffering).
- **Secrets:** Cluster and config APIs return `***` for credentials. PUT with `***` or an empty secret leaves the stored value unchanged.
- **Replication halt (experimental):** Off until an admin enables it. Freezes all jobs if the source cluster is compromised so the replica does not keep copying the attack. See [docs/protection.md](docs/protection.md).

## Back up your Kafka to S3 in 10 minutes

Replication keeps a hot standby in sync. It also copies deletes, bad writes and
anything done with stolen producer credentials. For a backup, mirror into
[KafScale](https://github.com/KafScale/platform), a Kafka-compatible broker that
stores immutable segments in S3, and keep that copy on separate credentials.

Prerequisites: Go, Docker (for the local MinIO bucket), `make`, and a reachable
source Kafka cluster.

**1. Start KafScale with a local S3 bucket (MinIO)**

```bash
git clone https://github.com/KafScale/platform.git && cd platform
make demo
```

The broker listens on `127.0.0.1:39092`. Segments land in the MinIO bucket
`kafscale`.

**2. Build and start kaf-mirror**

In a second terminal, from this repo:

```bash
mkdir -p bin
go build -o bin/kaf-mirror ./cmd/kaf-mirror
go build -o bin/mirror-cli ./cmd/mirror-cli
KAFMIRROR_DISABLE_IDEMPOTENT_WRITES=true ./bin/kaf-mirror
```

KafScale does not implement `InitProducerId`, so idempotent writes must be off
when it is the target. The admin password
for `admin@localhost` is printed once to `logs/kaf-mirror.log`.

**3. Add the clusters and start a job**

```bash
./bin/mirror-cli login admin@localhost --password <password-from-log>
./bin/mirror-cli clusters add   # your source cluster
./bin/mirror-cli clusters add   # target: brokers 127.0.0.1:39092
./bin/mirror-cli jobs add       # pick source, target and topics
./bin/mirror-cli jobs start
./bin/mirror-cli jobs status full
```

`clusters add` and `jobs add` are interactive. Source offsets are committed only
after KafScale acknowledges the write, so a stopped job resumes where it left
off. For production, run KafScale from its Helm chart against a real S3 bucket
and consider [replication halt](docs/protection.md), which freezes jobs if the
source looks compromised.

### Use cases

- **Backup vs replication.** Use kaf-mirror between two Kafka clusters for a
  hot standby, and into KafScale on S3 for a backup that a bad delete does not
  reach. See [Kafka backup vs replication](https://www.scalytics.io/kafka-backup-vs-replication).
- **DORA and NIS2.** The audit trail and scheduled compliance reports help show
  how the copy is run and checked. See the
  [Kafka DORA and NIS2 checklist](https://www.scalytics.io/kafka-dora-nis2-checklist).
- **Migration and failover drills** between Kafka-protocol clusters, with
  same-name, exact or regex topic mapping.

More on the backup pattern: [KafShield, Kafka backup and disaster recovery on S3](https://www.scalytics.io/kafshield).

### Need it managed or supported?

kaf-mirror is part of [Lascaris](https://www.scalytics.io/lascaris). Scalytics
can run it for you in your own cloud, or support your team running it on-prem
or air-gapped.

More open source from the same team, including [kshark-core](https://github.com/scalytics/kshark-core)
for Kafka connectivity checks, is listed at [scalytics.io/open-source](https://www.scalytics.io/open-source).

## Getting Started

### 1. How to Run Locally (Docker)

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
To reset the initial admin password:
```bash
docker compose -p kaf-mirror exec kaf-mirror ./admin-cli reset-admin-password admin@localhost
```

### 1b. Run from prebuilt image (GHCR)

The listen address, the `/app/data` and `/app/logs` layout, and the bootstrap password file next to the database are in the image built from this repository. The published `v1.3.1` image does not include them. Until a newer tag is published, build and run locally. Keep `/app/data` (SQLite and the bootstrap password file) and `/app/logs` on named volumes so they survive a restart or a new container:
```bash
docker build -t kaf-mirror:local .
docker run -d --name kaf-mirror -p 8080:8080 \
  -v kaf-mirror-data:/app/data \
  -v kaf-mirror-logs:/app/logs \
  kaf-mirror:local
```
`make docker-run` does the same, in the foreground, with the local `kaf-mirror:latest` tag. After a newer image is published, replace `kaf-mirror:local` with `ghcr.io/scalytics/kaf-mirror:<tag>`. You can also use the `latest` tag once it includes this change:
```bash
docker pull ghcr.io/scalytics/kaf-mirror:latest
```
The UI/API will be at `http://localhost:8080`:
```bash
curl -s http://localhost:8080/health
```
Stop the container with `docker stop -t 45 kaf-mirror`, so it has time to stop its jobs. See "Graceful Shutdown".

The image sets `KAF_MIRROR_SERVER_HOST=0.0.0.0`, so the server listens on all container interfaces. The bundled `configs/default.yml` keeps `host: "localhost"` for runs outside Docker. To listen on another address, pass `-e KAF_MIRROR_SERVER_HOST=<address>`. The image runs in `development` mode. If you mount a config with `mode: "production"`, give it TLS or set `server.allow_insecure`.

Read the bootstrap admin password, then delete the file:
```bash
docker logs kaf-mirror | grep "Password File"
docker exec kaf-mirror sh -c 'cat /app/data/kaf-mirror-bootstrap-*'
docker exec kaf-mirror sh -c 'rm /app/data/kaf-mirror-bootstrap-*'
```

## Topic Mappings

Each job has a list of mappings with a `source_topic_pattern`, a `target_topic_pattern` and `enabled`. kaf-mirror decides with one rule whether a source pattern is an exact topic name or a regex. The same rule is used when a job starts, by topic discovery, when a record is routed, by the mirror state, verify, lag and topic health views, and when mappings are saved.

*   **Exact name:** a pattern without any of the characters `\ ^ $ * + ? ( ) [ ] { } |` is an exact topic name. A dot does not make a pattern a regex, so `pay.v1` copies only `pay.v1`, not `pay.v1.dlq`, `my-pay.v1` or `payXv1`.
*   **Regex:** a pattern with at least one of those characters is a Go regex. It must match the whole topic name, as if it were written `^(?:pattern)$`. `app-.*` matches `app-a` and `app-b`, but not `myapp-a` or `xapp-a`. A pattern you already anchored, such as `^app-.*$`, still matches the same names. In a regex, a dot matches any character, so write `ev\..*` to match only topics that start with `ev.`.
*   **Target:** for an exact name, an empty target means the same name on the target cluster. For a regex, an empty target means `$0`, the whole source topic name, so every matched topic keeps its name. A regex target can use `$0` and capture groups: `orders-(.*)` with target `backup-$1` copies `orders-eu` to `backup-eu`.
*   **Conflicts:** if an exact mapping and a regex match the same topic, the exact mapping wins. A job does not start, and topic discovery fails the job, when two regex mappings send one topic to different targets, when two source topics would go to the same target through a regex, or when a regex gives an empty target name.
*   **Invalid regex:** creating a job or saving mappings with a regex that does not compile, for example `app-(`, is rejected with HTTP 400.
*   **Target topics:** a missing target topic is created with the partition count of the source topic. A topic that someone else creates at the same time counts as created. After a create, kaf-mirror waits until the topic is visible in metadata, so a job does not fail to start when a multi-broker target has not yet refreshed. If the topic already exists with a different partition count, the job does not start.
*   **Discovery:** jobs with a regex mapping look for new matching source topics every `replication.topic_discovery_interval` from the config file (default `5m`). The value applies to every job and is read when a job starts. A failed run is retried at the next interval.

| Source pattern | Target pattern | Source topics | Copied as |
|---|---|---|---|
| `pay.v1` | empty | `pay.v1`, `pay.v1.dlq`, `payXv1` | `pay.v1` to `pay.v1` |
| `a.b` | `a.b.copy` | `a.b`, `a.b.c`, `my-a.b`, `aXb` | `a.b` to `a.b.copy` |
| `app-.*` | empty | `app-a`, `app-b`, `myapp-c` | `app-a` to `app-a`, `app-b` to `app-b` |
| `app-.*` | `mirror-$0` | `app-a`, `xapp-a` | `app-a` to `mirror-app-a` |
| `orders-(.*)` | `backup-$1` | `orders-eu`, `orders-us` | `orders-eu` to `backup-eu`, `orders-us` to `backup-us` |

A job with a regex mapping, created with the API (`$KM`, `$TOKEN` and the cluster names as in your setup):
```bash
curl -s -X POST "$KM/jobs/" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"apps","source_cluster_name":"src","target_cluster_name":"tgt","topic_mappings":[{"source_topic_pattern":"app-.*","target_topic_pattern":"","enabled":true}]}'
```

## What a Copied Record Keeps

kaf-mirror copies every record field a producer can set:

*   **Key and value:** copied as is, including null keys and tombstones (null values).
*   **Headers:** all headers, in their original order.
*   **Partition:** a record from source partition N is written to partition N of the target topic. See "Partition counts" below for a target with fewer partitions.
*   **Timestamp:** the source record timestamp becomes the target record timestamp. If the source topic uses `message.timestamp.type=LogAppendTime`, that is the time the source broker appended the record. A record without a source timestamp gets the time of the copy.

Offsets are not kept. The target broker assigns its own offsets.

Because the target keeps the source timestamp, time-based retention on the target counts from the source time. A record older than the target `retention.ms` is deleted from the target soon after the copy, the same way it would be on the source.

The target topic keeps the source timestamp only with `message.timestamp.type=CreateTime`, which is the Kafka default. With `LogAppendTime` the target broker replaces it with its own append time. The target broker also rejects records whose timestamp is outside its `message.timestamp.before.max.ms` and `message.timestamp.after.max.ms` limits, so check these before you mirror old data into a topic with tight limits.

**Partition counts.** When a job starts, every source topic and its target topic must have the same number of partitions, otherwise the job does not start. Two cases can still leave the target with fewer partitions while a job runs: partitions are added to the source topic, or topic discovery maps a new source topic to a target topic that already exists with fewer partitions. Records from a source partition the target does not have are then placed the way the Kafka default partitioner does it: records with a key go to the partition of the key hash (murmur2), and records without a key stay on one target partition until the producer starts a new batch. kaf-mirror logs one warning per source partition when this happens. To go back to one-to-one placement, add the same number of partitions to the target topic. The producer picks them up on its next metadata refresh, within about five minutes.

## Resilience and Buffering

The application is designed to be resilient to network issues and outages of the target cluster. It does not require a separate disk-based buffer for the following reasons:

*   **Transient Network Issues:** The internal Kafka producer has an in-memory buffer and an automatic retry mechanism. This handles short-term network interruptions without data loss or duplication, thanks to its idempotent producer configuration.
*   **Prolonged Target Cluster Outages:** If the target cluster is unavailable for an extended period, the application will experience backpressure. The consumer will stop reading messages from the source cluster, and its consumer offset will not advance. The source Kafka cluster itself acts as the durable, large-scale buffer, retaining the data until the target cluster is available again. When the connection is restored, the application will resume mirroring from the last committed offset, ensuring no data is lost.
*   **Rejected Records:** If the target returns an error for a record, for example `MESSAGE_TOO_LARGE` or an authorization error, kaf-mirror tries the record 5 times. This covers errors the Kafka client does not retry itself, such as `MESSAGE_TOO_LARGE`, an authorization error, or a target topic that does not exist. Errors the client retries itself, such as `NOT_ENOUGH_REPLICAS` or a broker that cannot be reached, are retried without a limit and the job waits as described under Prolonged Target Cluster Outages. Between the 5 tries, kaf-mirror waits 1s, 2s, 4s and 8s. Each try sends the failed record and the records after it in the same partition again, in order. kaf-mirror does not fetch more records while it retries. If the fifth attempt fails, the job stops with status `failed`, and `failed_reason` names the topic, partition, offset and error. The source offset stays at the failed record. Fix the cause, then start the job again. It reads the failed record again and continues from there. These values are fixed and have no config key.
*   **Batching:** kaf-mirror sends all records of a fetch to the target without waiting for each ack, so many records are in flight at once. The job `batch_size`, in KB, sets how much a fetch holds. The Kafka client writes the records of each partition in source order. When every record of the fetch is acked, kaf-mirror commits the highest offset of each partition in one commit call. If a record fails, the commit of its partition stops at the last acked record before it, so the offset never moves past a record that was not copied. Records after the failed one that the target had already written are written again when the failed record is retried, so the target can get them twice. A slow or failing partition holds back the next fetch, not the commit of the other partitions in the same fetch. There is no setting for the number of records in flight. The job fields `parallelism` and `preserve_partitions` were removed, because kaf-mirror never used them. Records always keep their source partition.
*   **Stopping During a Retry:** If you stop a job while a record waits for the target or between attempts, the job stops without committing that record. Records of the same fetch that the target already acked are committed before the job stops. The next start reads the rest again. If the target had already written a record when you stopped the job, it is copied a second time.
*   **Partition Rebalance:** If the source consumer group takes a partition away from kaf-mirror while a record of that partition is retried, kaf-mirror stops the retry and does not commit the record. Records of that partition that kaf-mirror fetched but has not sent yet are not sent, and records of that partition that were acked but not committed yet are not committed. The consumer that gets the partition reads it from the last committed offset. The same happens when kaf-mirror loses its group membership. If the target had already written that record, the target gets it twice.
*   **Failed Jobs:** A job stops with status `failed` when a record fails all 5 tries, when the producer fails more than 100 times in a row, when topic discovery finds a regex mapping it cannot apply, or when the consumer or metrics goroutine panics. Discovery cannot apply a mapping when it would send a new source topic to a target topic that another source topic already uses, or when it gives an empty target topic name. `failed_reason` says why, for example `consumer stopped: produce failed for topic orders partition 0 offset 2 after 5 attempts: <error>`, `topic discovery failed: target topic merged is mapped from multiple sources: ev.a and ev.b` or `consumer panic: <value>`. The job closes its Kafka clients and leaves the source consumer group, so its partitions are free. Fix the cause, then start or restart the job. It resumes from the last committed offset and `failed_reason` is cleared. If you stop or restart the job before kaf-mirror handles the failure, the stop or restart wins. The failure is only logged, a stopped job stays `paused` and a restarted job keeps running.
*   **Graceful Shutdown:** On `SIGTERM` or `SIGINT`, kaf-mirror stops the API server and all running jobs at the same time. Each job stops sending, commits the records the target already acked, closes its Kafka clients and leaves the source consumer group, so another member can take its partitions at once. The status of the jobs stays `active`, so they start again when kaf-mirror starts. Then the background work stops. All of this has to finish within `server.shutdown_timeout`, default `30s`. If it does not, kaf-mirror logs `Shutdown incomplete` and exits. A record that was not committed is read again on the next start. Give the process manager more time than `server.shutdown_timeout` before it kills kaf-mirror: `terminationGracePeriodSeconds` in Kubernetes, which the Helm chart sets to 45, `stop_grace_period` in Docker Compose, which `docker-compose.yml` sets to `45s`, or `docker stop -t 45`. The default of `docker stop` is 10 seconds.
*   **Topic Discovery During an Outage:** Jobs with regex mappings look for new source topics at every discovery run. If a run fails, for example because the source or target cluster does not answer within 30 seconds or a new target topic cannot be created, kaf-mirror logs a warning and tries again at the next run. The job stays `active` and keeps copying the topics it already has, the same way a job with exact topic names waits out an outage. When a run succeeds again, kaf-mirror logs that discovery works again and starts copying the new topics it finds. Stopping the job during a run or while it waits to retry a topic creation returns at once.

Helm SQLite lives at `/app/data/kaf-mirror.db` on the PVC. Optional `egress.allowed_hosts` restricts AI and metrics HTTP destinations. Passwords must be at least 12 characters.

**b) Data Retention Policy**

To prevent the internal SQLite database from growing indefinitely, the application enforces a 30-day retention policy by default for the following data:

*   **Replication Metrics:** Time-series data on throughput, lag, etc.
*   **Operational Events:** Audit logs of all state-modifying actions.

This data is automatically pruned every 24 hours. Admins can configure a shorter period via `database.retention_days` (1–30 days).

**Optional) Building binaries on your host**

```bash
make build
```
This produces `bin/kaf-mirror`, `bin/admin-cli`, and `bin/mirror-cli` for direct use outside Docker.

### 2. Admin and Management

All ongoing management is performed via the `mirror-cli`, which interacts with the secure, role-based API.

**a) Initial Setup & Configuration**

The server auto-bootstraps on first start: it seeds default roles/permissions and creates `admin@localhost` with a random password. The password is written to a `kaf-mirror-bootstrap-<random>` file next to the database, readable only by the kaf-mirror user, and the log prints the file path. Read it, deliver it securely, and delete the file. For repairs or reseeding when empty, use:
```bash
./admin-cli repair
```
To reset the initial admin password:
```bash
./admin-cli reset-admin-password admin@localhost
```

**b) `mirror-cli`: The Unified Management Tool**

**Login**

Before you can use the `mirror-cli`, you need to log in to the backend:

```bash
./mirror-cli login <username> --password <password>
```

This will authenticate with the API and store a session token securely on your local machine.

**Configuration Management**

- `./mirror-cli config get`: Get the current configuration from the server.
- `./mirror-cli config save [file]`: Save a configuration file to the server. (Requires admin role)
- `./mirror-cli config system`: Run the initial interactive configuration wizard. (Requires admin role)
- `./mirror-cli config edit`: Edit configuration sections interactively. (Requires admin role)

**Cluster Management**

- `./mirror-cli clusters list`: Lists all configured Kafka clusters.
- `./mirror-cli clusters add`: Adds a new Kafka cluster with interactive prompts.
- `./mirror-cli clusters remove <cluster-name>`: Marks a cluster as inactive. (Requires admin role)
- `./mirror-cli clusters purge`: Purges all archived clusters. (Requires admin role)

**Replication Job Management**

- `./mirror-cli jobs list`: List all replication jobs with status and performance metrics.
- `./mirror-cli jobs add`: Create a new replication job with interactive configuration:
  - Automatic topic discovery from source cluster
  - Interactive selection of existing source topics. Regex mappings such as `user\..*` are set with the API, see "Topic Mappings"
  - Replication settings (batch size, compression)
  - Optional custom target topic mapping (exact or regex with capture substitution)
- `./mirror-cli jobs start [job-id]`: Start paused or stopped replication jobs with interactive selection.
- `./mirror-cli jobs stop [job-id]`: Stop running replication jobs with interactive selection.
- `./mirror-cli jobs pause [job-id]`: Pause running replication jobs (resumable) with interactive selection.
- `./mirror-cli jobs delete [job-id]`: Delete replication jobs with confirmation (admin only).
- `./mirror-cli jobs status metrics [job-id]`: Show detailed job status and metrics.
- `./mirror-cli jobs status full [job-id]`: Show full mirror state (progress, gaps, resume points).

**User Management**

- `./mirror-cli users list`: Lists all users. Requires `users:list` permission.
- `./mirror-cli users add`: Adds a new user with interactive prompts and generated password. Requires `users:create` permission. Operators can only create users with the `monitoring` role.
- `./mirror-cli users set-role <username> <role>`: Sets a user's role. Requires `users:assign-roles` permission. Users cannot change their own role.
- `./mirror-cli users reset-password [username]`: Reset another user's password and generate a new secure password. **Admin only**. Cannot reset own password. The new password is written to a file (temp by default or `--password-file`).
- `./mirror-cli users delete <username>`: Delete a user account. Requires `users:delete` permission.
- `./mirror-cli whoami`: Displays information about the current user.
- `./mirror-cli change-password`: Changes the current user's password.
- `./mirror-cli reset-token`: Resets the current user's API token.

**Monitoring**

- `./mirror-cli dashboard`: Show a live monitoring dashboard. Users with `admin` or `operator` roles will see additional information.

**TLS Certificate Management**

The system includes comprehensive TLS certificate management for secure communications:

- `./mirror-cli tls generate-certs`: Generate self-signed CA, server, and client certificates
- `./mirror-cli tls import-cert`: Import existing TLS certificates and configure server
- `./mirror-cli tls configure-client`: Configure CLI for HTTPS communication
- `./mirror-cli tls verify`: Test TLS connection and certificate validation

TLS features include:
- Automatic HTTPS server startup when certificates are configured
- Support for both self-signed and CA-signed certificates
- Client certificate authentication for CLI-to-server communication
- Certificate validation and compatibility checking
- Insecure mode support for development with self-signed certificates

**AI Configuration Management**

The system supports multiple AI providers for operational intelligence:

- `./mirror-cli config edit ai provider`: Configure AI provider (OpenAI, Claude, Gemini, Grok, Custom)
- `./mirror-cli config edit ai model`: Set AI model for selected provider
- Provider-specific configuration with API endpoints and credentials

Supported AI Providers:
- **OpenAI**: GPT-4, GPT-4-turbo, GPT-3.5-turbo models
- **Claude**: Anthropic Claude-3 models (Opus, Sonnet, Haiku)
- **Gemini**: Google Gemini Pro and Vision models
- **Grok**: xAI Grok beta models
- **Custom**: Support for custom API endpoints with configurable models

AI Features:
- Automatic provider endpoint configuration
- Secure API key management
- Model selection with provider-specific options
- Custom provider support for enterprise deployments (OpenAI-compatible proxies should use `custom`)
- API secret support for providers requiring dual authentication

**Compliance Reporting**

The system includes comprehensive compliance reporting capabilities:

- **Web Dashboard**: Access compliance reports through the web interface at `http://localhost:8080` or `https://localhost:8080` (with TLS)
- **Report Generation**: Generate daily, weekly, or monthly compliance reports in CSV format
- **Audit Trails**: All reports include detailed audit trails and security metrics
- **Role-Based Access**: Only users with `compliance` role can generate and view reports
- **Token Validation**: Dashboard includes automatic token validation and logout for expired sessions

Compliance features include:
- System access audit logs
- Data processing statistics
- Security event tracking
- Configuration change history
- AI usage analytics
- Performance metrics analysis

Automated scheduling is controlled by `compliance.schedule` (daily/weekly/monthly) and runs at `run_hour` local time; weekly runs on Monday and monthly runs on the first day of the month.

**c) `admin-cli`: Recovery**

- `./admin-cli repair`: Reseed default roles/permissions; if no users exist, create `admin@localhost` with a random password.
- `./admin-cli reset-admin-password <username>`: Reset the password for the initial admin user (the one marked `is_initial`).
- `./admin-cli users list`: List users directly from the database.
- `./admin-cli users add <username> --password <password>`: Adds a new user directly to the database.

**d) Backup and Data Management**

The `admin-cli` provides comprehensive backup, restore, and import capabilities for disaster recovery and system migration.

**Backup Operations**

Create backups of your kaf-mirror system data:

- `./admin-cli backup database <output-path>`: Backup the SQLite database containing all jobs, users, clusters, and operational data. Automatically appends timestamp if extension not provided.
- `./admin-cli backup config <output-path>`: Backup the configuration file. Use `--config-path` flag to specify custom config location.
- `./admin-cli backup full <output-directory>`: Create complete system backup including database and configuration in a timestamped directory.

Examples:
```bash
# Backup database with auto-generated filename
./admin-cli backup database my-backup
# Creates: my-backup-backup-20240813-150405.db

# Backup configuration
./admin-cli backup config config-backup --config-path configs/production.yml

# Full system backup
./admin-cli backup full /backup/location
# Creates: /backup/location/kaf-mirror-backup-20240813-150405/
```

**Restore Operations**

Restore from previously created backups:

- `./admin-cli restore database <backup-path>`: Restore database from backup file. **Warning: Overwrites current database.**
- `./admin-cli restore config <backup-path>`: Restore configuration from backup. Use `--config-path` flag to specify target location.

Examples:
```bash
# Restore database (requires confirmation)
./admin-cli restore database my-backup-backup-20240813-150405.db

# Restore configuration to custom location
./admin-cli restore config config-backup.yml --config-path configs/default.yml
```

**Import Operations**

Import data from external kaf-mirror instances or other sources:

- `./admin-cli import database <source-path>`: Import database from another kaf-mirror instance. **Replaces current database.**
- `./admin-cli import config <source-path>`: Import configuration from external source.
- `./admin-cli import full <source-directory>`: Import complete system from external backup directory.

Examples:
```bash
# Import database from another system
./admin-cli import database /path/to/external/kaf-mirror.db

# Import configuration
./admin-cli import config /path/to/external/config.yml

# Import full system backup
./admin-cli import full /path/to/kaf-mirror-backup-20240813-150405/
```

**Backup Best Practices**

- **Regular Backups**: Schedule daily database backups using cron jobs
- **Configuration Versioning**: Backup configuration before making changes
- **Migration Support**: Use full backups when migrating between environments
- **Disaster Recovery**: Store backups in separate location from production system
- **Testing**: Regularly test restore procedures in non-production environment

**Migration Workflow**

To migrate kaf-mirror between systems:

1. **Source System**: Create full backup
   ```bash
   ./admin-cli backup full /tmp/migration-backup
   ```

2. **Target System**: Import full backup
   ```bash
   ./admin-cli import full /tmp/migration-backup/kaf-mirror-backup-20240813-150405
   ```

3. **Verification**: Verify all clusters, jobs, and users migrated correctly
   ```bash
   ./mirror-cli login <username>
   ./mirror-cli clusters list
   ./mirror-cli jobs list
   ```

### 3. Run as a Service (Linux)

To install and run `kaf-mirror` as a `systemd` service on Linux, you can use the provided installation script:

```bash
./scripts/install.sh
```

This script will:
1.  Create a dedicated user and group for the service.
2.  Create the necessary directories.
3.  Copy the binaries and configuration to the correct locations.
4.  Install and start the `systemd` service.

### 4. Run in Kubernetes

The manifests in `k8s/` have not been updated with the Docker and Helm fixes in this change. A default `kubectl apply -f k8s/` still sets `mode: production` without TLS or `allow_insecure`, so the pod does not start, and the Deployment has no `Recreate` strategy. Use the Helm chart in section 5.

To deploy with the raw manifests after you have updated them, first build the Docker image:

```bash
docker build -t kaf-mirror:latest .
```

Once the image is built, you can apply the Kubernetes configuration files:

```bash
kubectl apply -f k8s/
```

This will create a deployment, a service, a persistent volume claim, a configmap, and a secret in your Kubernetes cluster.

**Configuration:**

*   **`k8s/configmap.yaml`**: You will need to edit this file to provide the correct Kafka broker addresses.
*   **`k8s/pvc.yaml`**: This file defines the persistent storage for the application's internal SQLite database. **Important:** This storage is for metadata (users, job configurations, audit logs, metrics) and is NOT used to buffer the data being mirrored. The size required does not depend on your Kafka topic throughput. The default of 1Gi is a generous starting point for most use cases. In a production environment, ensure this PVC is backed by a high-performance `StorageClass` (e.g., SSDs). You can monitor its usage and expand the volume later if needed.
*   **`k8s/secrets.yaml`**: This file provides a template for managing secrets. You will need to add your base64-encoded secrets (e.g., TLS keys, API tokens) to this file.
*   **`k8s/deployment.yaml`**: This file defines the deployment of the application. It is configured to use the `ConfigMap` for configuration, the `PersistentVolumeClaim` for data storage, and the `Secret` for sensitive information.
*   **`k8s/service.yaml`**: This file exposes the application via a `LoadBalancer` service.

### 5. Run with Helm

The Helm chart is in `deploy/helm/kaf-mirror`. A default install starts and answers `/health`. The chart's default image tag is `v1.3.1`, which does not include the password file location or the image listen-address change. Until a newer tag is published, build the image from this repository, make it available to the cluster, and point the chart at it:

```bash
docker build -t kaf-mirror:local .
helm install km deploy/helm/kaf-mirror \
  --set image.repository=kaf-mirror \
  --set image.tag=local \
  --set image.pullPolicy=IfNotPresent \
  --wait
kubectl port-forward svc/km-kaf-mirror 8088:8088
```

*   **TLS:** by default the server runs without TLS (`config.server.allowInsecure: true`). For production, create a TLS Secret, set `config.server.tls.existingSecret` to its name and `config.server.allowInsecure` to `false`. The Secret is mounted at `/app/tls`, the probes use HTTPS, and the ServiceMonitor scrapes HTTPS.
*   **Tokens:** `secretEnv` sets `KAF_MIRROR_AI_TOKEN`, `KAF_MIRROR_AI_API_SECRET` or `KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN` from a Secret key. Do not pass secrets with `--set`.
*   **Upgrades:** the Deployment uses the `Recreate` strategy, because SQLite on a ReadWriteOnce volume must not be opened by two pods. Keep one replica.
*   **Shutdown:** `terminationGracePeriodSeconds` (default 45) must stay longer than `config.server.shutdownTimeoutSeconds` (default 30), which sets `server.shutdown_timeout`. The render fails otherwise. See "Graceful Shutdown" under Resilience and Buffering.
*   **Bootstrap password:** it is written to `/app/data/kaf-mirror-bootstrap-<random>` on the PVC. Read it with `kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /app/data/kaf-mirror-bootstrap-*'`, then delete the file.

All values are listed in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

## Security

### Kafka Cluster Authentication

The consumer, the producer, the admin client, `POST /api/v1/clusters/test`, the topic list and detail views, the cluster status check and the inventory snapshot all build their Kafka client options with one function, `kafka.ClientOptions`. A cluster that connects in the test also connects when a job runs.

*   **TLS:** on when `security.protocol` is `SSL` or `SASL_SSL`, and always for the `confluent` and `azure` providers. The client checks the broker certificate against the system trust store and needs TLS 1.2 or newer.
*   **Generic SASL:** used when `security.enabled` is `true` and `security.sasl_mechanism` is set. `PLAIN`, `SCRAM-SHA-256` and `SCRAM-SHA-512` need `username` and `password`. `GSSAPI` needs the Kerberos settings below. Any other mechanism is an error.
*   **Provider presets:** `confluent` uses SASL PLAIN with `api_key` and `api_secret`. `azure` uses SASL PLAIN with the user `$ConnectionString` and `connection_string`. `redpanda` with `username` and `password` and no `sasl_mechanism` uses TLS and SCRAM-SHA-256. With an explicit `sasl_mechanism`, a `redpanda` cluster uses the generic settings.
*   **Validation:** creating or updating a cluster and `POST /api/v1/clusters/test` answer HTTP 400 when `security_config` is not a JSON object, a mechanism is unknown, a password is missing or a GSSAPI path is missing.
*   **Logs:** kaf-mirror logs the provider, whether TLS is on and the mechanism name. It never logs a password, API secret or connection string.

A cluster with SCRAM-SHA-512 in the config file:
```yaml
clusters:
  source:
    provider: "plain"
    brokers: "broker1:9093,broker2:9093"
    security:
      enabled: true
      protocol: "SASL_SSL"
      sasl_mechanism: "SCRAM-SHA-512"
      username: "kaf-mirror"
      password: ""
```
Keep `password: ""` in the file and set the value from the environment, for example from a Kubernetes Secret with `secretEnv` and the variable `KAF_MIRROR_CLUSTERS_SOURCE_SECURITY_PASSWORD`. kaf-mirror only reads an environment override for a key that is in the config file.

Test a SASL cluster before you save it. The body takes the same fields as `POST /api/v1/clusters`, plus an optional `security` object. Write the body to a file that only you can read, so the password is not on the command line:
```bash
umask 077
"${EDITOR:-vi}" scram-test.json
curl -s -X POST "$KM/clusters/test" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data @scram-test.json
```
with this content in `scram-test.json`:
```json
{"provider":"plain","brokers":"broker1:9093","security":{"enabled":true,"protocol":"SASL_SSL","sasl_mechanism":"SCRAM-SHA-512","username":"kaf-mirror","password":"<password>"}}
```

### Kerberos (GSSAPI)

kaf-mirror logs in with a keytab through franz-go and gokrb5. It needs three file paths and no inline secret:

| Key | Description |
|---|---|
| `security.kerberos.principal` | Client principal, for example `kaf-mirror@EXAMPLE.COM`. Without `@REALM`, `default_realm` from `krb5.conf` is used. |
| `security.kerberos.keytab_path` | Path to the keytab file inside the container. |
| `security.kerberos.krb5_conf_path` | Path to `krb5.conf` inside the container. |
| `security.kerberos.service_name` | Kerberos service name of the brokers. Default `kafka`. |

The keytab and `krb5.conf` are read when a client is created. A missing or unreadable file fails the client with the path in the error.

```yaml
clusters:
  source:
    provider: "plain"
    brokers: "broker1.example.com:9093"
    security:
      enabled: true
      protocol: "SASL_SSL"
      sasl_mechanism: "GSSAPI"
      kerberos:
        service_name: "kafka"
        principal: "kaf-mirror@EXAMPLE.COM"
        keytab_path: "/app/kerberos/keytab/kafka.keytab"
        krb5_conf_path: "/app/kerberos/krb5/krb5.conf"
```

The same settings for a cluster created with the API:
```bash
curl -s -X POST "$KM/clusters" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"src","provider":"plain","brokers":"broker1.example.com:9093","security_config":"{\"enabled\":true,\"protocol\":\"SASL_SSL\",\"sasl_mechanism\":\"GSSAPI\",\"kerberos\":{\"service_name\":\"kafka\",\"principal\":\"kaf-mirror@EXAMPLE.COM\",\"keytab_path\":\"/app/kerberos/keytab/kafka.keytab\",\"krb5_conf_path\":\"/app/kerberos/krb5/krb5.conf\"}}"}'
```

In Docker, mount the keytab and `krb5.conf` read-only. The container runs as UID 100 and GID 101, so give that group read access to the keytab:
```bash
chgrp 101 kafka.keytab && chmod 0440 kafka.keytab
docker run -d --name kaf-mirror -p 8080:8080 \
  -v kaf-mirror-data:/app/data \
  -v kaf-mirror-logs:/app/logs \
  -v "$PWD/kafka.keytab:/app/kerberos/keytab/kafka.keytab:ro" \
  -v "$PWD/krb5.conf:/app/kerberos/krb5/krb5.conf:ro" \
  kaf-mirror:local
```

In Kubernetes with the Helm chart, keep the keytab in a Secret and `krb5.conf` in a ConfigMap, and set `kerberos.keytabSecret` and `kerberos.krb5ConfigMap`. The chart mounts them at `/app/kerberos/keytab` and `/app/kerberos/krb5`. See "Kerberos" in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

## API Documentation

The full API documentation is available via Swagger at `http://localhost:8080/swagger/index.html`.

## Testing

To run the unit tests, use the following command:

```bash
go test ./...
```

## Architecture Context

kaf-mirror addresses one of the hardest Kafka operational challenges: **cross-cluster replication with disaster recovery, observability, and governed failover**. This is section 3.4 of my Kafka architecture guide — where most production deployments eventually fail.

**Full architecture guide:** [Apache Kafka® Architecture Leadership](https://www.novatechflow.com/p/kafka-architecture-leadership.html)

### The Problem

From the guide:

> "Kafka replication across regions requires: replication topology, latency budgets, conflict resolution, failover procedures."

Most teams discover these requirements during an incident, not before. MirrorMaker works but lacks:

- Unified visibility across replication jobs
- Recovery tooling when things break
- Anomaly detection before lag becomes critical
- Audit trails for compliance

### What kaf-mirror Provides
```
Source Cluster                       Target Cluster
      │                                    ▲
      │ consume (franz-go)                 │ produce (idempotent)
      ▼                                    │
┌──────────────────────────────────────────┴───────┐
│                    kaf-mirror                    │
├──────────────────────────────────────────────────┤
│  Topic mapping (default mirror + regex patterns) │
│  Job lifecycle (start / stop / pause)            │
│  Consumer lag as architectural signal            │
│  AI anomaly detection + tuning recommendations   │
│  Real-time dashboard + WebSocket updates         │
│  Compliance reporting + audit trails             │
│  Role-based security (admin/operator/monitor)    │
└──────────────────────────────────────────────────┘
```

### Resilience Model

The guide emphasizes treating lag as an architectural signal, not a metric to hide. kaf-mirror implements this:

> "If the target cluster is unavailable, the consumer stops reading and its offset does not advance. The source Kafka cluster itself acts as the durable buffer. When restored, replication resumes from the last committed offset."

No separate disk buffer. Kafka's retention *is* the buffer. This simplifies operations and supports at-least-once delivery with an idempotent producer (duplicates reduced, not transactional exactly-once across clusters).

### Key Capabilities

| Capability | Addresses |
|------------|-----------|
| franz-go engine | High-perf, idempotent producer (section 2.2) |
| Topic governance | Mapping rules, naming conventions (section 2.1) |
| Lag monitoring | Backpressure as signal (section 3.2) |
| Multi-cluster auth | TLS, SASL PLAIN, SCRAM and Kerberos (see "Kafka Cluster Authentication") |
| Audit trails | Compliance and ownership (section 1.5) |
| AI insights | Anomaly detection before incidents |

### Where It Fits
```
Primary DC                    DR Site / Analytics Region
    │                                │
    ▼                                ▼
┌────────┐   kaf-mirror    ┌────────┐
│ Kafka  │ ──────────────► │ Kafka  │ → Flink → Iceberg
│ Cluster│                 │ Cluster│
└────────┘                 └────────┘
```

---

## License

Apache License 2.0. See `LICENSE` for details.
