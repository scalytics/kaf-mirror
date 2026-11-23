# kaf-mirror Helm chart

The chart runs kaf-mirror as a single pod with a ConfigMap for its config, a PersistentVolumeClaim for SQLite and a Service.

## Install

The default image tag is `v1.3.1`. That published image does not include the password file location or the image listen-address change. Until a newer tag is published, build the image from this repository, make it available to the cluster, and point the chart at it:

```bash
docker build -t kaf-mirror:local .
helm install km deploy/helm/kaf-mirror \
  --set image.repository=kaf-mirror \
  --set image.tag=local \
  --set image.pullPolicy=IfNotPresent \
  --wait
kubectl port-forward svc/km-kaf-mirror 8088:8088
```

In another terminal:

```bash
curl -s http://localhost:8088/health
```

The default install starts without TLS: `config.server.mode` is `production` and `config.server.allowInsecure` is `true`. For production, give the server a certificate (see TLS below) and set `config.server.allowInsecure` to `false`, so the server refuses to start without TLS.

## Bootstrap admin password

On first start, kaf-mirror creates `admin@localhost` with a random password and writes it to `/app/data/kaf-mirror-bootstrap-<random>` on the PVC. kaf-mirror prints the file path on stdout, not the password, so `kubectl logs` shows it. Read it, deliver it securely, then delete the file:

```bash
kubectl logs deploy/km-kaf-mirror | grep "Password File"
kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /app/data/kaf-mirror-bootstrap-*'
kubectl exec deploy/km-kaf-mirror -- sh -c 'rm /app/data/kaf-mirror-bootstrap-*'
```

To set a new random password for `admin@localhost`, run `admin-cli` in the pod. It writes the password to `/tmp/kaf-mirror-admin-reset-<random>` in the container and prints that path:

```bash
kubectl exec deploy/km-kaf-mirror -- ./admin-cli reset-admin-password admin@localhost
kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /tmp/kaf-mirror-admin-reset-*'
kubectl exec deploy/km-kaf-mirror -- sh -c 'rm /tmp/kaf-mirror-admin-reset-*'
```

## Back up the database

The database is `/app/data/kaf-mirror.db` on the PVC. `admin-cli backup database` takes a consistent copy with `VACUUM INTO` while the pod runs, including writes still in the WAL file, and creates it with mode `0600`. Write the backup to the PVC, copy it out with a private umask, then delete it in the pod:

```bash
kubectl exec deploy/km-kaf-mirror -- ./admin-cli --db-path /app/data/kaf-mirror.db backup database /app/data/kaf-mirror-backup.db
(umask 077 && kubectl exec deploy/km-kaf-mirror -- cat /app/data/kaf-mirror-backup.db > kaf-mirror-backup.db)
kubectl exec deploy/km-kaf-mirror -- rm /app/data/kaf-mirror-backup.db
```

Breaking: the backup refuses to overwrite an existing file, so delete the old copy in the pod first or pick a new name. `admin-cli restore database` refuses to replace a database while the server holds `kaf-mirror.db.lock`, so it cannot run in the kaf-mirror pod while the server runs. A leftover `-wal` file is not enough to block a restore. To restore, scale the deployment to 0 and run `admin-cli restore database` with `--yes` in a separate pod that mounts the same PVC. The chart has no helper for that yet. See "Backup and Data Management" in the main README.

## TLS

Create a TLS Secret from your certificate and key files, then point the chart at it. The Secret is mounted read-only at `/app/tls`, the liveness and readiness probes switch to HTTPS, and the ServiceMonitor scrapes HTTPS (it skips certificate verification, as the kubelet probes do).

```bash
kubectl create secret tls km-tls --cert=server.crt --key=server.key
cat > tls-values.yaml <<'VALUES'
config:
  server:
    allowInsecure: false
    tls:
      existingSecret: km-tls
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f tls-values.yaml --wait
```

## Tokens from a Secret

`secretEnv` sets environment variables from Secret keys with `secretKeyRef`. kaf-mirror reads `KAF_MIRROR_<KEY>` for a config key, with dots replaced by underscores. The chart config contains `ai.token`, `ai.api_secret` and `monitoring.splunk.hec_token`, so these variables work:

| Variable | Config key |
|---|---|
| `KAF_MIRROR_AI_TOKEN` | `ai.token` |
| `KAF_MIRROR_AI_API_SECRET` | `ai.api_secret` |
| `KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN` | `monitoring.splunk.hec_token` |

```bash
umask 077
read -rs AI_TOKEN && printf '%s' "$AI_TOKEN" > ai-token.txt && unset AI_TOKEN
kubectl create secret generic km-secrets --from-file=ai-token=./ai-token.txt
rm ai-token.txt
cat > secret-values.yaml <<'VALUES'
config:
  ai:
    provider: openai
    model: gpt-4o
secretEnv:
  - name: KAF_MIRROR_AI_TOKEN
    secretName: km-secrets
    key: ai-token
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f secret-values.yaml --wait
```

## AI analysis

AI analysis is off by default. With an empty `config.ai.provider`, kaf-mirror makes no AI requests and the AI endpoints that start an analysis answer HTTP 503. To turn it on, set `config.ai.provider` and `config.ai.model`, and `config.ai.endpoint` for `custom`, and give the token with `secretEnv` as in the example above. See [docs/configuration.md](../../../docs/configuration.md#ai-analysis).

## Kerberos

Jobs use the clusters you create with `mirror-cli` or the API, not the `source` and `target` clusters of the config file. `config.sourceBrokers`, `config.sourceSecurity`, `config.targetBrokers` and `config.targetSecurity` are only used by `GET /api/v1/topics/source` and `GET /api/v1/topics/target`.

For a cluster with SASL GSSAPI, keep the keytab in a Secret and `krb5.conf` in a ConfigMap. The chart mounts the keytab read-only at `/app/kerberos/keytab/<kerberos.keytabKey>` with mode `0440`, readable by the pod group 101, and `krb5.conf` at `/app/kerberos/krb5/<kerberos.krb5ConfKey>`. The paths are only file paths in the config. No secret goes into the values or onto the command line.

```bash
kubectl create secret generic km-keytab --from-file=kafka.keytab=./kafka.keytab
kubectl create configmap km-krb5 --from-file=krb5.conf=./krb5.conf
cat > kerberos-values.yaml <<'VALUES'
kerberos:
  keytabSecret: km-keytab
  krb5ConfigMap: km-krb5
config:
  sourceBrokers: "broker1.example.com:9093"
  sourceSecurity:
    enabled: true
    protocol: SASL_SSL
    sasl_mechanism: GSSAPI
    kerberos:
      service_name: kafka
      principal: kaf-mirror@EXAMPLE.COM
      keytab_path: /app/kerberos/keytab/kafka.keytab
      krb5_conf_path: /app/kerberos/krb5/krb5.conf
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f kerberos-values.yaml --wait
```

`config.sourceSecurity` and `config.targetSecurity` are written as `security` of the `source` and `target` clusters in the config file. For clusters you create with the API, use the same paths in `security_config`. For SASL PLAIN or SCRAM, keep `password: ""` in `config.sourceSecurity` and set `KAF_MIRROR_CLUSTERS_SOURCE_SECURITY_PASSWORD` with `secretEnv`, so the password stays in a Secret.

## Upgrades and SQLite

The Deployment uses the `Recreate` strategy. SQLite on a ReadWriteOnce volume must not be opened by two pods, so an upgrade stops the old pod before the new one starts. Expect a short gap in replication during an upgrade. The chart runs exactly one pod, and rendering fails if `replicaCount` is set to anything other than 1.

## Shutdown

When Kubernetes stops the pod, kaf-mirror stops its jobs, commits what the target already acked, closes its Kafka clients and leaves the consumer group. The jobs stay `active` and resume when the new pod starts. kaf-mirror gives this `config.server.shutdownTimeoutSeconds` (default 30), set as `server.shutdown_timeout`. Kubernetes kills the pod after `terminationGracePeriodSeconds` (default 45). Keep the grace period longer than the shutdown timeout. The render fails if it is not.

```bash
cat > shutdown-values.yaml <<'VALUES'
terminationGracePeriodSeconds: 90
config:
  server:
    shutdownTimeoutSeconds: 60
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f shutdown-values.yaml --wait
```

## Values

| Key | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/scalytics/kaf-mirror` | Image repository. |
| `image.tag` | `v1.3.1` | Image tag. It follows `appVersion`. Until a newer tag is published, set this to an image built from this repository, as in the Install section. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `replicaCount` | `1` | Must be 1. SQLite is not shared, so any other value fails the render. |
| `terminationGracePeriodSeconds` | `45` | Seconds Kubernetes waits after `SIGTERM` before it kills the pod. Must be greater than `config.server.shutdownTimeoutSeconds`. |
| `service.type` | `ClusterIP` | Service type. |
| `service.port` | `8088` | Service port. It forwards to the container port `http`. |
| `config.server.host` | `0.0.0.0` | Listen address, set as `server.host` and as `KAF_MIRROR_SERVER_HOST`. |
| `config.server.port` | `8080` | Container listen port, also used by the probes. |
| `config.server.mode` | `production` | `server.mode`. |
| `config.server.allowInsecure` | `true` | `server.allow_insecure`. With `false` and no TLS, the server refuses to start in `production` mode. |
| `config.server.shutdownTimeoutSeconds` | `30` | `server.shutdown_timeout` in seconds: how long kaf-mirror waits for its jobs and the API server to stop on `SIGTERM`. |
| `config.server.tls.existingSecret` | `""` | Name of a Secret with the certificate and key. When set, TLS is on and the probes use HTTPS. |
| `config.server.tls.certKey` | `tls.crt` | Key of the certificate in the Secret. |
| `config.server.tls.keyKey` | `tls.key` | Key of the private key in the Secret. |
| `config.server.corsAllowedOrigins` | `["*"]` | `server.cors.allowed_origins`. |
| `config.logLevel` | `INFO` | `logging.level`. |
| `config.sourceBrokers` | `kafka-source:9092` | Brokers of the `source` cluster in the config file. Only used by `GET /api/v1/topics/source`. Jobs use clusters stored in the database. |
| `config.targetBrokers` | `kafka-target:9092` | Brokers of the `target` cluster in the config file. Only used by `GET /api/v1/topics/target`. |
| `config.sourceSecurity` | `enabled: false` | `security` block of the `source` cluster in the config file. Do not put passwords here; see Kerberos. |
| `config.targetSecurity` | `enabled: false` | `security` block of the `target` cluster in the config file. |
| `config.ai.provider` | `""` | `ai.provider`: `openai`, `claude`, `gemini`, `grok` or `custom`. Empty turns AI off. |
| `config.ai.endpoint` | `""` | `ai.endpoint`. Required for `custom`. |
| `config.ai.model` | `""` | `ai.model`. |
| `config.monitoring.enabled` | `false` | `monitoring.enabled`. |
| `config.monitoring.platform` | `""` | `monitoring.platform`: `splunk`, `loki` or `prometheus`. |
| `config.monitoring.prometheus.pushGateway` | `""` | `monitoring.prometheus.push_gateway`. Job metrics are pushed here when the platform is `prometheus`. |
| `config.protection.enabled` | `false` | `protection.enabled`. See [protection.md](../../../docs/protection.md). |
| `config.protection.autoHalt.enabled` | `false` | `protection.auto_halt.enabled`. |
| `config.egress.allowedHosts` | `[]` | `egress.allowed_hosts`. Empty allows any host except metadata addresses. |
| `kerberos.keytabSecret` | `""` | Secret with the Kerberos keytab. When set, it is mounted at `/app/kerberos/keytab`. |
| `kerberos.keytabKey` | `kafka.keytab` | Key of the keytab in the Secret, and its file name in the mount. |
| `kerberos.krb5ConfigMap` | `""` | ConfigMap with `krb5.conf`. When set, it is mounted at `/app/kerberos/krb5`. |
| `kerberos.krb5ConfKey` | `krb5.conf` | Key of `krb5.conf` in the ConfigMap, and its file name in the mount. |
| `secretEnv` | `[]` | List of `name`, `secretName` and `key`. Each entry sets an environment variable from a Secret key. |
| `persistence.enabled` | `true` | Keep `/app/data` on a PVC. With `false`, data is lost when the pod is replaced. |
| `persistence.storageClass` | `""` | StorageClass of the PVC. Empty uses the cluster default. |
| `persistence.size` | `2Gi` | PVC size. |
| `resources` | see `values.yaml` | Container resources. |
| `probes.liveness` | `initialDelaySeconds: 15`, `periodSeconds: 20` | Liveness probe timing. The probe checks `/health` on the `http` port. |
| `probes.readiness` | `initialDelaySeconds: 5`, `periodSeconds: 10` | Readiness probe timing. The probe checks `/health` on the `http` port. |
| `podSecurityContext` | UID 100, GID 101, `fsGroup` 101 | Pod security context. The IDs match the `kaf-mirror` user in the image. |
| `securityContext` | see `values.yaml` | Container security context. |
| `serviceAccount.create` | `true` | Create a ServiceAccount. |
| `serviceAccount.name` | `""` | ServiceAccount name. Empty uses the release full name. |
| `serviceAccount.annotations` | `{}` | ServiceAccount annotations. |
| `labels` | `{}` | Extra labels on every object. |
| `metrics.enabled` | `true` | Render a ServiceMonitor when the Prometheus Operator CRD is installed. When TLS is on, it scrapes HTTPS. `/metrics` serves only the Go runtime metrics of the process. Job metrics are not on `/metrics`. They are pushed to `config.monitoring.prometheus.pushGateway` when `config.monitoring.platform` is `prometheus`. |
| `metrics.path` | `/metrics` | Metrics path. |
| `metrics.serviceMonitor.labels` | `{}` | Extra ServiceMonitor labels. |
| `metrics.serviceMonitor.interval` | `30s` | Scrape interval. |
| `metrics.serviceMonitor.scrapeTimeout` | `10s` | Scrape timeout. |
| `nodeSelector` | `{}` | Pod node selector. |
| `tolerations` | `[]` | Pod tolerations. |
| `affinity` | `{}` | Pod affinity. |

`make helm-check` lints the chart and renders the default, TLS, Secret, ServiceMonitor and Kerberos cases. `go test ./tests/deploy/` fails when a key in `values.yaml` is not read by any template.
