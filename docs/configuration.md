# Configuration reference

This page lists every config key and environment variable that kaf-mirror reads. `tests/internal/config/reference_doc_test.go` checks that this list matches the config structs in `internal/config/config.go` and the `os.Getenv` calls in the code.

## How the config is loaded

*   Without `CONFIG_PATH`, kaf-mirror reads `default.yml` from `/etc/kaf-mirror`, or else from the `configs` directory of the project. A `prod.yml` in the same directory is then merged on top of it, if it exists.
*   With `CONFIG_PATH`, kaf-mirror reads only that file. `prod.yml` is not merged.
*   Every key can be set from the environment as `KAF_MIRROR_<KEY>`, with the dots of the key written as underscores, for example `KAF_MIRROR_SERVER_HOST` for `server.host` or `KAF_MIRROR_AI_TOKEN` for `ai.token`. kaf-mirror only reads an override for a key that is in the config file, so keep the key in the file with an empty value, for example `token: ""`.
*   The config is read once at start. Restart kaf-mirror after a change.
*   Jobs, clusters, topic mappings and users are not in the config file. They are stored in the SQLite database and managed with `mirror-cli`, the web UI or the API.
*   The `ai` section is read from the config file and the `KAF_MIRROR_AI_*` environment variables when `ai.provider`, `ai.token` or `ai.endpoint` is set there. Otherwise, if a config was saved with `PUT /api/v1/config` or `mirror-cli config`, the saved `ai` section is used.
*   `PUT /api/v1/config` and `POST /api/v1/config/import` ignore JSON fields that are not config keys.

## Config keys

| Key | Default | Description |
|---|---|---|
| `server.host` | `localhost` | Listen address. The Docker image sets `KAF_MIRROR_SERVER_HOST=0.0.0.0`. |
| `server.port` | none | Listen port. Required. |
| `server.mode` | empty | `development` or `production`. In `production` kaf-mirror does not start without TLS unless `server.allow_insecure` is `true`. `test` turns off the background work and is only for tests. |
| `server.web_path` | `./web` | Directory with the web UI and the docs served at `/docu`. |
| `server.allow_insecure` | `false` | Allows a listener without TLS in `production`. |
| `server.shutdown_timeout` | `30s` | Time kaf-mirror has to stop its jobs and the API server on `SIGTERM` or `SIGINT`. See "Graceful Shutdown" in the README. |
| `server.tls.enabled` | `false` | Serves HTTPS. |
| `server.tls.cert_file` | empty | Path to the server certificate. |
| `server.tls.key_file` | empty | Path to the server key. |
| `server.cors.allowed_origins` | `["http://localhost:8080"]` | Allowed CORS origins. `configs/default.yml`, `configs/docker.yml` and the Helm chart ship `["http://localhost:8080"]`, and an empty list means the same. The web UI is served by kaf-mirror itself and does not need CORS. Add an origin only for a browser app on another origin. With `*`, any origin is allowed and credentials are not allowed. |
| `database.path` | none | Path to the SQLite database. The first start writes the bootstrap admin password file to the same directory. |
| `database.retention_days` | `30` | Days of metrics and events to keep, from 1 to 30. |
| `logging.level` | none | `DEBUG`, `INFO`, `WARN`, `ERROR` or `FATAL`. Required. |
| `logging.file` | none | Log file. If the name has no date, kaf-mirror adds the start date, so `logs/kaf-mirror.log` becomes `logs/kaf-mirror-2026-01-31.log`. kaf-mirror does not rotate the file. Use `logrotate` or the log tooling of your platform. The size of each copied record is logged only at `DEBUG`. |
| `logging.console` | `false` | Also writes the log to stdout. |
| `clusters.<name>.provider` | empty | `plain`, `confluent`, `redpanda` or `azure`. |
| `clusters.<name>.cluster_id` | empty | Cluster ID, used by the `confluent` provider. |
| `clusters.<name>.brokers` | empty | Comma-separated broker list. |
| `clusters.<name>.security.enabled` | `false` | Turns on SASL. |
| `clusters.<name>.security.protocol` | empty | `PLAINTEXT`, `SSL`, `SASL_PLAINTEXT` or `SASL_SSL`. |
| `clusters.<name>.security.sasl_mechanism` | empty | `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512` or `GSSAPI`. |
| `clusters.<name>.security.username` | empty | SASL user. |
| `clusters.<name>.security.password` | empty | SASL password. Set it from the environment, not in the file. |
| `clusters.<name>.security.api_key` | empty | API key of the `confluent` provider. |
| `clusters.<name>.security.api_secret` | empty | API secret of the `confluent` provider. Set it from the environment. |
| `clusters.<name>.security.connection_string` | empty | Connection string of the `azure` provider. Set it from the environment. |
| `clusters.<name>.security.ca_file` | empty | PEM file with the CA certificates that sign the broker certificates. Empty uses the system trust store. Needs a TLS connection: `protocol` `SSL` or `SASL_SSL`, or the `confluent` or `azure` provider. Must be a regular file of at most 1 MiB. A path that does not exist yet is accepted in the config and fails when a client is created. |
| `clusters.<name>.security.cert_file` | empty | PEM client certificate for mTLS. Set it together with `key_file`. Needs a TLS connection. Must be a regular file of at most 1 MiB. |
| `clusters.<name>.security.key_file` | empty | PEM private key of `cert_file`. Set it together with `cert_file`. Needs a TLS connection. Must be a regular file of at most 1 MiB. |
| `clusters.<name>.security.kerberos.service_name` | `kafka` | Kerberos service name of the brokers. |
| `clusters.<name>.security.kerberos.principal` | empty | Kerberos client principal. |
| `clusters.<name>.security.kerberos.keytab_path` | empty | Path to the keytab. |
| `clusters.<name>.security.kerberos.krb5_conf_path` | empty | Path to `krb5.conf`. |
| `clusters.<name>.disable_idempotent_writes` | `false` | Turns off idempotent writes for every job whose stored target cluster has the same name, for example a cluster named `kafscale`. |
| `replication.topic_discovery_interval` | `5m` | How often jobs with a regex mapping look for new source topics. `0` turns discovery off. |
| `ai.provider` | empty | `openai`, `claude`, `gemini`, `grok` or `custom`. Empty turns AI off. See "AI analysis" below. |
| `ai.endpoint` | provider default | API endpoint. Required for `custom`. |
| `ai.token` | empty | API key. Set it from the environment with `KAF_MIRROR_AI_TOKEN`. |
| `ai.api_secret` | empty | Second secret for providers that need one. Sent as `X-API-Secret`. Set it from the environment. |
| `ai.model` | empty | Model name. |
| `ai.features.anomaly_detection` | `false` | Stored and shown by `mirror-cli`. The server does not use it. |
| `ai.features.performance_optimization` | `false` | Stored and shown by `mirror-cli`. The server does not use it. |
| `ai.features.incident_analysis` | `false` | Stored and shown by `mirror-cli`. The server does not use it. |
| `ai.self_healing.automated_remediation` | `false` | Force-restarts a job that has had zero throughput for 10 minutes. This is a local rule and does not need an AI provider. |
| `ai.self_healing.dynamic_throttling` | `false` | Every 5 minutes, changes the `batch_size` of active jobs from their metrics and restarts them. This makes no AI call. Increases stay at or below 10000 KB. Decreases stay at or above 100 KB. Optimize does not raise a size above 5000 KB and does not lower a size that is already 5000 KB or more. |
| `monitoring.enabled` | `false` | Sends job metrics to `monitoring.platform`. |
| `monitoring.platform` | empty | `splunk`, `loki` or `prometheus`. |
| `monitoring.splunk.hec_endpoint` | empty | Splunk HTTP Event Collector URL. |
| `monitoring.splunk.hec_token` | empty | Splunk HEC token. Set it from the environment with `KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN`. |
| `monitoring.splunk.index` | empty | Splunk index. |
| `monitoring.loki.endpoint` | empty | Loki push URL. |
| `monitoring.prometheus.push_gateway` | empty | Prometheus Pushgateway URL. Job metrics are pushed there. `/metrics` serves only the Go runtime metrics. |
| `compliance.schedule.enabled` | `false` | Generates compliance reports on a schedule. |
| `compliance.schedule.run_hour` | `2` | Local hour, 0 to 23. |
| `compliance.schedule.daily` | `false` | Daily report. If the schedule is on and no period is set, this is turned on. |
| `compliance.schedule.weekly` | `false` | Weekly report, on Monday. |
| `compliance.schedule.monthly` | `false` | Monthly report, on the first day of the month. |
| `egress.allowed_hosts` | empty | If set, AI and monitoring requests may only go to these hosts. |
| `protection.enabled` | `false` | Turns on replication halt at start. See [protection.md](protection.md). |
| `protection.halt_file` | `data/HALT` | If this file exists, all jobs halt. |
| `protection.halt_env` | `KAF_MIRROR_HALT` | If this variable is `true`, `1` or `yes`, all jobs halt. |
| `protection.auto_halt.enabled` | `false` | Halts jobs on a burst of empty records or produce errors. |
| `protection.auto_halt.tombstone_ratio` | `0.5` | Share of empty records among the consumed records in 60 seconds that halts. |
| `protection.auto_halt.tombstone_min` | `100` | Minimum empty records in 60 seconds before the ratio counts. |
| `protection.auto_halt.auth_error_burst` | `50` | Produce or auth errors in 60 seconds that halt. |

While protection is on, kaf-mirror never starts a job that writes into a cluster with the role `prod`. This has no key.

The settings of a job, such as `batch_size` (in KB, default 1000, from 1 to 102400) and `compression`, are stored with the job and set with `mirror-cli jobs add` or the API. The config file has no keys for them. `mirror-cli config edit replication batch-size` does not write a config key; use the job API.

### Clusters in the config file

Jobs use the clusters stored in the database, not the `clusters` section of the config file. The config file clusters are used only for:

*   `GET /api/v1/topics/source` and `GET /api/v1/topics/target`, which list the topics of the clusters named `source` and `target`. The Helm values `config.sourceBrokers`, `config.targetBrokers`, `config.sourceSecurity` and `config.targetSecurity` fill these two clusters.
*   `disable_idempotent_writes`, which applies to jobs whose stored target cluster has the same name.

## Environment variables

| Variable | Description |
|---|---|
| `CONFIG_PATH` | Path of the config file. Turns off the `prod.yml` merge. |
| `KAF_MIRROR_<KEY>` | Overrides a config key that is in the config file, for example `KAF_MIRROR_SERVER_HOST`. |
| `KAFMIRROR_DISABLE_IDEMPOTENT_WRITES` | `true`, `1` or `yes` turns off idempotent writes for all jobs. Needed when the target does not support `InitProducerId`, for example KafScale. Without idempotent writes, a retried write can be stored twice. |
| `KAF_MIRROR_HALT` | Default name of the halt variable, see `protection.halt_env`. |
| `EDITOR` | Editor that `mirror-cli config edit <section>` opens. Default `vim`. |

## Cluster API fields

`POST /api/v1/clusters` and `PUT /api/v1/clusters/{name}` take these fields:

| Field | Description |
|---|---|
| `name` | Cluster name. Jobs refer to it. Required. It must not be empty or only spaces and must not contain `/`, because it is part of the URL of the cluster. `POST /api/v1/clusters` removes leading and trailing spaces, the same as job fields. |
| `provider` | `plain`, `confluent`, `redpanda` or `azure`. |
| `brokers` | Comma-separated broker list. Required, also for `PUT`. |
| `cluster_id` | Cluster ID, for `confluent`. |
| `api_key`, `api_secret` | Credentials of the `confluent` provider. |
| `connection_string` | Connection string of the `azure` provider. |
| `role` | `prod`, `dr` or `other`. Default `other`. See [protection.md](protection.md). |
| `security_config` | A JSON object written as a string, with the keys `enabled`, `protocol`, `sasl_mechanism`, `username`, `password`, `ca_file`, `cert_file`, `key_file` and `kerberos` (`service_name`, `principal`, `keytab_path`, `krb5_conf_path`). |

A missing `name` or `brokers` answers HTTP 400. A `name` that another cluster already has answers HTTP 409. `DELETE /api/v1/clusters/{name}` of an unknown cluster answers HTTP 404.

Earlier versions accepted a cluster with an empty name, which no URL can reach. `DELETE /api/v1/clusters/purge` removes such a cluster when no job uses it. Delete the jobs that use it first, with `DELETE /api/v1/jobs/{id}`.

An example `security_config` value:
```json
{"enabled":true,"protocol":"SASL_SSL","sasl_mechanism":"SCRAM-SHA-512","username":"kaf-mirror","password":"<password>"}
```

## Job API fields

`POST /api/v1/jobs` needs `name`, `source_cluster_name`, `target_cluster_name` and at least one entry in `topic_mappings`, otherwise it answers HTTP 400. A field that is only spaces counts as empty. A source or target cluster that does not exist answers HTTP 404 with `Source cluster not found` or `Target cluster not found`, also for `PUT /api/v1/jobs/{id}`. `PUT /api/v1/jobs/{id}` keeps the stored value of a field that is omitted, empty or only spaces. A `name` that another job already has answers HTTP 409. The response has the stored `created_at` and `updated_at`.

When a source topic is deleted and created again, kaf-mirror switches to the new Kafka topic ID and copies the new topic from its earliest offsets. It notes topic IDs from the source metadata when the job starts, when a topic is added, and from any successful fetch, including an empty one. It checks those IDs every 10 seconds and when a fetch answers `UNKNOWN_TOPIC_ID`. `GET /api/v1/jobs/{id}/metrics/current` reports `source_topic_recreations`, the number of switches since the job started, and `missing_source_topics`, the source topics that were deleted and do not exist yet. Both come from the last sample of the running job and are 0 and empty when the job is not running. The Prometheus gauges `kaf_mirror_source_topic_recreations` and `kaf_mirror_incident_source_topic_missing` report the same values.

## AI analysis

AI analysis is off until `ai.provider` is set. Without a provider kaf-mirror makes no AI requests and has no default endpoint. At start it logs `AI analysis is off: ai.provider is not set`, and the AI endpoints that start an analysis (`POST /api/v1/ai/test`, `/ai/explain/{event}`, `/ai/incidents/{event_id}/analyze`, `/jobs/{id}/ai/analyze` and `/jobs/{id}/ai/historical-analysis`) answer HTTP 503 with `AI is not configured`. An unknown provider, or `custom` without `ai.endpoint`, also leaves AI off and is logged.

For a self-hosted model, such as vLLM or Ollama on your network, set `ai.provider` to `custom` and `ai.endpoint` to that API, for example `http://127.0.0.1:11434/v1` or `http://vllm.ml.svc.cluster.local:8000/v1`. Loopback and private addresses are allowed. Link-local and cloud metadata addresses stay blocked. If you set `egress.allowed_hosts`, include the AI host.

To turn it on, set the provider and the model in the config file and the token from the environment:
```yaml
ai:
  provider: "openai"
  model: "gpt-4o"
  token: ""
```
```bash
read -rs KAF_MIRROR_AI_TOKEN && export KAF_MIRROR_AI_TOKEN
./bin/kaf-mirror
```
