# kaf-mirror Helm chart

The chart runs kaf-mirror as a single pod with a ConfigMap for its config, a PersistentVolumeClaim for SQLite and a Service.

## Install

The default image tag is `v2.0.0`. To run the current `main` branch instead, build the image from this repository, load it into the cluster, and point the chart at it. `image.pullPolicy` is `IfNotPresent`, so the node that runs the pod must already have the image.

```bash
docker build -t kaf-mirror:local .
```

On kind, when kind uses the same Docker daemon as the build:

```bash
kind load docker-image kaf-mirror:local
```

With Docker Engine 29 and its containerd image store, the default for new installs, `kind load docker-image` can print `ctr: content digest sha256:... not found` and a failure message. When it does, or when Docker is rootless or you need `sudo` to talk to it, so kind cannot see that daemon, save the image and load the archive:

```bash
docker save kaf-mirror:local -o kaf-mirror-local.tar
kind load image-archive kaf-mirror-local.tar
```

On minikube:

```bash
minikube image load kaf-mirror:local
```

The image settings go in `km-values.yaml`. Every `helm` command on this page passes `-f km-values.yaml` first, so an upgrade never drops them and the pod never goes back to the default image.

```bash
cat > km-values.yaml <<'VALUES'
image:
  repository: kaf-mirror
  tag: local
  pullPolicy: IfNotPresent
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml --wait
kubectl get deploy km-kaf-mirror -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
kubectl port-forward svc/km-kaf-mirror 8088:8088
```

The `kubectl get` line prints `kaf-mirror:local`.

In another terminal:

```bash
curl -s http://localhost:8088/health
```

On kind or k3s with the default `local-path` provisioner, kaf-mirror refuses to start on the volume, because the volume belongs to root and every user can write to it. Turn on the init container that gives it to the kaf-mirror user:

```bash
cat > volume-permissions-values.yaml <<'VALUES'
volumePermissions:
  enabled: true
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f volume-permissions-values.yaml --wait
```

See "Storage permissions". A namespace under the restricted Pod Security standard rejects this init container. Leave `volumePermissions.enabled` false there and use storage that honors `fsGroup`, or a volume already owned by UID 100 and GID 101.

A `helm upgrade` that replaces the pod drops `kubectl port-forward`. Start it again. The default install answers `curl -s http://localhost:8088/health`.

Each section below writes its own values file and adds it after `km-values.yaml`. Helm does not keep values from an earlier upgrade, so pass every values file you use on each upgrade, in the same order. The TLS section writes `tls-values.yaml` and the Tokens section writes `secret-values.yaml`. The upgrade that uses both is after those files exist.

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

## Storage permissions

The pod runs as UID 100 and GID 101 with `fsGroup` 101. kaf-mirror needs write access to `/app/data`, as the owner or through group 101. With storage that honors `fsGroup`, the volume root belongs to group 101 and the default chart works. Most CSI drivers for block storage do, for example the AWS EBS, GCE PD and Azure Disk drivers.

kaf-mirror refuses to start when `/app/data` belongs to another user and every user can write to it without the sticky bit, because any user could then replace the database files. The log names the directory, its mode and its owner, and says: use storage that honors fsGroup, or set volumePermissions.enabled: true in the Helm values, or chown the directory to the kaf-mirror user outside Helm. The `local-path` provisioner of kind and k3s does not honor `fsGroup`. It creates the directory as `root:root` with mode `0777`, so the default chart does not start on it. On those clusters, enable `volumePermissions`. A root init container then gives `/app/data` to UID 100 and GID 101 and sets mode `0770`. kaf-mirror owns the directory after that and sets it to mode `0700`:

```bash
cat > volume-permissions-values.yaml <<'VALUES'
volumePermissions:
  enabled: true
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f volume-permissions-values.yaml --wait
```

The init container uses the `busybox` image. It runs as root (`runAsUser` 0 and `runAsNonRoot: false`) and adds the `CHOWN`, `FOWNER` and `DAC_OVERRIDE` capabilities. A namespace under the restricted Pod Security standard rejects that pod. On those clusters, use storage that honors `fsGroup`, or a volume already owned by UID 100 and GID 101, and leave `volumePermissions.enabled` false. Storage that honors `fsGroup` does not need the init container.

A pre-created volume that belongs to UID 100 and GID 101 also works, without the init container. On kind, create the directory on the node, a StorageClass and a PersistentVolume for it, then install the chart with the matching storage class:

```bash
docker exec kind-control-plane sh -c 'mkdir -p /var/lib/kaf-mirror && chown 100:101 /var/lib/kaf-mirror && chmod 0770 /var/lib/kaf-mirror'
kubectl apply -f - <<'YAML'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: kaf-mirror-manual
provisioner: kubernetes.io/no-provisioner
reclaimPolicy: Retain
volumeBindingMode: WaitForFirstConsumer
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: km-kaf-mirror-data
spec:
  capacity:
    storage: 2Gi
  accessModes: [ReadWriteOnce]
  storageClassName: kaf-mirror-manual
  persistentVolumeReclaimPolicy: Retain
  hostPath:
    path: /var/lib/kaf-mirror
    type: Directory
YAML
cat > storage-values.yaml <<'VALUES'
persistence:
  storageClass: kaf-mirror-manual
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f storage-values.yaml --wait
```

The PVC of the chart asks for the `kaf-mirror-manual` class, and Kubernetes binds it to this volume. The pod must run on the node that has the directory, which is always the case on a one-node kind cluster.

kaf-mirror sets `/app/data` to mode `0700` only when it owns the directory. If the directory belongs to another user, kaf-mirror leaves its mode alone and logs a warning when the directory is open to the group or others. The database, its `-wal` and `-shm` files and `kaf-mirror.db.lock` are always created with mode `0600`. An existing database file of another user that the group or others can read stops the start, because kaf-mirror cannot make it private.

## Back up the database

The database is `/app/data/kaf-mirror.db` on the PVC. `admin-cli backup database` takes a consistent copy with `VACUUM INTO` while the pod runs, including writes still in the WAL file, and creates it with mode `0600`. Write the backup to the PVC, copy it out with a private umask, then delete it in the pod:

```bash
kubectl exec deploy/km-kaf-mirror -- ./admin-cli --db-path /app/data/kaf-mirror.db backup database /app/data/kaf-mirror-backup.db
(umask 077 && kubectl exec deploy/km-kaf-mirror -- cat /app/data/kaf-mirror-backup.db > kaf-mirror-backup.db)
kubectl exec deploy/km-kaf-mirror -- rm /app/data/kaf-mirror-backup.db
```

Breaking: the backup refuses to overwrite an existing file, so delete the old copy in the pod first or pick a new name. `admin-cli restore database` takes `kaf-mirror.db.lock` and holds it through the rename, so it cannot run in the kaf-mirror pod while the server runs, and the server will not start while the restore holds the lock. A leftover `-wal` file is not enough to block a restore. Processes that open the database without the lock are not detected, so scale the deployment to 0 first. Then run `admin-cli restore database` with `--yes` in a separate pod that mounts the same PVC. The chart has no helper for that yet. Restore is supported on Linux and macOS, not on Windows. See "Backup and Data Management" in the main README.

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
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f tls-values.yaml --wait
```

The upgrade replaces the pod, so start `kubectl port-forward svc/km-kaf-mirror 8088:8088` again. Health is now HTTPS:

```bash
curl -sk https://localhost:8088/health
```

## Kafka TLS

For brokers with a private CA or mTLS, keep the CA certificate and the client certificate and key in a Secret and set `kafkaTLS.existingSecret`. The chart mounts every key of the Secret read-only at `/app/kafka-tls` with mode `0440`, readable by the pod group 101. Those files must be regular files of at most 1 MiB; a symlink to a regular file, as used by Kubernetes Secret mounts, is accepted. Then set `ca_file`, `cert_file` and `key_file` to those paths in `security_config` of the cluster you create with the API, or in `config.sourceSecurity` and `config.targetSecurity`. See "TLS with a private CA or mTLS" in the [main README](../../../README.md#tls-with-a-private-ca-or-mtls).

```bash
kubectl create secret generic km-kafka-tls --from-file=ca.crt=./ca.crt --from-file=tls.crt=./tls.crt --from-file=tls.key=./tls.key
cat > kafka-tls-values.yaml <<'VALUES'
kafkaTLS:
  existingSecret: km-kafka-tls
config:
  sourceBrokers: "broker1.example.com:9093"
  sourceSecurity:
    protocol: SSL
    ca_file: /app/kafka-tls/ca.crt
    cert_file: /app/kafka-tls/tls.crt
    key_file: /app/kafka-tls/tls.key
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f kafka-tls-values.yaml --wait
```

Start `kubectl port-forward svc/km-kaf-mirror 8088:8088` again.

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
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f secret-values.yaml --wait
```

`tls-values.yaml` and `secret-values.yaml` are both written above. An upgrade that uses them together:

```bash
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f tls-values.yaml -f secret-values.yaml --wait
```

Start `kubectl port-forward svc/km-kaf-mirror 8088:8088` again. With `tls-values.yaml`, use the HTTPS health check from the TLS section.

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
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f kerberos-values.yaml --wait
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
helm upgrade --install km deploy/helm/kaf-mirror -f km-values.yaml -f shutdown-values.yaml --wait
```

Start `kubectl port-forward svc/km-kaf-mirror 8088:8088` again.

## Values

| Key | Default | Description |
|---|---|---|
| `nameOverride` | `""` | Replaces the chart name `kaf-mirror` in the `app.kubernetes.io/name` label and in the full object name `<release>-<name>`. |
| `fullnameOverride` | `""` | Sets the full object name. Empty uses `<release>-kaf-mirror`, or `<release>` when the release name already contains the chart name. The commands on this page use the release `km`, so the objects are named `km-kaf-mirror`. |
| `image.repository` | `ghcr.io/scalytics/kaf-mirror` | Image repository. |
| `image.tag` | `v2.0.0` | Image tag. It follows `appVersion`. To run the current `main` branch, set this to an image built from this repository, as in the Install section. |
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
| `config.server.corsAllowedOrigins` | `["http://localhost:8080"]` | `server.cors.allowed_origins`. The web UI is served by kaf-mirror and does not need CORS. Add an origin only for a browser app on another origin. |
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
| `kafkaTLS.existingSecret` | `""` | Secret with Kafka client TLS files, such as `ca.crt`, `tls.crt` and `tls.key`. When set, every key is mounted at `/app/kafka-tls`. |
| `kerberos.keytabSecret` | `""` | Secret with the Kerberos keytab. When set, it is mounted at `/app/kerberos/keytab`. |
| `kerberos.keytabKey` | `kafka.keytab` | Key of the keytab in the Secret, and its file name in the mount. |
| `kerberos.krb5ConfigMap` | `""` | ConfigMap with `krb5.conf`. When set, it is mounted at `/app/kerberos/krb5`. |
| `kerberos.krb5ConfKey` | `krb5.conf` | Key of `krb5.conf` in the ConfigMap, and its file name in the mount. |
| `secretEnv` | `[]` | List of `name`, `secretName` and `key`. Each entry sets an environment variable from a Secret key. |
| `persistence.enabled` | `true` | Keep `/app/data` on a PVC. With `false`, data is lost when the pod is replaced. |
| `persistence.storageClass` | `""` | StorageClass of the PVC. Empty uses the cluster default. |
| `persistence.size` | `2Gi` | PVC size. |
| `volumePermissions.enabled` | `false` | Run a root init container (`runAsUser` 0, `runAsNonRoot: false`) that gives `/app/data` to UID 100 and GID 101 and sets mode `0770`. It adds `CHOWN`, `FOWNER` and `DAC_OVERRIDE`. Use this on kind or k3s `local-path` storage, which does not honor `fsGroup`. A namespace under the restricted Pod Security standard rejects that pod: use storage that honors `fsGroup`, or a volume already owned by UID 100 and GID 101, and leave this `false`. See "Storage permissions". |
| `volumePermissions.image` | `busybox:1.37`, `IfNotPresent` | Image of the init container. |
| `resources` | see `values.yaml` | Container resources. |
| `probes.liveness` | `initialDelaySeconds: 15`, `periodSeconds: 20` | Liveness probe timing. The probe checks `/health` on the `http` port. |
| `probes.readiness` | `initialDelaySeconds: 5`, `periodSeconds: 10` | Readiness probe timing. The probe checks `/health` on the `http` port. |
| `podSecurityContext` | UID 100, GID 101, `fsGroup` 101, `fsGroupChangePolicy` `OnRootMismatch` | Pod security context. The IDs match the `kaf-mirror` user in the image. See "Storage permissions". |
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

`make helm-check` lints the chart and renders the default, TLS, Secret, ServiceMonitor, Kafka TLS and Kerberos cases. `go test ./tests/deploy/` fails when a key in `values.yaml` is not read by any template, when a template reads a key that is not in `values.yaml` or in this table, or when a `helm` command on this page does not pass `-f km-values.yaml`.
