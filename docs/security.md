# Security

## Kafka Cluster Authentication

The consumer, the producer, the admin client, `POST /api/v1/clusters/test`, the topic list and detail views, the cluster status check and the inventory snapshot all build their Kafka client options with one function, `kafka.ClientOptions`. A cluster that connects in the test also connects when a job runs.

*   **TLS:** on when `security.protocol` is `SSL` or `SASL_SSL`, and always for the `confluent` and `azure` providers. The client checks the broker certificate against the system trust store, or against `ca_file` when it is set, and needs TLS 1.2 or newer. `cert_file` and `key_file` send a client certificate for mTLS. See "TLS with a private CA or mTLS" below.
*   **Generic SASL:** used when `security.enabled` is `true` and `security.sasl_mechanism` is set. `PLAIN`, `SCRAM-SHA-256` and `SCRAM-SHA-512` need `username` and `password`. `GSSAPI` needs the Kerberos settings below. Any other mechanism is an error.
*   **Provider presets:** `confluent` uses SASL PLAIN with `api_key` and `api_secret`. `azure` uses SASL PLAIN with the user `$ConnectionString` and `connection_string`. `redpanda` with `username` and `password` and no `sasl_mechanism` uses TLS and SCRAM-SHA-256. With an explicit `sasl_mechanism`, a `redpanda` cluster uses the generic settings.
*   **Validation:** creating or updating a cluster and `POST /api/v1/clusters/test` answer HTTP 400 when `security_config` is not a JSON object, a mechanism is unknown, a password is missing, a GSSAPI path is missing, only one of `cert_file` and `key_file` is set, `ca_file`, `cert_file` or `key_file` is set on a cluster without TLS, or a set `ca_file`, `cert_file` or `key_file` exists and is not a regular file or is larger than 1 MiB.
*   **Logs:** kaf-mirror logs the provider, whether TLS is on and the mechanism name. It never logs a password, API secret or connection string.

Jobs use the clusters stored in the database. Create them with `mirror-cli clusters add`, which asks for the password without echo, or with `POST /api/v1/clusters`. The security settings go in `security_config`, a JSON object written as a string. All fields are listed in [docs/configuration.md](configuration.md#cluster-api-fields). The `clusters` section of the config file is only used by `GET /api/v1/topics/source` and `/target`, and for `disable_idempotent_writes`.

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

## TLS with a private CA or mTLS

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

In Kubernetes with the Helm chart, keep the files in a Secret and set `kafkaTLS.existingSecret`. The chart mounts every key of the Secret at `/app/kafka-tls`. See "Kafka TLS" in [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md).

## Kerberos (GSSAPI)

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

In Kubernetes with the Helm chart, keep the keytab in a Secret and `krb5.conf` in a ConfigMap, and set `kerberos.keytabSecret` and `kerberos.krb5ConfigMap`. The chart mounts them at `/app/kerberos/keytab` and `/app/kerberos/krb5`. See "Kerberos" in [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md).
