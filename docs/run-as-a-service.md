# Run as a service (Linux)

To install and run `kaf-mirror` as a `systemd` service on Linux, you can use the provided installation script:

```bash
./scripts/install.sh
```

This script will:
1.  Create a dedicated user and group for the service.
2.  Create the necessary directories.
3.  Copy the binaries and configuration to the correct locations.
4.  Install and start the `systemd` service.
