# Replication halt (experimental)

An admin kill-switch for kaf-mirror. **Off by default.**

If the source cluster is compromised, kaf-mirror will copy that traffic onto the replica unless you stop it. This feature is how you freeze replication. It is experimental: it can false-positive, and it does not protect Kafka itself.

## Why this exists

In a live incident the **source** cluster was ransomed. kaf-mirror kept running and replicated the attack. Data **already** stored by kafscale on S3 was not encrypted, because those objects are read-only. New writes from the mirror would still have landed as new (bad) objects.

This switch stops **new** replication. It does not decrypt the source, and it does not roll back the replica.

## Turn it on

You must be an admin (`protection:manage`). From the CLI:

```bash
mirror-cli login
mirror-cli protection enable
mirror-cli protection status
mirror-cli protection halt "source compromised"
mirror-cli protection resume     # does not restart jobs
mirror-cli protection disable
```

A non-admin token gets 403. Same operations over the API: `POST /api/v1/protection/enable` (and `disable`, `halt`, `resume`). Full command list: [cli-commands.md](cli-commands.md).

### API

Base path `/api/v1`. Auth: `Authorization: Bearer <token>`.

| Method | Path | Who | What |
|---|---|---|---|
| GET | `/protection` | any logged-in user | Status (enabled, halted, reason) |
| POST | `/protection/enable` | admin (`protection:manage`) | Turn the feature on |
| POST | `/protection/disable` | admin | Turn it off and clear an API halt |
| POST | `/protection/halt` | admin | Pause every job. Body: `{"reason":"..."}` |
| POST | `/protection/resume` | admin | Clear an API halt. Does not restart jobs. Does not clear `data/HALT` or `KAF_MIRROR_HALT`. |

Cluster create/update JSON includes `role`: `prod`, `dr`, or `other`.

Swagger: `/docu/swagger/` (tag **protection**).

To arm it at process start, set in config:

```yaml
protection:
  enabled: true
  halt_file: "data/HALT"
  halt_env: "KAF_MIRROR_HALT"
  auto_halt:
    enabled: true
```

Until this is on: no halt file, no env, no auto-stop, and jobs into `prod` clusters are allowed as usual.

## Label clusters

When you add a cluster, set `role`:

| Role | Meaning |
|---|---|
| `prod` | Production. While protection is on, kaf-mirror will **not** start a job that writes **into** this cluster. |
| `dr` | Replica / DR (including a kafscale/S3-backed cluster). |
| `other` | Default. No extra rule. |

Mark the live source `prod` and the replica `dr`. The CLI asks for this on add and edit:

```bash
mirror-cli clusters add
mirror-cli clusters edit
```

## Stop all replication (halt)

Any of these pause **every running job**. Jobs stay paused until an admin starts them again.

| How | Command |
|---|---|
| CLI | `mirror-cli protection halt "source compromised"` |
| API | `POST /api/v1/protection/halt` with `{"reason":"source compromised"}` |
| File | create `data/HALT` (path is `protection.halt_file`) |
| Env | `KAF_MIRROR_HALT=1` (name is `protection.halt_env`) |
| Auto | traffic looks like encryption or a delete storm (see below) |

Use the **file or env** if the API is down. The dashboard shows a banner when halt is active.

## Resume

Halt from the CLI/API is cleared with:

```bash
mirror-cli protection resume
```

That does **not** clear a halt file or `KAF_MIRROR_HALT`. Remove the file and unset the env or replication stays stopped.

Resume does **not** restart jobs. When you trust the source:

```bash
mirror-cli jobs start <job-id>
```

## Automatic halt

Automatic halt runs only when protection is on and `protection.auto_halt.enabled` is `true`. The bundled `configs/default.yml` sets it to `true`. The Helm chart does not set it, so it is off there. There are no per-detector toggles.

kaf-mirror samples records and will halt if:

- Many **existing keys** get new **high-entropy** values (looks like encrypt-in-place on the source), or a large volume of high-entropy payloads when keys are unique
- A flood of **empty values** (tombstones / deletes)
- A burst of produce/auth errors
- Payloads get much larger while source lag suddenly drops

The counter checks (empty values, errors, payload size) look at the reports of the last 60 seconds of one job. Reports of different jobs are never compared with each other, and when a job restarts its counters start a new window.

These are heuristics. They can fire on a legitimate bulk rewrite. If you already know the source is bad, halt yourself; do not wait for auto.

Payload inspection runs before a record is submitted to the target. The record that triggers a halt is rejected, and every subsequent send, including a retry, checks protection again. Its source offset is not committed. Writes already submitted to the target cannot be withdrawn. That check reads an in-memory snapshot. Halt, resume, enable, and disable update the snapshot before they return. A poll every 200ms refreshes it from the database, the halt file, and the halt variable, so a file or variable change can take that long to block a send. A failed refresh blocks sends until a later refresh succeeds. If saving a halt fails, the running process keeps the halt in memory until a successful resume; repair storage before restarting the process, because that unsaved halt cannot survive a restart.

Automatic halt cannot see ransomware that only encrypts **S3 objects** under kafscale (for example SSE-C). That is outside the Kafka consume path. Already-written S3 objects staying read-only is still what saved data in the incident.

## What this does not do

- Make Kafka or the replica ransomware-proof
- Delay replication by N minutes
- Prove source and target have the same records (offsets are per cluster). `validate-mirror` uses **source consumer-group lag** and whether topics exist.
- Unlock or restore encrypted source data
