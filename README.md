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
- **Hybrid Configuration:** The application loads its base configuration from a `default.yml` file and stores runtime changes (like replication jobs) in a SQLite database. Every config key and environment variable is listed in [docs/configuration.md](docs/configuration.md).
- **Comprehensive REST API:** A full-featured API built with Fiber provides endpoints for managing the application.
- **Job Management:** A `JobManager` controls the lifecycle of replication jobs (start, stop, pause).
- **AI-Powered Insights (opt-in):** Off until `ai.provider` is set. Without a provider kaf-mirror makes no AI requests. See [docs/configuration.md](docs/configuration.md#ai-analysis).
    - **Anomaly Detection:** Automatically detects anomalies in replication metrics.
    - **Performance Tuning:** Provides recommendations for optimizing replication performance.
    - **Incident Analysis:** Offers detailed analysis of operational incidents.
- **Compliance Reporting:** Automated daily/weekly/monthly compliance reports with audit trails.
- **Role-Based Security:** Token-based authentication with admin/operator/monitoring/compliance roles.
- **Admin CLI:** A command-line tool for bootstrapping the application and for emergency maintenance.
- **Unified Management CLI:** A single, powerful CLI for all ongoing management tasks.
- **Dashboard updates:** The web UI polls the API every 30 seconds. `/ws` accepts authenticated WebSocket clients via an `Authorization: Bearer` header. The web UI reads the permissions of the user from `GET /auth/me` and hides buttons the user cannot use. It fits phone screens down to 390px.
- **Empty lists:** List endpoints answer `[]` when there is nothing to list, never `null`.
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
when it is the target. On the first start kaf-mirror creates `admin@localhost`
with a random password and writes it to `data/kaf-mirror-bootstrap-<random>`,
next to the database. The path is printed on stdout as `Password File`. In a
third terminal, read the password, then delete the file:

```bash
cat data/kaf-mirror-bootstrap-*
rm data/kaf-mirror-bootstrap-*
```

**3. Add the clusters and start a job**

```bash
./bin/mirror-cli login admin@localhost   # asks for the password
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
To reset the initial admin password. `admin-cli` writes the new password to `/tmp/kaf-mirror-admin-reset-<random>` in the container and prints that path:
```bash
docker compose -p kaf-mirror exec kaf-mirror ./admin-cli reset-admin-password admin@localhost
docker compose -p kaf-mirror exec kaf-mirror sh -c 'cat /tmp/kaf-mirror-admin-reset-*'
docker compose -p kaf-mirror exec kaf-mirror sh -c 'rm /tmp/kaf-mirror-admin-reset-*'
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

## Topic Mappings

Each job has a list of mappings with a `source_topic_pattern`, a `target_topic_pattern` and `enabled`. kaf-mirror decides with one rule whether a source pattern is an exact topic name or a regex. The same rule is used when a job starts, by topic discovery, when a record is routed, by the mirror state, verify, lag and topic health views, and when mappings are saved.

*   **Exact name:** a pattern without any of the characters `\ ^ $ * + ? ( ) [ ] { } |` is an exact topic name. A dot does not make a pattern a regex, so `pay.v1` copies only `pay.v1`, not `pay.v1.dlq`, `my-pay.v1` or `payXv1`.
*   **Regex:** a pattern with at least one of those characters is a Go regex. It must match the whole topic name, as if it were written `^(?:pattern)$`. `app-.*` matches `app-a` and `app-b`, but not `myapp-a` or `xapp-a`. A pattern you already anchored, such as `^app-.*$`, still matches the same names. In a regex, a dot matches any character, so write `ev\..*` to match only topics that start with `ev.`.
*   **Target:** for an exact name, an empty target means the same name on the target cluster. For a regex, an empty target means `$0`, the whole source topic name, so every matched topic keeps its name. A regex target can use `$0` and capture groups: `orders-(.*)` with target `backup-$1` copies `orders-eu` to `backup-eu`.
*   **Conflicts:** if an exact mapping and a regex match the same topic, the exact mapping wins. A job does not start, and topic discovery fails the job, when two regex mappings send one topic to different targets, when two source topics would go to the same target through a regex, or when a regex gives an empty target name.
*   **Invalid regex:** creating a job or saving mappings with a regex that does not compile, for example `app-(`, is rejected with HTTP 400.
*   **Target topics:** a missing target topic is created with the partition count of the source topic. A topic that someone else creates at the same time counts as created. After a create, kaf-mirror waits until the topic is visible in metadata, so a job does not fail to start when a multi-broker target has not yet refreshed. A job start then reads the target metadata up to 5 times, waiting 1s, 2s, 3s and 4s between the reads, because each read can go to a broker that has not seen the new topic yet. If the topic is still missing after the fifth read, the job does not start and `failed_reason` says `target topic <name> does not exist after creation attempt`. If the topic already exists with a different partition count, the job does not start.
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
  -d '{"name":"apps","source_cluster_name":"src","target_cluster_name":"tgt","batch_size":1000,"topic_mappings":[{"source_topic_pattern":"app-.*","target_topic_pattern":"","enabled":true}]}'
```

`batch_size` is in KB. A job created without `batch_size`, or with `0`, gets the default of 1000. A value below 0 or above 102400 (100 MB, the largest request the Kafka client sends) is rejected with HTTP 400. An update without `batch_size` keeps the stored value. A job that was stored with `batch_size` 0 by an older version starts with 1000, and its next update stores 1000.

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

## Job Metrics

Every running job reports its counters every 10 seconds. Each report holds the counts since this run of the job started. kaf-mirror stores only the growth since the last report of the same run, so the totals of a job do not grow after a job restart or a kaf-mirror restart when no records come in.

*   `messages_replicated` and `bytes_transferred` (key plus value bytes) count each source record once, when the target first acks it. A record that is written again during a retry is not counted again.
*   `messages_consumed` and `bytes_consumed` count each record when its source offset is committed.
*   `error_count` counts failed produce attempts, so a record that fails twice counts 2, plus source fetch errors, one for each partition in each fetch that returns an error, and one for each check that finds a source topic missing.
*   `tombstone_count` counts committed records with an empty value.
*   `current_lag` is the lag of the job consumer group at the last report, computed the same way as `GET /api/v1/jobs/{id}/lag` and summed over all mapped source topics. It keeps growing while the target is down. If the source offsets cannot be read within 5 seconds, the report uses the lag from the last fetch instead.

`GET /api/v1/jobs/{id}/metrics/current` returns the totals since the job was created and `GET /api/v1/jobs/{id}/metrics/history` returns the stored reports, with `tombstone_count_delta` for each. `GET /api/v1/jobs/{id}/lag` reads the lag of the job consumer group `kaf-mirror-job-<id>` from the source cluster for each mapped topic: for every partition, the end offset minus the committed offset. A partition without a commit, or with a commit below the log start (for example after `DeleteRecords`), counts from its log start offset, and a partition never counts below 0. It also works for a stopped job. With `$KM`, `$TOKEN` and the job ID in `$JOB` as in your setup:

```bash
curl -s "$KM/jobs/$JOB/metrics/current" -H "Authorization: Bearer $TOKEN" | jq '{messages_replicated, bytes_transferred, error_count, tombstone_count, current_lag}'
curl -s "$KM/jobs/$JOB/lag" -H "Authorization: Bearer $TOKEN" | jq .
```

kaf-mirror does not rotate its log file. Use `logrotate` or the log tooling of your platform, see `logging.file` in [docs/configuration.md](docs/configuration.md). The size of copied records is logged only at `DEBUG`. At `INFO`, a job logs one line per source topic when that topic has a record larger than 2 KB, as a compression hint.

When new produce or fetch errors come in, the job logs `Error spike detected` once. It logs it again only after a report with no new errors, followed by new errors.

`GET /api/v1/jobs/{id}/metrics/current` also returns the incident flags `source_stalled`, `target_stalled`, `critical_lag`, `high_error_rate` and `error_spike`. They come from the last report of the running job, so they match the `Source stalled`, `Critical lag detected` and `Error spike detected` log lines and the Prometheus gauges. A job that is not running, or has not sent a report since it started, returns them as `false`. They are not stored in SQLite, so `GET /api/v1/jobs/{id}/metrics/history` does not have them:
```bash
curl -s "$KM/jobs/$JOB/metrics/current" -H "Authorization: Bearer $TOKEN" | jq '{source_stalled, target_stalled, critical_lag, high_error_rate, error_spike}'
```

`source_stalled` is set when the consumer group lag is above 0 and neither `messages_replicated` nor `messages_consumed` grows for about 70 seconds (seven 10-second reports). An idle topic with lag 0 does not stall. After a report with progress, the window starts again. `target_stalled` stays false: after batching, a record is counted as consumed only when the target has acked it and the source offset is committed, so a stuck target looks like no progress with lag and is reported as `source_stalled`. The lag here is the consumer group lag, so it grows while the target is down even though the job does not fetch new records. The Prometheus gauges `kaf_mirror_incident_source_stalled` and `kaf_mirror_incident_target_stalled` still exist, and the target gauge stays 0.

At job start kaf-mirror reads the topic configs of both clusters and logs the `compression.type` of each source and target topic, and warns when they differ. The inventory snapshot stores the same configs. If the configs cannot be read, for example because the broker does not support `DescribeConfigs`, kaf-mirror logs a warning and reports the compression as `NONE`.

## Resilience and Buffering

The application is designed to be resilient to network issues and outages of the target cluster. It does not require a separate disk-based buffer for the following reasons:

*   **Transient Network Issues:** The internal Kafka producer has an in-memory buffer and an automatic retry mechanism. This handles short-term network interruptions without data loss. The idempotent producer keeps these client retries from writing a record twice. When idempotent writes are off, with `KAFMIRROR_DISABLE_IDEMPOTENT_WRITES=true` or `clusters.<name>.disable_idempotent_writes` as needed for KafScale, a retried write can be stored twice on the target.
*   **Prolonged Target Cluster Outages:** If the target cluster is unavailable for an extended period, the application will experience backpressure. The consumer will stop reading messages from the source cluster, and its consumer offset will not advance. The source Kafka cluster itself acts as the durable, large-scale buffer, retaining the data until the target cluster is available again. When the connection is restored, the application will resume mirroring from the last committed offset, ensuring no data is lost.
*   **Rejected Records:** If the target returns an error for a record, for example `MESSAGE_TOO_LARGE` or an authorization error, kaf-mirror tries the record 5 times. This covers errors the Kafka client does not retry itself, such as `MESSAGE_TOO_LARGE`, an authorization error, or a target topic that does not exist. Errors the client retries itself, such as `NOT_ENOUGH_REPLICAS` or a broker that cannot be reached, are retried without a limit and the job waits as described under Prolonged Target Cluster Outages. Between the 5 tries, kaf-mirror waits 1s, 2s, 4s and 8s. Each try sends the failed record and the records after it in the same partition again, in order. kaf-mirror does not fetch more records while it retries. If the fifth attempt fails, the job stops with status `failed`, and `failed_reason` names the topic, partition, offset and error. The target can reject a whole produce batch of a partition because of one record, for example `MESSAGE_TOO_LARGE` when one record is larger than the target `max.message.bytes`. When more than one record of a partition fails in the same try, kaf-mirror sends those records again one at a time, waiting for each ack. The records before the bad one are copied and committed, and the 5 tries and `failed_reason` belong to the record the target really rejects. After the last of those records, the rest of the fetch is sent together again. A record that is too large is not failed at once: it gets the same 5 tries, so you can raise the target `max.message.bytes` while the job retries. A record larger than the job `batch_size` is rejected by the Kafka client with `MESSAGE_TOO_LARGE` in the same way. The source offset stays at the failed record. Fix the cause, then start the job again. It reads the failed record again and continues from there. These values are fixed and have no config key.
*   **Batching:** kaf-mirror sends all records of a fetch to the target without waiting for each ack, so many records are in flight at once. The job `batch_size`, in KB, sets how much a fetch holds. The Kafka client writes the records of each partition in source order. When every record of the fetch is acked, kaf-mirror commits the highest offset of each partition in one commit call. If a record fails, the commit of its partition stops at the last acked record before it, so the offset never moves past a record that was not copied. Records after the failed one that the target had already written are written again when the failed record is retried, so the target can get them twice. A slow or failing partition holds back the next fetch, not the commit of the other partitions in the same fetch. There is no setting for the number of records in flight. The job fields `parallelism` and `preserve_partitions` were removed, because kaf-mirror never used them. Records always keep their source partition.
*   **Stopping During a Retry:** If you stop a job while a record waits for the target or between attempts, the job stops without committing that record. Records of the same fetch that the target already acked are committed before the job stops. The next start reads the rest again. If the target had already written a record when you stopped the job, it is copied a second time.
*   **Partition Rebalance:** If the source consumer group takes a partition away from kaf-mirror while a record of that partition is retried, kaf-mirror stops the retry and does not commit the record. Records of that partition that kaf-mirror fetched but has not sent yet are not sent, and records of that partition that were acked but not committed yet are not committed. The consumer that gets the partition reads it from the last committed offset. The same happens when kaf-mirror loses its group membership. If the target had already written that record, the target gets it twice.
*   **Failed Jobs:** A job stops with status `failed` when a record fails all 5 tries, when the producer fails more than 100 times in a row, when topic discovery finds a regex mapping it cannot apply, or when the consumer or metrics goroutine panics. Discovery cannot apply a mapping when it would send a new source topic to a target topic that another source topic already uses, or when it gives an empty target topic name. `failed_reason` says why, for example `consumer stopped: produce failed for topic orders partition 0 offset 2 after 5 attempts: <error>`, `topic discovery failed: target topic merged is mapped from multiple sources: ev.a and ev.b` or `consumer panic: <value>`. The job closes its Kafka clients and leaves the source consumer group, so its partitions are free. Fix the cause, then start or restart the job. It resumes from the last committed offset and `failed_reason` is cleared. If you stop or restart the job before kaf-mirror handles the failure, the stop or restart wins. The failure is only logged, a stopped job stays `paused` and a restarted job keeps running.
*   **Graceful Shutdown:** On `SIGTERM` or `SIGINT`, kaf-mirror stops the API server and all running jobs at the same time. Each job stops sending, commits the records the target already acked, closes its Kafka clients and leaves the source consumer group, so another member can take its partitions at once. The status of the jobs stays `active`, so they start again when kaf-mirror starts. Then the background work stops. All of this has to finish within `server.shutdown_timeout`, default `30s`. If it does not, kaf-mirror logs `Shutdown incomplete` and exits. A record that was not committed is read again on the next start. Give the process manager more time than `server.shutdown_timeout` before it kills kaf-mirror: `terminationGracePeriodSeconds` in Kubernetes, which the Helm chart sets to 45, `stop_grace_period` in Docker Compose, which `docker-compose.yml` sets to `45s`, or `docker stop -t 45`. The default of `docker stop` is 10 seconds.
*   **Source Fetch Errors:** When a fetch from the source returns an error for a partition, kaf-mirror counts it in `error_count` and logs it. The same error of the same partition is logged at most once every 30 seconds, and the next line says how many were left out. When a fetch returns only errors and no records, the consumer waits before it fetches again: 100ms, doubling up to 5s, and back to no wait after a fetch without errors, also an empty one, or with records. Fetch errors are not counted by the protection auto-halt, which only looks at produce errors.
*   **Recreated Source Topic:** kaf-mirror notes the Kafka topic ID of each source topic from the source metadata when the job starts, when a topic is added, and from any successful fetch, including an empty one. It reads the topic metadata again every 10 seconds, and at once when a fetch answers `UNKNOWN_TOPIC_ID`. When the topic ID of a source topic has changed, the topic was deleted and created again. kaf-mirror then commits the earliest offset of each partition of the new topic for the job consumer group and takes the topic out of the consumer. If this member owns partitions of the topic after that, it waits for the group to revoke them, then adds the topic again, so the consumer loads the new metadata and copies the new topic from its earliest offsets. A member that owns none does not wait. The job stays `active`. The log has an `ERROR` line `source topic <topic> was recreated, topic ID <old> changed to <new>` with the IDs in the format of `kafka-topics.sh --describe`, and `source_topic_recreations` in `GET /api/v1/jobs/{id}/metrics/current` and the Prometheus gauge `kaf_mirror_source_topic_recreations` count the switches since the job started. Earliest is the start position because a job has no setting for it, and the consumer of a job always starts a partition without committed offsets at the earliest offset. kaf-mirror commits it itself because not every cluster removes the committed offsets of a deleted topic. When the source topic is deleted and not created again, the job stays `active`, every check adds 1 to `error_count`, `missing_source_topics` in `GET /api/v1/jobs/{id}/metrics/current` lists the topic, the Prometheus gauge `kaf_mirror_incident_source_topic_missing` is 1, and the log has `source topic <topic> does not exist` at most once every 30 seconds. When the topic is created again, kaf-mirror switches to it as above. A broker without topic IDs, before Kafka 2.8, or a topic ID of all zeros, keeps the old behavior: the consumer follows the topic by name and nothing is switched. A short `UNKNOWN_TOPIC_ID` while a topic is created, with the topic ID unchanged, only waits and fetches again.
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
This regenerates the docs and produces `bin/kaf-mirror`, `bin/admin-cli`, and `bin/mirror-cli` for direct use outside Docker. To build only the binaries:
```bash
make binaries
```
`make release-binaries VERSION=v1.4.0` builds the release assets into `dist/` with the same commands the release workflow uses. It needs `x86_64-linux-gnu-gcc` for the Linux builds. Each binary is built from its package, for example `go build ./cmd/admin-cli`, not from a single `main.go`.

### 2. Admin and Management

All ongoing management is performed via the `mirror-cli`, which interacts with the secure, role-based API.

**a) Initial Setup & Configuration**

The server auto-bootstraps on first start: it seeds default roles/permissions and creates `admin@localhost` with a random password. The password is written to a `kaf-mirror-bootstrap-<random>` file next to the database, readable only by the kaf-mirror user, and the file path is printed on stdout as `Password File`. Read it, deliver it securely, and delete the file. For repairs or reseeding when empty, use:
```bash
./admin-cli repair
```
To reset the initial admin password. The new password is written to a `kaf-mirror-admin-reset-<random>` file in the temp directory, and the path is printed:
```bash
./admin-cli reset-admin-password admin@localhost
```
`admin-cli repair` also writes a new bootstrap password to a `kaf-mirror-bootstrap-<random>` file in the temp directory when it creates `admin@localhost`.

**b) `mirror-cli`: The Unified Management Tool**

**Login**

Before you can use the `mirror-cli`, you need to log in to the backend:

```bash
./mirror-cli login <username>
```

It asks for the password without echo. Breaking: the `--password` flag is removed, so the password never ends up in the shell history or the process list. This will authenticate with the API and store a session token in `~/.kaf-mirror/token` with mode `0600`.

Without a terminal, for example in a script, `login` reads the password from the first line of stdin:

```bash
read -rs KM_PASSWORD
printf '%s\n' "$KM_PASSWORD" | ./mirror-cli login admin@localhost
unset KM_PASSWORD
```

Running `mirror-cli` without a command starts the interactive shell, which needs a terminal on stdin and stdout. Without one it prints an error and exits with status 1. `mirror-cli --help` and the other commands work without a terminal. `mirror-cli dashboard` also needs a terminal. The `jobs` commands that take an optional job id, such as `jobs start`, `jobs status full` and `jobs delete`, ask you to select a job when you leave the id out. Without a terminal, pass the job id. Otherwise they print `Operation cancelled: no terminal to select a job, pass the job id`, exit with status 1 and send no request.

A command that fails exits with status 1. That includes an unknown subcommand, such as `clusters delete`. `mirror-cli --help` and a group command with no extra arguments, such as `mirror-cli clusters`, exit with status 0. The group command prints its help.

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

**Cluster Status**

Each cluster has a stored status, shown in `GET /api/v1/clusters`, `mirror-cli clusters list`, the web UI clusters tab, topology and "Active Clusters" count, and the `mirror-cli` dashboard:

*   `unknown`: not checked yet. A new or restored cluster has this status until its first check.
*   `active`: the last connection check reached the cluster.
*   `unreachable`: the last connection check failed or timed out.
*   `inactive`: removed with `clusters remove`. It is not checked, and it is archived after 72 hours.
*   `archived`: inactive for more than 72 hours. `clusters purge` deletes archived clusters.

kaf-mirror checks every cluster that is not `inactive` or `archived` when it starts and then every 60 seconds. Each check connects to the cluster with the same client options a job uses, has a 10 second timeout, and at most 4 probes run at a time in the whole process. That cap covers the background loop, `GET /api/v1/clusters/{name}/status`, checks after create, update or restore, and the pre-flight check of a job restart. Concurrent checks of the same cluster share one probe only when the connection settings still match, including provider, brokers, SASL and TLS settings, API key and secret, and the connection string. The probe fingerprint is an HMAC-SHA256 of the connection settings, keyed with a random key that exists only in this process, and includes an HMAC of the contents of `ca_file`, `cert_file` and `key_file`. The secrets and the file contents are never stored or logged in plain text. If that key cannot be created, or a TLS file cannot be read, a check is never shared and the cluster is probed again. A slower check does not overwrite a later result or a result from a newer cluster configuration, and a cluster that is deleted and created again under the same name is treated as a new check. A cluster that comes back is `active` again after the next check. The interval and the timeout have no config key.

`GET /api/v1/clusters/{name}/status` runs one of those checks now. It answers HTTP 200 with `{"status":"active","message":"Cluster is reachable"}` when the cluster is reached. When the check fails or times out, it stores `unreachable` and answers HTTP 503. If the request is cancelled while it waits for a free probe slot, the stored status is left alone. Concurrent requests for the same cluster share the in-flight probe. Inactive and archived clusters are not probed; the call answers HTTP 200 with their stored status.

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
- `./mirror-cli jobs delete [job-id]`: Delete replication jobs with confirmation (admin only). The API call `DELETE /api/v1/jobs/{id}` answers HTTP 204 when the job is deleted, and HTTP 404 with `Job not found` when it does not exist.
- `./mirror-cli jobs status metrics [job-id]`: Show detailed job status and metrics.
- `./mirror-cli jobs status full [job-id]`: Show full mirror state (progress, gaps, resume points).

**User Management**

- `./mirror-cli users list`: Lists all users. Requires `users:list` permission.
- `./mirror-cli users add`: Adds a new user with interactive prompts and a generated password. The password is written to a new mode `0600` file in the temp directory, and only the file path is printed. Breaking: the unread `--password` flag is removed. Requires `users:create` permission, which only the `admin` role has.
- `./mirror-cli users set-role <username> <role>`: Sets a user's role. Requires `users:assign-roles` permission. Users cannot change their own role.
- `./mirror-cli users reset-password [username]`: Reset another user's password and generate a new secure password. **Admin only**. Cannot reset own password. The new password is written to a mode `0600` file. Without `--password-file`, it is a new file in the temp directory and its path is printed. With `--password-file`, the command does not print the path again, also not in an error.
- `./mirror-cli users delete <username>`: Delete a user account. Requires `users:delete` permission.
- `./mirror-cli whoami`: Displays the username and role of the current user. Breaking: it no longer prints the API token.
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

AI is off until `ai.provider` is set. Without a provider kaf-mirror sends nothing to any AI service, has no default endpoint, logs `AI analysis is off: ai.provider is not set` at start, and the AI endpoints that start an analysis answer HTTP 503. Restart kaf-mirror after you change the AI settings. See [docs/configuration.md](docs/configuration.md#ai-analysis).

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
- **Role-Based Access**: Users with the `admin` or `compliance` role can generate and view reports. The web UI hides the report buttons from other users
- **Audit Trail Content**: The CSV audit trail lists every successful `POST`, `PUT`, `PATCH` and `DELETE` API request in the period, with the time in UTC, the user, the method and path, and the details. These are the same events as the activity log
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
- `./admin-cli users add <username>`: Adds a new user directly to the database. It asks for the password without echo, or reads the first line of stdin when there is no terminal. Breaking: the `--password` flag is removed.

**d) Backup and Data Management**

The `admin-cli` provides backup, restore, and import commands for disaster recovery and system migration.

`admin-cli --help` needs no config file and no database. A database command works with `--db-path` and no config file when `CONFIG_PATH` is unset. If `CONFIG_PATH` is set and that file cannot be read, the command fails. Without `--db-path`, admin-cli reads `database.path` from the loaded config. A missing database path fails with `database path is required: pass --db-path or provide a config file`. `--db-path` is a flag of `admin-cli`. `--config-path` belongs to the backup, restore and import subcommand that uses it.

**Backup Operations**

`backup database` and `backup full` take a consistent copy of the SQLite database with `VACUUM INTO`. The copy includes writes that are still in the WAL file (`kaf-mirror.db-wal`), so you can run them while kaf-mirror runs. Every backup file is created with mode `0600`, because the database holds password hashes and API tokens and the config file can hold credentials, and `backup full` creates its directory with mode `0700`. Breaking: a backup never overwrites an existing file. If the path exists, the command fails and leaves the file alone.

- `./admin-cli backup database <output-path>`: Back up the SQLite database with all jobs, users, clusters and operational data. A path that does not end in `.db` gets `-backup-<timestamp>.db` added.
- `./admin-cli backup config <output-path>`: Back up the configuration file. A path that does not end in `.yml` gets `-config-backup-<timestamp>.yml` added. Use `--config-path` to name the config file.
- `./admin-cli backup full <output-directory>`: Write `database.db` and `config.yml` to a new `kaf-mirror-backup-<timestamp>` directory. The copied config is the base file that was loaded: the file named by `CONFIG_PATH`, or `default.yml` when `CONFIG_PATH` is unset. It is not the `prod.yml` overlay. Pass `--config-path` to copy a different file. When `--db-path` is set and no config was loaded, the default `--config-path` is `configs/default.yml`. If that file is missing, the database is still written and the command prints `Warning: configuration <path> was not included in the backup`.

Examples:
```bash
./admin-cli backup database my-backup
./admin-cli backup database kaf-mirror-copy.db
./admin-cli backup config config-backup --config-path configs/default.yml
./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db backup full /tmp/kaf-mirror-backups --config-path configs/default.yml
ls -l my-backup-backup-*.db config-backup-config-backup-*.yml /tmp/kaf-mirror-backups/kaf-mirror-backup-*/
```

The first command writes `my-backup-backup-<timestamp>.db`, for example `my-backup-backup-20240813-150405.db`. The third writes `config-backup-config-backup-<timestamp>.yml`.

For a backup of the database in the Helm chart, see "Back up the database" in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

**Restore Operations**

Stop kaf-mirror before you restore or import a database. Restore and import take the exclusive lock on `kaf-mirror.db.lock` themselves and hold it through the copy, sidecar cleanup and rename, so the server cannot start in the middle of a restore. If the server already holds the lock, the command fails. A leftover `-wal` file after a crash does not block a restore. The lock does not see a process that opens the database without it, for example the `sqlite3` CLI or a forgotten admin-cli, so stop kaf-mirror first. It checks that the backup is a readable SQLite database, writes a copy next to the database (following a symlink at the database path), removes the old `-wal` and `-shm` files and then moves the copy in place with mode `0600`. The server takes the same lock before it opens the database, creates the data directory with mode `0700` and the database, `-wal` and `-shm` files with mode `0600`, and refuses to start with a clear error while a restore holds the lock. Database restore and import are supported on Linux and macOS, not on Windows. Release binaries of `admin-cli` and `kaf-mirror` are Linux-only; on a Mac you can `go build` them.

- `./admin-cli restore database <backup-path>`: Restore the database from a backup file. **Overwrites the current database.**
- `./admin-cli restore config <backup-path>`: Restore the configuration from a backup. Use `--config-path` to name the target file.

Restore and import ask for confirmation. Breaking: without a terminal, for example in a script, they fail with a non-zero exit status unless you pass `--yes`. Before they printed "cancelled" and exited 0.

Examples:
```bash
./admin-cli restore database my-backup-backup-20240813-150405.db
./admin-cli restore config config-backup-config-backup-20240813-150405.yml --config-path configs/default.yml
./admin-cli restore database my-backup-backup-20240813-150405.db --yes
```

Replace the timestamps with the ones of your backup files.

**Import Operations**

Import data from another kaf-mirror instance. `import database` and `import full` replace the database the same way as `restore database`.

- `./admin-cli import database <source-path>`: Import the database of another kaf-mirror instance. **Replaces the current database.**
- `./admin-cli import config <source-path>`: Import a configuration file.
- `./admin-cli import full <source-directory>`: Import `database.db` and `config.yml` from a `backup full` directory. `config.yml` is written to `--config-path` (default `configs/default.yml`). When the backup has no `config.yml`, the command prints `No configuration found in source directory, skipping.` and still imports the database.

Examples:
```bash
./admin-cli import database /path/to/external/kaf-mirror.db
./admin-cli import config /path/to/external/config.yml
./admin-cli import full /tmp/kaf-mirror-backups/kaf-mirror-backup-20240813-150405
```

**Backup Best Practices**

- **Regular Backups**: Schedule daily database backups, for example with cron. Each run needs a new file name, so keep the default timestamped name or add the date.
- **Configuration Versioning**: Back up the configuration before you change it.
- **Migration Support**: Use full backups when you move between environments.
- **Disaster Recovery**: Store backups away from the production system, readable only by the operators.
- **Testing**: Test the restore regularly in a non-production environment.

**Migration Workflow**

To migrate kaf-mirror between systems:

1. **Source system**: create a full backup. The backup directory gets `database.db` and `config.yml`. `--db-path` and `--config-path` name those files, so the command works from any directory. With no `--config-path`, `config.yml` is the loaded base file (`CONFIG_PATH`, or `default.yml`) when a config was loaded, and `configs/default.yml` when it was not.
   ```bash
   ./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db backup full /tmp/migration-backup --config-path /etc/kaf-mirror/default.yml
   ```

2. **Target system**: stop kaf-mirror, copy the backup directory over and import it. `--config-path` is where `config.yml` from the backup is written.
   ```bash
   ./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db import full /tmp/migration-backup/kaf-mirror-backup-20240813-150405 --config-path /etc/kaf-mirror/default.yml --yes
   ```

3. **Verification**: start kaf-mirror and check that all clusters, jobs and users were migrated.
   ```bash
   ./mirror-cli login admin@localhost
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

Use the Helm chart in section 5. It is the supported way to run kaf-mirror in Kubernetes, and CI lints and renders it with `make helm-check`. The raw manifests that were in `k8s/` were removed: they used config keys kaf-mirror does not read, did not start with their defaults (`mode: production` without TLS), mounted a Secret at a path nothing reads, and had no `Recreate` strategy and no shutdown grace period.

### 5. Run with Helm

The Helm chart is in `deploy/helm/kaf-mirror`. A default install starts and answers `/health` on storage that honors `fsGroup`. The chart's default image tag is `v1.3.1`, which does not include the password file location or the image listen-address change. Until a newer tag is published, build the image from this repository, load it into the cluster, and point the chart at it. `image.pullPolicy` is `IfNotPresent`, so the node that runs the pod must already have the image.

```bash
docker build -t kaf-mirror:local .
```

On kind, when kind uses the same Docker daemon as the build:

```bash
kind load docker-image kaf-mirror:local
```

When Docker is rootless, or you need `sudo` to talk to it, kind cannot see that daemon. Save the image and load the archive:

```bash
docker save kaf-mirror:local -o kaf-mirror-local.tar
kind load image-archive kaf-mirror-local.tar
```

On minikube:

```bash
minikube image load kaf-mirror:local
```

```bash
cat > km-values.yaml <<'VALUES'
image:
  repository: kaf-mirror
  tag: local
  pullPolicy: IfNotPresent
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml --wait
kubectl port-forward svc/km-kaf-mirror 8088:8088
```

In another terminal, `curl -s http://localhost:8088/health`. The chart README has the rest of the install.

On kind or k3s with the default `local-path` storage, which does not honor `fsGroup`, kaf-mirror refuses to start on the volume. Turn on the init container that gives the volume to the kaf-mirror user:

```bash
cat > volume-permissions-values.yaml <<'VALUES'
volumePermissions:
  enabled: true
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f volume-permissions-values.yaml --wait
```

That init container runs as root and adds `CHOWN`, `FOWNER` and `DAC_OVERRIDE`. A namespace under the restricted Pod Security standard rejects it. Use storage that honors `fsGroup`, or a volume already owned by UID 100 and GID 101, and leave `volumePermissions.enabled` false. See "Storage permissions" in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

Pass `-f km-values.yaml` first on every later `helm upgrade`, followed by the values files of the other settings you use. Helm does not keep values from an earlier upgrade, so an upgrade without `km-values.yaml` goes back to the default image. An upgrade that replaces the pod drops `kubectl port-forward`. Start it again.

*   **TLS:** by default the server runs without TLS (`config.server.allowInsecure: true`). For production, create a TLS Secret, set `config.server.tls.existingSecret` to its name and `config.server.allowInsecure` to `false`. The Secret is mounted at `/app/tls`, the probes use HTTPS, and the ServiceMonitor scrapes HTTPS. Start the port-forward again after that upgrade and check `curl -sk https://localhost:8088/health`.
*   **Kafka TLS:** for a private CA or mTLS to the brokers, put the files in a Secret and set `kafkaTLS.existingSecret`. The Secret is mounted at `/app/kafka-tls`.
*   **Tokens:** `secretEnv` sets `KAF_MIRROR_AI_TOKEN`, `KAF_MIRROR_AI_API_SECRET` or `KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN` from a Secret key. Do not pass secrets with `--set`.
*   **Metrics:** the ServiceMonitor scrapes `/metrics`, which serves only the Go runtime metrics of the process. Job metrics are not on `/metrics`. They are stored in SQLite, returned by `GET /api/v1/jobs/{id}/metrics/current` and pushed to a Prometheus Pushgateway when `monitoring.platform` is `prometheus`.
*   **AI:** off by default. Set `config.ai.provider` to turn it on. See [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md#ai-analysis).
*   **Upgrades:** the Deployment uses the `Recreate` strategy, because SQLite on a ReadWriteOnce volume must not be opened by two pods. Keep one replica.
*   **Shutdown:** `terminationGracePeriodSeconds` (default 45) must stay longer than `config.server.shutdownTimeoutSeconds` (default 30), which sets `server.shutdown_timeout`. The render fails otherwise. See "Graceful Shutdown" under Resilience and Buffering.
*   **Bootstrap password:** it is written to `/app/data/kaf-mirror-bootstrap-<random>` on the PVC. Read it with `kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /app/data/kaf-mirror-bootstrap-*'`, then delete the file.

All values are listed in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

## Security

### Kafka Cluster Authentication

The consumer, the producer, the admin client, `POST /api/v1/clusters/test`, the topic list and detail views, the cluster status check and the inventory snapshot all build their Kafka client options with one function, `kafka.ClientOptions`. A cluster that connects in the test also connects when a job runs.

*   **TLS:** on when `security.protocol` is `SSL` or `SASL_SSL`, and always for the `confluent` and `azure` providers. The client checks the broker certificate against the system trust store, or against `ca_file` when it is set, and needs TLS 1.2 or newer. `cert_file` and `key_file` send a client certificate for mTLS. See "TLS with a private CA or mTLS" below.
*   **Generic SASL:** used when `security.enabled` is `true` and `security.sasl_mechanism` is set. `PLAIN`, `SCRAM-SHA-256` and `SCRAM-SHA-512` need `username` and `password`. `GSSAPI` needs the Kerberos settings below. Any other mechanism is an error.
*   **Provider presets:** `confluent` uses SASL PLAIN with `api_key` and `api_secret`. `azure` uses SASL PLAIN with the user `$ConnectionString` and `connection_string`. `redpanda` with `username` and `password` and no `sasl_mechanism` uses TLS and SCRAM-SHA-256. With an explicit `sasl_mechanism`, a `redpanda` cluster uses the generic settings.
*   **Validation:** creating or updating a cluster and `POST /api/v1/clusters/test` answer HTTP 400 when `security_config` is not a JSON object, a mechanism is unknown, a password is missing, a GSSAPI path is missing, only one of `cert_file` and `key_file` is set, `ca_file`, `cert_file` or `key_file` is set on a cluster without TLS, or a set `ca_file`, `cert_file` or `key_file` exists and is not a regular file or is larger than 1 MiB.
*   **Logs:** kaf-mirror logs the provider, whether TLS is on and the mechanism name. It never logs a password, API secret or connection string.

Jobs use the clusters stored in the database. Create them with `mirror-cli clusters add`, which asks for the password without echo, or with `POST /api/v1/clusters`. The security settings go in `security_config`, a JSON object written as a string. All fields are listed in [docs/configuration.md](docs/configuration.md#cluster-api-fields). The `clusters` section of the config file is only used by `GET /api/v1/topics/source` and `/target`, and for `disable_idempotent_writes`.

A cluster with SCRAM-SHA-512, created with the API. Write the body to a file that only you can read, so the password is not on the command line:
```bash
umask 077
"${EDITOR:-vi}" scram-cluster.json
curl -s -X POST "$KM/clusters" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data @scram-cluster.json
rm scram-cluster.json
```
with this content in `scram-cluster.json`:
```json
{"name":"src","provider":"plain","brokers":"broker1:9093,broker2:9093","security_config":"{\"enabled\":true,\"protocol\":\"SASL_SSL\",\"sasl_mechanism\":\"SCRAM-SHA-512\",\"username\":\"kaf-mirror\",\"password\":\"<password>\"}"}
```

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

### TLS with a private CA or mTLS

Set these keys in `security_config` of the cluster, or in `security` of a cluster in the config file. They are file paths on the kaf-mirror host or in the container. No certificate or key goes into the database.

| Key | Description |
|---|---|
| `ca_file` | PEM file with the CA certificates that sign the broker certificates. Without it, the system trust store is used. The file must be a regular file of at most 1 MiB. |
| `cert_file` | PEM client certificate for mTLS. Set it together with `key_file`. The file must be a regular file of at most 1 MiB. |
| `key_file` | PEM private key of `cert_file`. The file must be a regular file of at most 1 MiB. |

They need a TLS connection: `protocol` `SSL` or `SASL_SSL`, or the `confluent` or `azure` provider. Each file must be a regular file of at most 1 MiB. A symlink to a regular file is accepted, including the `..data` symlinks used by Kubernetes Secret mounts. Paths are not limited to one directory, so a config file cluster and the Helm mount at `/app/kafka-tls` both work. A FIFO, device, directory or larger file is rejected. The files are read when a client is created. A path that does not exist yet is accepted when the cluster is saved, and a missing file, a `ca_file` without a PEM certificate, or a key that does not match the certificate fails the client. If a TLS file cannot be read during a cluster probe, the check is not shared and the cluster is probed again. They work with SASL on `SASL_SSL`. kaf-mirror does not read `SSL_CERT_FILE` for this. Use `ca_file`.

A cluster with a private CA and a client certificate, created with the API:
```bash
curl -s -X POST "$KM/clusters" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"src","provider":"plain","brokers":"broker1.example.com:9093","security_config":"{\"protocol\":\"SSL\",\"ca_file\":\"/app/kafka-tls/ca.crt\",\"cert_file\":\"/app/kafka-tls/tls.crt\",\"key_file\":\"/app/kafka-tls/tls.key\"}"}'
```

In Docker, mount the files read-only. The container runs as UID 100 and GID 101, so give that group read access to the key. `chgrp` without `sudo` returns `Operation not permitted` unless your user is in group 101. If the group cannot be changed, `chmod 0444 tls.key` lets UID 100 read the file, and every user on the host can read the private key:
```bash
sudo chgrp 101 tls.key && sudo chmod 0440 tls.key
docker run -d --name kaf-mirror -p 8080:8080 \
  -v kaf-mirror-data:/app/data \
  -v kaf-mirror-logs:/app/logs \
  -v "$PWD/ca.crt:/app/kafka-tls/ca.crt:ro" \
  -v "$PWD/tls.crt:/app/kafka-tls/tls.crt:ro" \
  -v "$PWD/tls.key:/app/kafka-tls/tls.key:ro" \
  kaf-mirror:local
```

In Kubernetes with the Helm chart, keep the files in a Secret and set `kafkaTLS.existingSecret`. The chart mounts every key of the Secret at `/app/kafka-tls`. See "Kafka TLS" in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

### Kerberos (GSSAPI)

kaf-mirror logs in with a keytab through franz-go and gokrb5. It needs three file paths and no inline secret. Set them in `security_config` of the cluster:

| Key | Description |
|---|---|
| `kerberos.principal` | Client principal, for example `kaf-mirror@EXAMPLE.COM`. Without `@REALM`, `default_realm` from `krb5.conf` is used. |
| `kerberos.keytab_path` | Path to the keytab file inside the container. |
| `kerberos.krb5_conf_path` | Path to `krb5.conf` inside the container. |
| `kerberos.service_name` | Kerberos service name of the brokers. Default `kafka`. |

The keytab and `krb5.conf` are read when a client is created. A missing or unreadable file fails the client with the path in the error.

A cluster with Kerberos, created with the API:
```bash
curl -s -X POST "$KM/clusters" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"src","provider":"plain","brokers":"broker1.example.com:9093","security_config":"{\"enabled\":true,\"protocol\":\"SASL_SSL\",\"sasl_mechanism\":\"GSSAPI\",\"kerberos\":{\"service_name\":\"kafka\",\"principal\":\"kaf-mirror@EXAMPLE.COM\",\"keytab_path\":\"/app/kerberos/keytab/kafka.keytab\",\"krb5_conf_path\":\"/app/kerberos/krb5/krb5.conf\"}}"}'
```

In Docker, mount the keytab and `krb5.conf` read-only. The container runs as UID 100 and GID 101, so give that group read access to the keytab. `chgrp` without `sudo` returns `Operation not permitted` unless your user is in group 101. If the group cannot be changed, `chmod 0444 kafka.keytab` lets UID 100 read the file, and every user on the host can read the keytab:
```bash
sudo chgrp 101 kafka.keytab && sudo chmod 0440 kafka.keytab
docker run -d --name kaf-mirror -p 8080:8080 \
  -v kaf-mirror-data:/app/data \
  -v kaf-mirror-logs:/app/logs \
  -v "$PWD/kafka.keytab:/app/kerberos/keytab/kafka.keytab:ro" \
  -v "$PWD/krb5.conf:/app/kerberos/krb5/krb5.conf:ro" \
  kaf-mirror:local
```

In Kubernetes with the Helm chart, keep the keytab in a Secret and `krb5.conf` in a ConfigMap, and set `kerberos.keytabSecret` and `kerberos.krb5ConfigMap`. The chart mounts them at `/app/kerberos/keytab` and `/app/kerberos/krb5`. See "Kerberos" in [deploy/helm/kaf-mirror/README.md](deploy/helm/kaf-mirror/README.md).

## API Documentation

The full API documentation is available via Swagger at `http://localhost:8080/docu/swagger/`. The OpenAPI files are in `web/docu/swagger/`. Regenerate them with `make docs`.

## Testing

To run the unit tests, use the following command:

```bash
go test ./...
```

## Architecture Context

kaf-mirror addresses one of the hardest Kafka operational challenges: **cross-cluster replication with disaster recovery, observability, and governed failover**. This is section 3.4 of my Kafka architecture guide, where most production deployments eventually fail.

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

No separate disk buffer. Kafka's retention *is* the buffer. This simplifies operations and supports at-least-once delivery with an idempotent producer (duplicates reduced, not transactional exactly-once across clusters). With idempotent writes off, for example for KafScale, client retries can add duplicates.

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
