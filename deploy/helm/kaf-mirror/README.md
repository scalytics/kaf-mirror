# kaf-mirror Helm chart

The chart runs kaf-mirror as a single pod with a ConfigMap for its config, a PersistentVolumeClaim for SQLite and a Service.

## Install

```bash
helm install km deploy/helm/kaf-mirror --wait
kubectl port-forward svc/km-kaf-mirror 8088:8088
```

In another terminal:

```bash
curl -s http://localhost:8088/health
```

The default install starts without TLS: `config.server.mode` is `production` and `config.server.allowInsecure` is `true`. For production, give the server a certificate (see TLS below) and set `config.server.allowInsecure` to `false`, so the server refuses to start without TLS.

## Bootstrap admin password

On first start, kaf-mirror creates `admin@localhost` with a random password and writes it to `/app/data/kaf-mirror-bootstrap-<random>` on the PVC. The log prints the file path, not the password. Read it, deliver it securely, then delete the file:

```bash
kubectl logs deploy/km-kaf-mirror | grep "Password File"
kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /app/data/kaf-mirror-bootstrap-*'
kubectl exec deploy/km-kaf-mirror -- sh -c 'rm /app/data/kaf-mirror-bootstrap-*'
```

## TLS

Create a TLS Secret from your certificate and key files, then point the chart at it. The Secret is mounted read-only at `/app/tls`, and the liveness and readiness probes switch to HTTPS.

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
kubectl create secret generic km-secrets --from-file=ai-token=./ai-token.txt
cat > secret-values.yaml <<'VALUES'
secretEnv:
  - name: KAF_MIRROR_AI_TOKEN
    secretName: km-secrets
    key: ai-token
VALUES
helm upgrade --install km deploy/helm/kaf-mirror -f secret-values.yaml --wait
```

## Upgrades and SQLite

The Deployment uses the `Recreate` strategy. SQLite on a ReadWriteOnce volume must not be opened by two pods, so an upgrade stops the old pod before the new one starts. Expect a short gap in replication during an upgrade. Keep `replicaCount` at 1.

## Values

| Key | Default | Description |
|---|---|---|
| `image.repository` | `ghcr.io/scalytics/kaf-mirror` | Image repository. |
| `image.tag` | `v1.3.1` | Image tag. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `replicaCount` | `1` | Number of pods. Keep it at 1, SQLite is not shared. |
| `service.type` | `ClusterIP` | Service type. |
| `service.port` | `8088` | Service port. It forwards to the container port `http`. |
| `config.server.host` | `0.0.0.0` | Listen address, set as `server.host` and as `KAF_MIRROR_SERVER_HOST`. |
| `config.server.port` | `8080` | Container listen port, also used by the probes. |
| `config.server.mode` | `production` | `server.mode`. |
| `config.server.allowInsecure` | `true` | `server.allow_insecure`. With `false` and no TLS, the server refuses to start in `production` mode. |
| `config.server.tls.existingSecret` | `""` | Name of a Secret with the certificate and key. When set, TLS is on and the probes use HTTPS. |
| `config.server.tls.certKey` | `tls.crt` | Key of the certificate in the Secret. |
| `config.server.tls.keyKey` | `tls.key` | Key of the private key in the Secret. |
| `config.server.corsAllowedOrigins` | `["*"]` | `server.cors.allowed_origins`. |
| `config.logLevel` | `INFO` | `logging.level`. |
| `config.sourceBrokers` | `kafka-source:9092` | Brokers of the `source` cluster in the config file. |
| `config.targetBrokers` | `kafka-target:9092` | Brokers of the `target` cluster in the config file. |
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
| `metrics.enabled` | `true` | Render a ServiceMonitor when the Prometheus Operator CRD is installed. |
| `metrics.path` | `/metrics` | Metrics path. |
| `metrics.serviceMonitor.labels` | `{}` | Extra ServiceMonitor labels. |
| `metrics.serviceMonitor.interval` | `30s` | Scrape interval. |
| `metrics.serviceMonitor.scrapeTimeout` | `10s` | Scrape timeout. |
| `nodeSelector` | `{}` | Pod node selector. |
| `tolerations` | `[]` | Pod tolerations. |
| `affinity` | `{}` | Pod affinity. |

`make helm-check` lints the chart and renders the default, TLS, Secret and ServiceMonitor cases. `go test ./tests/deploy/` fails when a key in `values.yaml` is not read by any template.
