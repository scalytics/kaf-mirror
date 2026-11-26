# Kubernetes and Helm

Use the Helm chart below. It is the supported way to run kaf-mirror in Kubernetes, and CI lints and renders it with `make helm-check`. The raw manifests that were in `k8s/` were removed: they used config keys kaf-mirror does not read, did not start with their defaults (`mode: production` without TLS), mounted a Secret at a path nothing reads, and had no `Recreate` strategy and no shutdown grace period.

## Run with Helm

The Helm chart is in `deploy/helm/kaf-mirror`. A default install starts and answers `/health` on storage that honors `fsGroup`. The chart's default image tag is `v2.0.0`. To run the current `main` branch instead, build the image from this repository, load it into the cluster, and point the chart at it. `image.pullPolicy` is `IfNotPresent`, so the node that runs the pod must already have the image.

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

That init container runs as root and adds `CHOWN`, `FOWNER` and `DAC_OVERRIDE`. A namespace under the restricted Pod Security standard rejects it. Use storage that honors `fsGroup`, or a volume already owned by UID 100 and GID 101, and leave `volumePermissions.enabled` false. See "Storage permissions" in [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md).

Pass `-f km-values.yaml` first on every later `helm upgrade`, followed by the values files of the other settings you use. Helm does not keep values from an earlier upgrade, so an upgrade without `km-values.yaml` goes back to the default image. An upgrade that replaces the pod drops `kubectl port-forward`. Start it again.

*   **TLS:** by default the server runs without TLS (`config.server.allowInsecure: true`). For production, create a TLS Secret, set `config.server.tls.existingSecret` to its name and `config.server.allowInsecure` to `false`. The Secret is mounted at `/app/tls`, the probes use HTTPS, and the ServiceMonitor scrapes HTTPS. Start the port-forward again after that upgrade and check `curl -sk https://localhost:8088/health`.
*   **Kafka TLS:** for a private CA or mTLS to the brokers, put the files in a Secret and set `kafkaTLS.existingSecret`. The Secret is mounted at `/app/kafka-tls`.
*   **Tokens:** `secretEnv` sets `KAF_MIRROR_AI_TOKEN`, `KAF_MIRROR_AI_API_SECRET` or `KAF_MIRROR_MONITORING_SPLUNK_HEC_TOKEN` from a Secret key. Do not pass secrets with `--set`.
*   **Metrics:** the ServiceMonitor scrapes `/metrics`, which serves only the Go runtime metrics of the process. Job metrics are not on `/metrics`. They are stored in SQLite, returned by `GET /api/v1/jobs/{id}/metrics/current` and pushed to a Prometheus Pushgateway when `monitoring.platform` is `prometheus`.
*   **AI:** off by default. Set `config.ai.provider` to turn it on. See [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md#ai-analysis).
*   **Upgrades:** the Deployment uses the `Recreate` strategy, because SQLite on a ReadWriteOnce volume must not be opened by two pods. Keep one replica.
*   **Shutdown:** `terminationGracePeriodSeconds` (default 45) must stay longer than `config.server.shutdownTimeoutSeconds` (default 30), which sets `server.shutdown_timeout`. The render fails otherwise. See "Graceful Shutdown" in [resilience.md](resilience.md).
*   **Bootstrap password:** it is written to `/app/data/kaf-mirror-bootstrap-<random>` on the PVC. Read it with `kubectl exec deploy/km-kaf-mirror -- sh -c 'cat /app/data/kaf-mirror-bootstrap-*'`, then delete the file.

All values are listed in [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md).
