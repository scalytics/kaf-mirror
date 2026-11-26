# Job metrics

Every running job reports its counters every 10 seconds. Each report holds the counts since this run of the job started. kaf-mirror stores only the growth since the last report of the same run, so the totals of a job do not grow after a job restart or a kaf-mirror restart when no records come in.

*   `messages_replicated` and `bytes_transferred` (key plus value bytes) count each source record once, when the target first acks it. A record that is written again during a retry is not counted again.
*   `messages_consumed` and `bytes_consumed` count each record when its source offset is committed.
*   `error_count` counts failed produce attempts, so a record that fails twice counts 2, plus source fetch errors, one for each partition in each fetch that returns an error, and one for each check that finds a source topic missing.
*   `tombstone_count` counts committed records with an empty value.
*   `current_lag` is the lag of the job consumer group at the last report, computed the same way as `GET /api/v1/jobs/{id}/lag` and summed over all mapped source topics. It keeps growing while the target is down. If the source offsets cannot be read within 5 seconds, the report uses the lag from the last fetch instead.

`GET /api/v1/jobs/{id}/metrics/current` returns the totals since the job was created and `GET /api/v1/jobs/{id}/metrics/history` returns the stored reports, with `tombstone_count_delta` for each. `GET /api/v1/jobs/{id}/lag` reads the lag of the job consumer group `kaf-mirror-job-<id>` from the source cluster for each mapped topic: for every partition, the end offset minus the committed offset. A partition without a commit, or with a commit below the log start (for example after `DeleteRecords`), counts from its log start offset, and a partition never counts below 0. It also works for a stopped job. With `$KM`, `$TOKEN` and the job ID in `$JOB` as in your setup:

```bash
curl -s "$KM/jobs/$JOB/metrics/current" -H "Authorization: Bearer $TOKEN" | jq '{messages_replicated, bytes_transferred, error_count, tombstone_count, current_lag}'
curl -s "$KM/jobs/$JOB/lag" -H "Authorization: Bearer $TOKEN" | jq .
```

kaf-mirror does not rotate its log file. Use `logrotate` or the log tooling of your platform, see `logging.file` in [docs/configuration.md](configuration.md). The size of copied records is logged only at `DEBUG`. At `INFO`, a job logs one line per source topic when that topic has a record larger than 2 KB, as a compression hint.

When new produce or fetch errors come in, the job logs `Error spike detected` once. It logs it again only after a report with no new errors, followed by new errors.

`GET /api/v1/jobs/{id}/metrics/current` also returns the incident flags `source_stalled`, `target_stalled`, `critical_lag`, `high_error_rate` and `error_spike`. They come from the last report of the running job, so they match the `Source stalled`, `Critical lag detected` and `Error spike detected` log lines and the Prometheus gauges. A job that is not running, or has not sent a report since it started, returns them as `false`. They are not stored in SQLite, so `GET /api/v1/jobs/{id}/metrics/history` does not have them:
```bash
curl -s "$KM/jobs/$JOB/metrics/current" -H "Authorization: Bearer $TOKEN" | jq '{source_stalled, target_stalled, critical_lag, high_error_rate, error_spike}'
```

`source_stalled` is set when the consumer group lag is above 0 and neither `messages_replicated` nor `messages_consumed` grows for about 70 seconds (seven 10-second reports). An idle topic with lag 0 does not stall. After a report with progress, the window starts again. `target_stalled` stays false: after batching, a record is counted as consumed only when the target has acked it and the source offset is committed, so a stuck target looks like no progress with lag and is reported as `source_stalled`. The lag here is the consumer group lag, so it grows while the target is down even though the job does not fetch new records. The Prometheus gauges `kaf_mirror_incident_source_stalled` and `kaf_mirror_incident_target_stalled` still exist, and the target gauge stays 0.

At job start kaf-mirror reads the topic configs of both clusters and logs the `compression.type` of each source and target topic, and warns when they differ. The inventory snapshot stores the same configs. If the configs cannot be read, for example because the broker does not support `DescribeConfigs`, kaf-mirror logs a warning and reports the compression as `NONE`.
