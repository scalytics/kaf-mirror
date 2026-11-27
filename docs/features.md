# Features

The full feature list. The short version is in the [README](../README.md#what-it-does).

- **Core Replication Engine:** Built with `franz-go`, it handles consuming from a source Kafka cluster and producing to a target cluster with same-name mirroring by default, plus optional exact/regex mappings.
- **Web Dashboard:** Tab-based interface with Operations, Health, Executive, and Compliance sections.
- **Hybrid Configuration:** The application loads its base configuration from a `default.yml` file and stores runtime changes (like replication jobs) in a SQLite database. Every config key and environment variable is listed in [docs/configuration.md](configuration.md).
- **Comprehensive REST API:** A full-featured API built with Fiber provides endpoints for managing the application.
- **Job Management:** A `JobManager` controls the lifecycle of replication jobs (start, stop, pause).
- **AI-Powered Insights (opt-in):** Off until `ai.provider` is set. Without a provider kaf-mirror makes no AI requests. See [docs/configuration.md](configuration.md#ai-analysis).
    - **Anomaly Detection:** Automatically detects anomalies in replication metrics.
    - **Performance Tuning:** Provides recommendations for optimizing replication performance.
    - **Incident Analysis:** Offers detailed analysis of operational incidents.
- **Compliance Reporting:** Automated daily/weekly/monthly compliance reports with audit trails.
- **Role-Based Security:** Token-based authentication with admin/operator/monitoring/compliance roles.
- **Admin CLI:** A command-line tool for bootstrapping the application and for emergency maintenance.
- **Unified Management CLI:** A single CLI for all ongoing management tasks.
- **Dashboard updates:** The web UI polls the API every 30 seconds. `/ws` accepts authenticated WebSocket clients via an `Authorization: Bearer` header. The web UI reads the permissions of the user from `GET /auth/me` and hides buttons the user cannot use. It fits phone screens down to 390px.
- **Empty lists:** List endpoints answer `[]` when there is nothing to list, never `null`.
- **Offset commit:** Source offsets are committed only after the target produce is acknowledged. A failed produce is retried, and the offset is never committed past a record that was not copied. See [Resilience and buffering](resilience.md).
- **Secrets:** Cluster and config APIs return `***` for credentials. PUT with `***` or an empty secret leaves the stored value unchanged.
- **Replication halt (tech preview):** Off by default and admin-only. Freezes all jobs if the source cluster is compromised so the replica does not keep copying the attack. See [docs/protection.md](protection.md).
