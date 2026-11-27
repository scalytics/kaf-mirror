# kaf-mirror documentation

The [README](../README.md) has the 60 second quick start. Everything else is here.

## Run it

- [Getting started](getting-started.md): Docker, a prebuilt image, bind mounts and binaries you build yourself.
- [Kubernetes and Helm](kubernetes.md): the Helm chart, TLS, secrets and upgrades. The chart values are in the [chart README](../deploy/helm/kaf-mirror/README.md).
- [Run as a service](run-as-a-service.md): a systemd service on Linux.
- [Configuration](configuration.md): every config key, environment variable, cluster and job API field, and AI analysis.

## Operate it

- [Administration](administration.md): `mirror-cli` and `admin-cli`, cluster status, users, compliance reports, backup, restore and import.
- [CLI command reference](cli-commands.md): the generated `mirror-cli` reference.
- [Shell completion](shell-completion.md): Bash, Zsh, Fish and PowerShell.
- [Role-based access control](rbac.md): the permissions of each role per endpoint.
- [Security](security.md): Kafka authentication, TLS with a private CA or mTLS, and Kerberos.
- [Replication halt](protection.md) (tech preview): off by default and admin-only. Stops all replication if the source looks compromised.

## How it works

- [Topic mappings](topic-mappings.md): exact names, regex and target names.
- [What a copied record keeps](record-fidelity.md): keys, values, headers, partitions and timestamps.
- [Job metrics](metrics.md): counters, lag and how they are counted.
- [Resilience and buffering](resilience.md): retries, outages, offsets and shutdown.
- [Architecture](architecture.md): where kaf-mirror fits.
- [Features](features.md): the full feature list.
- [API](api.md): Swagger and the OpenAPI files.

## Use it for a backup

- [Back up to S3 with KafScale](kafscale-backup.md): mirror into a Kafka-compatible target that keeps immutable segments in S3.

## Contribute

- [Testing](testing.md)
- [CONTRIBUTING.md](../CONTRIBUTING.md)
