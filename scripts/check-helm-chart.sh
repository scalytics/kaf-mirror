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

krb_values=$(mktemp)
trap 'rm -f "$krb_values"' EXIT
cat >"$krb_values" <<'VALUES'
kerberos:
  keytabSecret: km-keytab
  krb5ConfigMap: km-krb5
config:
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
krb_out=$(helm template km "$chart" -f "$krb_values")
grep -q "secretName: km-keytab" <<<"$krb_out" || fail "kerberos render does not mount the keytab Secret"
grep -q "mountPath: /app/kerberos/keytab" <<<"$krb_out" || fail "kerberos render has no keytab mount"
grep -q "name: km-krb5" <<<"$krb_out" || fail "kerberos render does not mount the krb5.conf ConfigMap"
grep -q "mountPath: /app/kerberos/krb5" <<<"$krb_out" || fail "kerberos render has no krb5.conf mount"
grep -q "keytab_path: /app/kerberos/keytab/kafka.keytab" <<<"$krb_out" || fail "kerberos render has no keytab_path in the config"
grep -q "sasl_mechanism: GSSAPI" <<<"$krb_out" || fail "kerberos render has no GSSAPI source security"
if grep -q "kerberos-keytab" <<<"$default_out"; then
  fail "default render mounts a keytab"
fi

if helm template km "$chart" --set replicaCount=2 >/dev/null 2>&1; then
  fail "render accepts replicaCount other than 1"
fi
if helm template km "$chart" --set replicaCount=0 >/dev/null 2>&1; then
  fail "render accepts replicaCount 0"
fi
grep -q "replicas: 1$" <<<"$default_out" || fail "default render does not run exactly one replica"

grep -q "terminationGracePeriodSeconds: 45$" <<<"$default_out" || fail "default render has no terminationGracePeriodSeconds of 45"
grep -q 'shutdown_timeout: "30s"' <<<"$default_out" || fail "default render has no shutdown_timeout of 30s"
grace_out=$(helm template km "$chart" --set terminationGracePeriodSeconds=90 --set config.server.shutdownTimeoutSeconds=60)
grep -q "terminationGracePeriodSeconds: 90$" <<<"$grace_out" || fail "terminationGracePeriodSeconds is not read from the values"
grep -q 'shutdown_timeout: "60s"' <<<"$grace_out" || fail "shutdown_timeout is not read from the values"
if helm template km "$chart" --set terminationGracePeriodSeconds=30 >/dev/null 2>&1; then
  fail "render accepts a terminationGracePeriodSeconds that is not longer than the shutdown timeout"
fi
if helm template km "$chart" --set config.server.shutdownTimeoutSeconds=0 >/dev/null 2>&1; then
  fail "render accepts a shutdown timeout of 0"
fi

echo "helm-check: ok"
