# Architecture context

kaf-mirror addresses one of the hardest Kafka operational challenges: **cross-cluster replication with disaster recovery, observability, and governed failover**. This is section 3.4 of my Kafka architecture guide, where most production deployments eventually fail.

**Full architecture guide:** [Apache Kafka® Architecture Leadership](https://www.novatechflow.com/p/kafka-architecture-leadership.html)

## The Problem

From the guide:

> "Kafka replication across regions requires: replication topology, latency budgets, conflict resolution, failover procedures."

Most teams discover these requirements during an incident, not before. MirrorMaker works but lacks:

- Unified visibility across replication jobs
- Recovery tooling when things break
- Anomaly detection before lag becomes critical
- Audit trails for compliance

## What kaf-mirror Provides
```
Source Cluster                       Target Cluster
      │                                    ▲
      │ consume (franz-go)                 │ produce (idempotent)
      ▼                                    │
┌──────────────────────────────────────────┴───────┐
│                    kaf-mirror                    │
├──────────────────────────────────────────────────┤
│  Topic mapping (default mirror + regex patterns) │
│  Job lifecycle (start / stop / pause)            │
│  Consumer lag as architectural signal            │
│  AI anomaly detection + tuning recommendations   │
│  Real-time dashboard + WebSocket updates         │
│  Compliance reporting + audit trails             │
│  Role-based security (admin/operator/monitor)    │
└──────────────────────────────────────────────────┘
```

## Resilience Model

The guide emphasizes treating lag as an architectural signal, not a metric to hide. kaf-mirror implements this:

> "If the target cluster is unavailable, the consumer stops reading and its offset does not advance. The source Kafka cluster itself acts as the durable buffer. When restored, replication resumes from the last committed offset."

No separate disk buffer. Kafka's retention *is* the buffer. This simplifies operations and supports at-least-once delivery with an idempotent producer (duplicates reduced, not transactional exactly-once across clusters). With idempotent writes off, needed only for KafScale before v2.1.0, client retries can add duplicates.

## Key Capabilities

| Capability | Addresses |
|------------|-----------|
| franz-go engine | High-perf, idempotent producer (section 2.2) |
| Topic governance | Mapping rules, naming conventions (section 2.1) |
| Lag monitoring | Backpressure as signal (section 3.2) |
| Multi-cluster auth | TLS, SASL PLAIN, SCRAM and Kerberos (see [security.md](security.md)) |
| Audit trails | Compliance and ownership (section 1.5) |
| AI insights | Anomaly detection before incidents |

## Where It Fits
```
Primary DC                    DR Site / Analytics Region
    │                                │
    ▼                                ▼
┌────────┐   kaf-mirror    ┌────────┐
│ Kafka  │ ──────────────► │ Kafka  │ → Flink → Iceberg
│ Cluster│                 │ Cluster│
└────────┘                 └────────┘
```

---
