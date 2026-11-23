#!/usr/bin/env bash
set -euo pipefail

chart="deploy/helm/kaf-mirror"

fail() {
  echo "helm-check: $1" >&2
  exit 1
}

helm lint "$chart"

default_out=$(helm template km "$chart")
grep -q "type: Recreate" <<<"$default_out" || fail "default render has no Recreate strategy"
grep -q "allow_insecure: true" <<<"$default_out" || fail "default render does not allow an insecure listener"
grep -q "scheme: HTTP$" <<<"$default_out" || fail "default probes do not use HTTP"
if grep -qE "qip\.|ops-foundation|source_cluster|target_cluster|job_id" <<<"$default_out"; then
  fail "default render still has leftover project values"
fi

tls_out=$(helm template km "$chart" \
  --set config.server.tls.existingSecret=km-tls \
  --set config.server.allowInsecure=false \
  --set-json 'secretEnv=[{"name":"KAF_MIRROR_AI_TOKEN","secretName":"km-secrets","key":"ai-token"}]')
grep -q "secretName: km-tls" <<<"$tls_out" || fail "tls render does not mount the Secret"
grep -q 'cert_file: "/app/tls/tls.crt"' <<<"$tls_out" || fail "tls render has no cert_file"
grep -q 'key_file: "/app/tls/tls.key"' <<<"$tls_out" || fail "tls render has no key_file"
grep -q "allow_insecure: false" <<<"$tls_out" || fail "tls render still allows an insecure listener"
grep -q "scheme: HTTPS" <<<"$tls_out" || fail "tls probes do not use HTTPS"
grep -q "name: km-secrets" <<<"$tls_out" || fail "secretEnv is not rendered as a secretKeyRef"

monitor_out=$(helm template km "$chart" --api-versions monitoring.coreos.com/v1)
grep -q "kind: ServiceMonitor" <<<"$monitor_out" || fail "ServiceMonitor is not rendered"
if grep -q "scheme: https" <<<"$monitor_out"; then
  fail "default ServiceMonitor scrapes HTTPS"
fi

tls_mon_out=$(helm template km "$chart" --api-versions monitoring.coreos.com/v1 --set config.server.tls.existingSecret=km-tls --set config.server.allowInsecure=false)
grep -q "kind: ServiceMonitor" <<<"$tls_mon_out" || fail "ServiceMonitor is not rendered with TLS"
grep -q "scheme: https" <<<"$tls_mon_out" || fail "tls ServiceMonitor does not scrape HTTPS"

echo "helm-check: ok"
