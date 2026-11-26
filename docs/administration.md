# Administration

Day to day management with `mirror-cli` and recovery with `admin-cli`. The full command list is in [cli-commands.md](cli-commands.md). Shell completion is in [shell-completion.md](shell-completion.md).

All ongoing management is performed via the `mirror-cli`, which interacts with the secure, role-based API.

**a) Initial Setup & Configuration**

The server auto-bootstraps on first start: it seeds default roles/permissions and creates `admin@localhost` with a random password. The password is written to a `kaf-mirror-bootstrap-<random>` file next to the database, readable only by the kaf-mirror user, and the file path is printed on stdout as `Password File`. Read it, deliver it securely, and delete the file. For repairs or reseeding when empty, use:
```bash
./admin-cli repair
```
To reset the initial admin password. The new password is written to a `kaf-mirror-admin-reset-<random>` file in the temp directory, and the path is printed:
```bash
./admin-cli reset-admin-password admin@localhost
```
`admin-cli repair` also writes a new bootstrap password to a `kaf-mirror-bootstrap-<random>` file in the temp directory when it creates `admin@localhost`.

**b) `mirror-cli`: The Unified Management Tool**

**Login**

Before you can use the `mirror-cli`, you need to log in to the backend:

```bash
./mirror-cli login <username>
```

It asks for the password without echo. Breaking: the `--password` flag is removed, so the password never ends up in the shell history or the process list. This will authenticate with the API and store a session token in `~/.kaf-mirror/token` with mode `0600`.

Without a terminal, for example in a script, `login` reads the password from the first line of stdin:

```bash
read -rs KM_PASSWORD
printf '%s\n' "$KM_PASSWORD" | ./mirror-cli login admin@localhost
unset KM_PASSWORD
```

Running `mirror-cli` without a command starts the interactive shell, which needs a terminal on stdin and stdout. Without one it prints an error and exits with status 1. `mirror-cli --help` and the other commands work without a terminal. `mirror-cli dashboard` also needs a terminal. The `jobs` commands that take an optional job id, such as `jobs start`, `jobs status full` and `jobs delete`, ask you to select a job when you leave the id out. Without a terminal, pass the job id. Otherwise they print `Operation cancelled: no terminal to select a job, pass the job id`, exit with status 1 and send no request.

A command that fails exits with status 1. That includes an unknown subcommand, such as `clusters delete`. `mirror-cli --help` and a group command with no extra arguments, such as `mirror-cli clusters`, exit with status 0. The group command prints its help.

**Configuration Management**

- `./mirror-cli config get`: Get the current configuration from the server.
- `./mirror-cli config save [file]`: Save a configuration file to the server. (Requires admin role)
- `./mirror-cli config system`: Run the initial interactive configuration wizard. (Requires admin role)
- `./mirror-cli config edit`: Edit configuration sections interactively. (Requires admin role)

**Cluster Management**

- `./mirror-cli clusters list`: Lists all configured Kafka clusters.
- `./mirror-cli clusters add`: Adds a new Kafka cluster with interactive prompts.
- `./mirror-cli clusters remove <cluster-name>`: Marks a cluster as inactive. (Requires admin role)
- `./mirror-cli clusters purge`: Purges all archived clusters. (Requires admin role)

**Cluster Status**

Each cluster has a stored status, shown in `GET /api/v1/clusters`, `mirror-cli clusters list`, the web UI clusters tab, topology and "Active Clusters" count, and the `mirror-cli` dashboard:

*   `unknown`: not checked yet. A new or restored cluster has this status until its first check.
*   `active`: the last connection check reached the cluster.
*   `unreachable`: the last connection check failed or timed out.
*   `inactive`: removed with `clusters remove`. It is not checked, and it is archived after 72 hours.
*   `archived`: inactive for more than 72 hours. `clusters purge` deletes archived clusters.

kaf-mirror checks every cluster that is not `inactive` or `archived` when it starts and then every 60 seconds. Each check connects to the cluster with the same client options a job uses, has a 10 second timeout, and at most 4 probes run at a time in the whole process. That cap covers the background loop, `GET /api/v1/clusters/{name}/status`, checks after create, update or restore, and the pre-flight check of a job restart. Concurrent checks of the same cluster share one probe only when the connection settings still match, including provider, brokers, SASL and TLS settings, API key and secret, and the connection string. The probe fingerprint is an HMAC-SHA256 of the connection settings, keyed with a random key that exists only in this process, and includes an HMAC of the contents of `ca_file`, `cert_file` and `key_file`. The secrets and the file contents are never stored or logged in plain text. If that key cannot be created, or a TLS file cannot be read, a check is never shared and the cluster is probed again. A slower check does not overwrite a later result or a result from a newer cluster configuration, and a cluster that is deleted and created again under the same name is treated as a new check. A cluster that comes back is `active` again after the next check. The interval and the timeout have no config key.

`GET /api/v1/clusters/{name}/status` runs one of those checks now. It answers HTTP 200 with `{"status":"active","message":"Cluster is reachable"}` when the cluster is reached. When the check fails or times out, it stores `unreachable` and answers HTTP 503. If the request is cancelled while it waits for a free probe slot, the stored status is left alone. Concurrent requests for the same cluster share the in-flight probe. Inactive and archived clusters are not probed; the call answers HTTP 200 with their stored status.

**Replication Job Management**

- `./mirror-cli jobs list`: List all replication jobs with status and performance metrics.
- `./mirror-cli jobs add`: Create a new replication job with interactive configuration:
  - Automatic topic discovery from source cluster
  - Interactive selection of existing source topics. Regex mappings such as `user\..*` are set with the API, see [topic-mappings.md](topic-mappings.md)
  - Replication settings (batch size, compression)
  - Optional custom target topic mapping (exact or regex with capture substitution)
- `./mirror-cli jobs start [job-id]`: Start paused or stopped replication jobs with interactive selection.
- `./mirror-cli jobs stop [job-id]`: Stop running replication jobs with interactive selection.
- `./mirror-cli jobs pause [job-id]`: Pause running replication jobs (resumable) with interactive selection.
- `./mirror-cli jobs delete [job-id]`: Delete replication jobs with confirmation (admin only). The API call `DELETE /api/v1/jobs/{id}` answers HTTP 204 when the job is deleted, and HTTP 404 with `Job not found` when it does not exist.
- `./mirror-cli jobs status metrics [job-id]`: Show detailed job status and metrics.
- `./mirror-cli jobs status full [job-id]`: Show full mirror state (progress, gaps, resume points).

**User Management**

- `./mirror-cli users list`: Lists all users. Requires `users:list` permission.
- `./mirror-cli users add`: Adds a new user with interactive prompts and a generated password. The password is written to a new mode `0600` file in the temp directory, and only the file path is printed. Breaking: the unread `--password` flag is removed. Requires `users:create` permission, which only the `admin` role has.
- `./mirror-cli users set-role <username> <role>`: Sets a user's role. Requires `users:assign-roles` permission. Users cannot change their own role.
- `./mirror-cli users reset-password [username]`: Reset another user's password and generate a new secure password. **Admin only**. Cannot reset own password. The new password is written to a mode `0600` file. Without `--password-file`, it is a new file in the temp directory and its path is printed. With `--password-file`, the command does not print the path again, also not in an error.
- `./mirror-cli users delete <username>`: Delete a user account. Requires `users:delete` permission.
- `./mirror-cli whoami`: Displays the username and role of the current user. Breaking: it no longer prints the API token.
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

AI is off until `ai.provider` is set. Without a provider kaf-mirror sends nothing to any AI service, has no default endpoint, logs `AI analysis is off: ai.provider is not set` at start, and the AI endpoints that start an analysis answer HTTP 503. Restart kaf-mirror after you change the AI settings. See [docs/configuration.md](configuration.md#ai-analysis).

The system supports multiple AI providers for operational intelligence:

- `./mirror-cli config edit ai provider`: Configure AI provider (OpenAI, Claude, Gemini, Grok, Custom)
- `./mirror-cli config edit ai model`: Set AI model for selected provider
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
- Custom provider support for enterprise deployments (OpenAI-compatible proxies should use `custom`)
- API secret support for providers requiring dual authentication

**Compliance Reporting**

The system includes comprehensive compliance reporting capabilities:

- **Web Dashboard**: Access compliance reports through the web interface at `http://localhost:8080` or `https://localhost:8080` (with TLS)
- **Report Generation**: Generate daily, weekly, or monthly compliance reports in CSV format
- **Audit Trails**: All reports include detailed audit trails and security metrics
- **Role-Based Access**: Users with the `admin` or `compliance` role can generate and view reports. The web UI hides the report buttons from other users
- **Audit Trail Content**: The CSV audit trail lists every successful `POST`, `PUT`, `PATCH` and `DELETE` API request in the period, with the time in UTC, the user, the method and path, and the details. These are the same events as the activity log
- **Token Validation**: Dashboard includes automatic token validation and logout for expired sessions

Compliance features include:
- System access audit logs
- Data processing statistics
- Security event tracking
- Configuration change history
- AI usage analytics
- Performance metrics analysis

Automated scheduling is controlled by `compliance.schedule` (daily/weekly/monthly) and runs at `run_hour` local time; weekly runs on Monday and monthly runs on the first day of the month.

**c) `admin-cli`: Recovery**

- `./admin-cli repair`: Reseed default roles/permissions; if no users exist, create `admin@localhost` with a random password.
- `./admin-cli reset-admin-password <username>`: Reset the password for the initial admin user (the one marked `is_initial`).
- `./admin-cli users list`: List users directly from the database.
- `./admin-cli users add <username>`: Adds a new user directly to the database. It asks for the password without echo, or reads the first line of stdin when there is no terminal. Breaking: the `--password` flag is removed.

**d) Backup and Data Management**

The `admin-cli` provides backup, restore, and import commands for disaster recovery and system migration.

`admin-cli --help` needs no config file and no database. A database command works with `--db-path` and no config file when `CONFIG_PATH` is unset. If `CONFIG_PATH` is set and that file cannot be read, the command fails. Without `--db-path`, admin-cli reads `database.path` from the loaded config. A missing database path fails with `database path is required: pass --db-path or provide a config file`. `--db-path` is a flag of `admin-cli`. `--config-path` belongs to the backup, restore and import subcommand that uses it.

**Backup Operations**

`backup database` and `backup full` take a consistent copy of the SQLite database with `VACUUM INTO`. The copy includes writes that are still in the WAL file (`kaf-mirror.db-wal`), so you can run them while kaf-mirror runs. Every backup file is created with mode `0600`, because the database holds password hashes and API tokens and the config file can hold credentials, and `backup full` creates its directory with mode `0700`. Breaking: a backup never overwrites an existing file. If the path exists, the command fails and leaves the file alone.

- `./admin-cli backup database <output-path>`: Back up the SQLite database with all jobs, users, clusters and operational data. A path that does not end in `.db` gets `-backup-<timestamp>.db` added.
- `./admin-cli backup config <output-path>`: Back up the configuration file. A path that does not end in `.yml` gets `-config-backup-<timestamp>.yml` added. Use `--config-path` to name the config file.
- `./admin-cli backup full <output-directory>`: Write `database.db` and `config.yml` to a new `kaf-mirror-backup-<timestamp>` directory. The copied config is the base file that was loaded: the file named by `CONFIG_PATH`, or `default.yml` when `CONFIG_PATH` is unset. It is not the `prod.yml` overlay. Pass `--config-path` to copy a different file. When `--db-path` is set and no config was loaded, the default `--config-path` is `configs/default.yml`. If that file is missing, the database is still written and the command prints `Warning: configuration <path> was not included in the backup`.

Examples:
```bash
./admin-cli backup database my-backup
./admin-cli backup database kaf-mirror-copy.db
./admin-cli backup config config-backup --config-path configs/default.yml
./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db backup full /tmp/kaf-mirror-backups --config-path configs/default.yml
ls -l my-backup-backup-*.db config-backup-config-backup-*.yml /tmp/kaf-mirror-backups/kaf-mirror-backup-*/
```

The first command writes `my-backup-backup-<timestamp>.db`, for example `my-backup-backup-20240813-150405.db`. The third writes `config-backup-config-backup-<timestamp>.yml`.

For a backup of the database in the Helm chart, see "Back up the database" in [deploy/helm/kaf-mirror/README.md](../deploy/helm/kaf-mirror/README.md).

**Restore Operations**

Stop kaf-mirror before you restore or import a database. Restore and import take the exclusive lock on `kaf-mirror.db.lock` themselves and hold it through the copy, sidecar cleanup and rename, so the server cannot start in the middle of a restore. If the server already holds the lock, the command fails. A leftover `-wal` file after a crash does not block a restore. The lock does not see a process that opens the database without it, for example the `sqlite3` CLI or a forgotten admin-cli, so stop kaf-mirror first. It checks that the backup is a readable SQLite database, writes a copy next to the database (following a symlink at the database path), removes the old `-wal` and `-shm` files and then moves the copy in place with mode `0600`. The server takes the same lock before it opens the database, creates the data directory with mode `0700` and the database, `-wal` and `-shm` files with mode `0600`, and refuses to start with a clear error while a restore holds the lock. Database restore and import are supported on Linux and macOS, not on Windows. Release binaries of `admin-cli` and `kaf-mirror` are Linux-only; on a Mac you can `go build` them.

- `./admin-cli restore database <backup-path>`: Restore the database from a backup file. **Overwrites the current database.**
- `./admin-cli restore config <backup-path>`: Restore the configuration from a backup. Use `--config-path` to name the target file.

Restore and import ask for confirmation. Breaking: without a terminal, for example in a script, they fail with a non-zero exit status unless you pass `--yes`. Before they printed "cancelled" and exited 0.

Examples:
```bash
./admin-cli restore database my-backup-backup-20240813-150405.db
./admin-cli restore config config-backup-config-backup-20240813-150405.yml --config-path configs/default.yml
./admin-cli restore database my-backup-backup-20240813-150405.db --yes
```

Replace the timestamps with the ones of your backup files.

**Import Operations**

Import data from another kaf-mirror instance. `import database` and `import full` replace the database the same way as `restore database`.

- `./admin-cli import database <source-path>`: Import the database of another kaf-mirror instance. **Replaces the current database.**
- `./admin-cli import config <source-path>`: Import a configuration file.
- `./admin-cli import full <source-directory>`: Import `database.db` and `config.yml` from a `backup full` directory. `config.yml` is written to `--config-path` (default `configs/default.yml`). When the backup has no `config.yml`, the command prints `No configuration found in source directory, skipping.` and still imports the database.

Examples:
```bash
./admin-cli import database /path/to/external/kaf-mirror.db
./admin-cli import config /path/to/external/config.yml
./admin-cli import full /tmp/kaf-mirror-backups/kaf-mirror-backup-20240813-150405
```

**Backup Best Practices**

- **Regular Backups**: Schedule daily database backups, for example with cron. Each run needs a new file name, so keep the default timestamped name or add the date.
- **Configuration Versioning**: Back up the configuration before you change it.
- **Migration Support**: Use full backups when you move between environments.
- **Disaster Recovery**: Store backups away from the production system, readable only by the operators.
- **Testing**: Test the restore regularly in a non-production environment.

**Migration Workflow**

To migrate kaf-mirror between systems:

1. **Source system**: create a full backup. The backup directory gets `database.db` and `config.yml`. `--db-path` and `--config-path` name those files, so the command works from any directory. With no `--config-path`, `config.yml` is the loaded base file (`CONFIG_PATH`, or `default.yml`) when a config was loaded, and `configs/default.yml` when it was not.
   ```bash
   ./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db backup full /tmp/migration-backup --config-path /etc/kaf-mirror/default.yml
   ```

2. **Target system**: stop kaf-mirror, copy the backup directory over and import it. `--config-path` is where `config.yml` from the backup is written.
   ```bash
   ./admin-cli --db-path /var/lib/kaf-mirror/kaf-mirror.db import full /tmp/migration-backup/kaf-mirror-backup-20240813-150405 --config-path /etc/kaf-mirror/default.yml --yes
   ```

3. **Verification**: start kaf-mirror and check that all clusters, jobs and users were migrated.
   ```bash
   ./mirror-cli login admin@localhost
   ./mirror-cli clusters list
   ./mirror-cli jobs list
   ```


## Data retention

To prevent the internal SQLite database from growing indefinitely, the application enforces a 30-day retention policy by default for the following data:

*   **Replication Metrics:** Time-series data on throughput, lag, etc.
*   **Operational Events:** Audit logs of all state-modifying actions.

This data is automatically pruned every 24 hours. Admins can configure a shorter period via `database.retention_days` (1–30 days).

## Notes

Helm SQLite lives at `/app/data/kaf-mirror.db` on the PVC. Optional `egress.allowed_hosts` restricts AI and metrics HTTP destinations. Passwords must be at least 12 characters.
