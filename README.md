# kaf-mirror

**kaf-mirror** is a high-performance, AI-enhanced Kafka replication tool designed for robust and intelligent data mirroring between Kafka clusters.

## Features

- **Core Replication Engine:** Built with `franz-go`, it handles consuming from a source Kafka cluster and producing to a target cluster based on defined topic mappings (exact and regex).
- **Enterprise Web Dashboard:** Professional tab-based interface with Operations, Health, Executive, and Compliance sections.
- **Hybrid Configuration:** The application loads its base configuration from a `default.yml` file and stores runtime changes (like replication jobs) in a SQLite database.
- **Comprehensive REST API:** A full-featured API built with Fiber provides endpoints for managing the application.
- **Job Management:** A `JobManager` controls the lifecycle of replication jobs (start, stop, pause).
- **AI-Powered Insights:**
    - **Anomaly Detection:** Automatically detects anomalies in replication metrics.
    - **Performance Tuning:** Provides recommendations for optimizing replication performance.
    - **Incident Analysis:** Offers detailed analysis of operational incidents.
- **Compliance Reporting:** Automated daily/weekly/monthly compliance reports with audit trails.
- **Role-Based Security:** Token-based authentication with admin/operator/monitoring/compliance roles.
- **Admin CLI:** A command-line tool for bootstrapping the application and for emergency maintenance.
- **Unified Management CLI:** A single, powerful CLI for all ongoing management tasks.
- **Real-Time WebSocket:** Live dashboard updates with secure token-based WebSocket authentication.

## Getting Started

### 1. How to Run Locally (Docker)

- Start everything (app + two local Kafka brokers) from the repo root:
  ```bash
  make dev-up   # uses docker compose
  ```
- The UI/API is at `http://localhost:8080`.
- Stop and clean:
  ```bash
  make dev-down
  ```
- Data and logs are under `./data` and `./logs`.

Admin bootstrap is automatic: on first start, the server seeds roles/permissions and creates `admin@localhost` with a random password printed in the logs. If you need to recreate roles or (re)bootstrap when the DB is empty:
```bash
docker-compose -p kaf-mirror exec kaf-mirror ./admin-cli repair
```
To reset the initial admin password:
```bash
docker-compose -p kaf-mirror exec kaf-mirror ./admin-cli reset-admin-password admin@localhost
```

### 1b. Run from prebuilt image (GHCR)

You can pull and run the published container without building locally:
```bash
docker pull ghcr.io/scalytics/kaf-mirror:latest
docker run -p 8080:8080 ghcr.io/scalytics/kaf-mirror:latest
```
The UI/API will be at `http://localhost:8080`.

## Resilience and Buffering

The application is designed to be resilient to network issues and outages of the target cluster. It does not require a separate disk-based buffer for the following reasons:

*   **Transient Network Issues:** The internal Kafka producer has an in-memory buffer and an automatic retry mechanism. This handles short-term network interruptions without data loss or duplication, thanks to its default idempotent configuration.
*   **Prolonged Target Cluster Outages:** If the target cluster is unavailable for an extended period, the application will experience backpressure. The consumer will stop reading messages from the source cluster, and its consumer offset will not advance. The source Kafka cluster itself acts as the durable, large-scale buffer, retaining the data until the target cluster is available again. When the connection is restored, the application will resume mirroring from the last committed offset, ensuring no data is lost.

**b) Data Retention Policy**

To prevent the internal SQLite database from growing indefinitely, the application enforces a 7-day retention policy for the following data:

*   **Replication Metrics:** Time-series data on throughput, lag, etc.
*   **Operational Events:** Audit logs of all state-modifying actions.

This data is automatically pruned every 24 hours.

**Optional) Building binaries on your host**

```bash
make build
```
This produces `bin/kaf-mirror`, `bin/admin-cli`, and `bin/mirror-cli` for direct use outside Docker.

### 2. Admin and Management

All ongoing management is performed via the `mirror-cli`, which interacts with the secure, role-based API.

**a) Initial Setup & Configuration**

The server auto-bootstraps on first start: it seeds default roles/permissions and creates `admin@localhost` with a random password printed once in the logs. For repairs or reseeding when empty, use:
```bash
./admin-cli repair
```
To reset the initial admin password:
```bash
./admin-cli reset-admin-password admin@localhost
```

**b) `mirror-cli`: The Unified Management Tool**

**Login**

Before you can use the `mirror-cli`, you need to log in to the backend:

```bash
./mirror-cli login <username> --password <password>
```

This will authenticate with the API and store a session token securely on your local machine.

**Configuration Management**

- `./mirror-cli config get`: Get the current configuration from the server.
- `./mirror-cli config set [config-file]`: Set the configuration on the server.
- `./mirror-cli config configure`: Create or update the configuration file using an interactive wizard. (Requires admin role)
- `./mirror-cli config edit`: Edit the configuration file in your default editor. (Requires admin role)

**Cluster Management**

- `./mirror-cli clusters list`: Lists all configured Kafka clusters.
- `./mirror-cli clusters add --name <cluster-name> --brokers <broker-list>`: Adds a new Kafka cluster.
- `./mirror-cli clusters remove <cluster-name>`: Marks a cluster as inactive. (Requires admin role)
- `./mirror-cli clusters purge`: Purges all archived clusters. (Requires admin role)

**Replication Job Management**

- `./mirror-cli jobs list`: List all replication jobs with status and performance metrics.
- `./mirror-cli jobs add`: Create a new replication job with interactive configuration:
  - Automatic topic discovery from source cluster
  - Topic pattern support with wildcards (`user.*`, `events-*`)
  - Interactive topic selection or manual input
  - Comprehensive replication settings (batch size, parallelism, compression)
  - Custom target topic name mapping
- `./mirror-cli jobs start [job-id]`: Start paused or stopped replication jobs with interactive selection.
- `./mirror-cli jobs stop [job-id]`: Stop running replication jobs with interactive selection.
- `./mirror-cli jobs pause [job-id]`: Pause running replication jobs (resumable) with interactive selection.
- `./mirror-cli jobs delete [job-id]`: Delete replication jobs with confirmation (admin only).
- `./mirror-cli jobs status [job-id]`: Show detailed job status and performance metrics.

**User Management**

- `./mirror-cli users list`: Lists all users. Requires `users:list` permission.
- `./mirror-cli users add <username> --password <password> --role <role>`: Adds a new user. Requires `users:create` permission. Operators can only create users with the `monitoring` role.
- `./mirror-cli users set-role <username> <role>`: Sets a user's role. Requires `users:assign-roles` permission. Users cannot change their own role.
- `./mirror-cli users reset-password [username]`: Reset another user's password and generate a new secure password. **Admin only**. Cannot reset own password.
- `./mirror-cli users delete <username>`: Delete a user account. Requires `users:delete` permission.
- `./mirror-cli whoami`: Displays information about the current user.
- `./mirror-cli change-password`: Changes the current user's password.
- `./mirror-cli reset-token`: Resets the current user's API token.

**Monitoring**

- `./mirror-cli dashboard`: Show a live monitoring dashboard. Users with `admin` or `operator` roles will see additional information.

**TLS Certificate Management**

The system includes comprehensive TLS certificate management for secure communications:

- `./mirror-cli tls generate-certs`: Generate self-signed CA, server, and client certificates
- `./mirror-cli tls import-cert`: Import existing TLS certificates and configure server
- `./mirror-cli tls configure-client`: Configure CLI for HTTPS communication
- `./mirror-cli tls verify`: Test TLS connection and certificate validation

TLS features include:
- Automatic HTTPS server startup when certificates are configured
- Support for both self-signed and CA-signed certificates
- Client certificate authentication for CLI-to-server communication
- Certificate validation and compatibility checking
- Insecure mode support for development with self-signed certificates

**AI Configuration Management**

The system supports multiple AI providers for operational intelligence:

- `./bin/mirror-cli config edit ai provider`: Configure AI provider (OpenAI, Claude, Gemini, Grok, Custom)
- `./bin/mirror-cli config edit ai model`: Set AI model for selected provider
- Provider-specific configuration with API endpoints and credentials

Supported AI Providers:
- **OpenAI**: GPT-4, GPT-4-turbo, GPT-3.5-turbo models
- **Claude**: Anthropic Claude-3 models (Opus, Sonnet, Haiku)
- **Gemini**: Google Gemini Pro and Vision models
- **Grok**: xAI Grok beta models
- **Custom**: Support for custom API endpoints with configurable models

AI Features:
- Automatic provider endpoint configuration
- Secure API key management
- Model selection with provider-specific options
- Custom provider support for enterprise deployments
- API secret support for providers requiring dual authentication

**Compliance Reporting**

The system includes comprehensive compliance reporting capabilities:

- **Web Dashboard**: Access compliance reports through the web interface at `http://localhost:8080` or `https://localhost:8080` (with TLS)
- **Report Generation**: Generate daily, weekly, or monthly compliance reports in CSV format
- **Audit Trails**: All reports include detailed audit trails and security metrics
- **Role-Based Access**: Only users with `compliance` role can generate and view reports
- **Token Validation**: Dashboard includes automatic token validation and logout for expired sessions

Compliance features include:
- System access audit logs
- Data processing statistics
- Security event tracking
- Configuration change history
- AI usage analytics
- Performance metrics analysis

**c) `admin-cli`: Recovery**

- `./admin-cli repair`: Reseed default roles/permissions; if no users exist, create `admin@localhost` with a random password.
- `./admin-cli reset-admin-password <username>`: Reset the password for the initial admin user (the one marked `is_initial`).
- `./admin-cli users list`: List users directly from the database.
- `./admin-cli users add <username> --password <password>`: Adds a new user directly to the database.

**d) Backup and Data Management**

The `admin-cli` provides comprehensive backup, restore, and import capabilities for disaster recovery and system migration.

**Backup Operations**

Create backups of your kaf-mirror system data:

- `./admin-cli backup database <output-path>`: Backup the SQLite database containing all jobs, users, clusters, and operational data. Automatically appends timestamp if extension not provided.
- `./admin-cli backup config <output-path>`: Backup the configuration file. Use `--config-path` flag to specify custom config location.
- `./admin-cli backup full <output-directory>`: Create complete system backup including database and configuration in a timestamped directory.

Examples:
```bash
# Backup database with auto-generated filename
./admin-cli backup database my-backup
# Creates: my-backup-backup-20240813-150405.db

# Backup configuration
./admin-cli backup config config-backup --config-path configs/production.yml

# Full system backup
./admin-cli backup full /backup/location
# Creates: /backup/location/kaf-mirror-backup-20240813-150405/
```

**Restore Operations**

Restore from previously created backups:

- `./admin-cli restore database <backup-path>`: Restore database from backup file. **Warning: Overwrites current database.**
- `./admin-cli restore config <backup-path>`: Restore configuration from backup. Use `--config-path` flag to specify target location.

Examples:
```bash
# Restore database (requires confirmation)
./admin-cli restore database my-backup-backup-20240813-150405.db

# Restore configuration to custom location
./admin-cli restore config config-backup.yml --config-path configs/default.yml
```

**Import Operations**

Import data from external kaf-mirror instances or other sources:

- `./admin-cli import database <source-path>`: Import database from another kaf-mirror instance. **Replaces current database.**
- `./admin-cli import config <source-path>`: Import configuration from external source.
- `./admin-cli import full <source-directory>`: Import complete system from external backup directory.

Examples:
```bash
# Import database from another system
./admin-cli import database /path/to/external/kaf-mirror.db

# Import configuration
./admin-cli import config /path/to/external/config.yml

# Import full system backup
./admin-cli import full /path/to/kaf-mirror-backup-20240813-150405/
```

**Backup Best Practices**

- **Regular Backups**: Schedule daily database backups using cron jobs
- **Configuration Versioning**: Backup configuration before making changes
- **Migration Support**: Use full backups when migrating between environments
- **Disaster Recovery**: Store backups in separate location from production system
- **Testing**: Regularly test restore procedures in non-production environment

**Migration Workflow**

To migrate kaf-mirror between systems:

1. **Source System**: Create full backup
   ```bash
   ./admin-cli backup full /tmp/migration-backup
   ```

2. **Target System**: Import full backup
   ```bash
   ./admin-cli import full /tmp/migration-backup/kaf-mirror-backup-20240813-150405
   ```

3. **Verification**: Verify all clusters, jobs, and users migrated correctly
   ```bash
   ./mirror-cli login <username>
   ./mirror-cli clusters list
   ./mirror-cli jobs list
   ```

### 3. Run as a Service (Linux)

To install and run `kaf-mirror` as a `systemd` service on Linux, you can use the provided installation script:

```bash
./scripts/install.sh
```

This script will:
1.  Create a dedicated user and group for the service.
2.  Create the necessary directories.
3.  Copy the binaries and configuration to the correct locations.
4.  Install and start the `systemd` service.

### 4. Run in Kubernetes

To deploy the application to Kubernetes, you will first need to build the Docker image:

```bash
docker build -t kaf-mirror:latest .
```

Once the image is built, you can apply the Kubernetes configuration files:

```bash
kubectl apply -f k8s/
```

This will create a deployment, a service, a persistent volume claim, a configmap, and a secret in your Kubernetes cluster.

**Configuration:**

*   **`k8s/configmap.yaml`**: You will need to edit this file to provide the correct Kafka broker addresses.
*   **`k8s/pvc.yaml`**: This file defines the persistent storage for the application's internal SQLite database. **Important:** This storage is for metadata (users, job configurations, audit logs, metrics) and is NOT used to buffer the data being mirrored. The size required does not depend on your Kafka topic throughput. The default of 1Gi is a generous starting point for most use cases. In a production environment, ensure this PVC is backed by a high-performance `StorageClass` (e.g., SSDs). You can monitor its usage and expand the volume later if needed.
*   **`k8s/secrets.yaml`**: This file provides a template for managing secrets. You will need to add your base64-encoded secrets (e.g., TLS keys, API tokens) to this file.
*   **`k8s/deployment.yaml`**: This file defines the deployment of the application. It is configured to use the `ConfigMap` for configuration, the `PersistentVolumeClaim` for data storage, and the `Secret` for sensitive information.
*   **`k8s/service.yaml`**: This file exposes the application via a `LoadBalancer` service.

## Security

### Kerberos Authentication

This application supports Kerberos authentication for connecting to Kafka clusters. To enable Kerberos, you will need to configure the following in your `configs/default.yml` file for both the source and target clusters:

```yaml
source_cluster:
  brokers: "broker1:9092,broker2:9092"
  security:
    enabled: true
    sasl_mechanism: "GSSAPI"
    kerberos:
      service_name: "kafka"
      jaas_config: |
        com.sun.security.auth.module.Krb5LoginModule required
        useKeyTab=true
        storeKey=true
        keyTab="/path/to/your/keytab.keytab"
        principal="your-principal@YOUR-REALM.COM";
```

## API Documentation

The full API documentation is available via Swagger at `http://localhost:8080/swagger/index.html`.

## Testing

To run the unit tests, use the following command:

```bash
go test ./...
```

## License

Apache License 2.0. See `LICENSE` for details.
