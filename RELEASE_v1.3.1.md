# kaf-mirror v1.3.1 Release Notes

Release date: 2026-10-22

## Security
- Built with Go 1.26.10, which fixes CVE-2026-111943 (GO-2026-6809). Certificate.Verify in crypto/x509 now rejects a list of compromised intermediate certificates. Release binaries, the Docker image and CI all use Go 1.26.10.

## Docs
- README: Kafka-to-S3 backup quickstart, use cases, support links and related projects.

## Upgrade notes
- No configuration or API changes since v1.3.0. Replace the binary or pull `ghcr.io/scalytics/kaf-mirror:v1.3.1`.
