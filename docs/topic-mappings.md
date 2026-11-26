# Topic mappings

Each job has a list of mappings with a `source_topic_pattern`, a `target_topic_pattern` and `enabled`. kaf-mirror decides with one rule whether a source pattern is an exact topic name or a regex. The same rule is used when a job starts, by topic discovery, when a record is routed, by the mirror state, verify, lag and topic health views, and when mappings are saved.

*   **Exact name:** a pattern without any of the characters `\ ^ $ * + ? ( ) [ ] { } |` is an exact topic name. A dot does not make a pattern a regex, so `pay.v1` copies only `pay.v1`, not `pay.v1.dlq`, `my-pay.v1` or `payXv1`.
*   **Regex:** a pattern with at least one of those characters is a Go regex. It must match the whole topic name, as if it were written `^(?:pattern)$`. `app-.*` matches `app-a` and `app-b`, but not `myapp-a` or `xapp-a`. A pattern you already anchored, such as `^app-.*$`, still matches the same names. In a regex, a dot matches any character, so write `ev\..*` to match only topics that start with `ev.`.
*   **Target:** for an exact name, an empty target means the same name on the target cluster. For a regex, an empty target means `$0`, the whole source topic name, so every matched topic keeps its name. A regex target can use `$0` and capture groups: `orders-(.*)` with target `backup-$1` copies `orders-eu` to `backup-eu`.
*   **Conflicts:** if an exact mapping and a regex match the same topic, the exact mapping wins. A job does not start, and topic discovery fails the job, when two regex mappings send one topic to different targets, when two source topics would go to the same target through a regex, or when a regex gives an empty target name.
*   **Invalid regex:** creating a job or saving mappings with a regex that does not compile, for example `app-(`, is rejected with HTTP 400.
*   **Target topics:** a missing target topic is created with the partition count of the source topic. A topic that someone else creates at the same time counts as created. After a create, kaf-mirror waits until the topic is visible in metadata, so a job does not fail to start when a multi-broker target has not yet refreshed. A job start then reads the target metadata up to 5 times, waiting 1s, 2s, 3s and 4s between the reads, because each read can go to a broker that has not seen the new topic yet. If the topic is still missing after the fifth read, the job does not start and `failed_reason` says `target topic <name> does not exist after creation attempt`. If the topic already exists with a different partition count, the job does not start.
*   **Discovery:** jobs with a regex mapping look for new matching source topics every `replication.topic_discovery_interval` from the config file (default `5m`). The value applies to every job and is read when a job starts. A failed run is retried at the next interval.

| Source pattern | Target pattern | Source topics | Copied as |
|---|---|---|---|
| `pay.v1` | empty | `pay.v1`, `pay.v1.dlq`, `payXv1` | `pay.v1` to `pay.v1` |
| `a.b` | `a.b.copy` | `a.b`, `a.b.c`, `my-a.b`, `aXb` | `a.b` to `a.b.copy` |
| `app-.*` | empty | `app-a`, `app-b`, `myapp-c` | `app-a` to `app-a`, `app-b` to `app-b` |
| `app-.*` | `mirror-$0` | `app-a`, `xapp-a` | `app-a` to `mirror-app-a` |
| `orders-(.*)` | `backup-$1` | `orders-eu`, `orders-us` | `orders-eu` to `backup-eu`, `orders-us` to `backup-us` |

A job with a regex mapping, created with the API (`$KM`, `$TOKEN` and the cluster names as in your setup):
```bash
curl -s -X POST "$KM/jobs/" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"apps","source_cluster_name":"src","target_cluster_name":"tgt","batch_size":1000,"topic_mappings":[{"source_topic_pattern":"app-.*","target_topic_pattern":"","enabled":true}]}'
```

`batch_size` is in KB. A job created without `batch_size`, or with `0`, gets the default of 1000. A value below 0 or above 102400 (100 MB, the largest request the Kafka client sends) is rejected with HTTP 400. An update without `batch_size` keeps the stored value. A job that was stored with `batch_size` 0 by an older version starts with 1000, and its next update stores 1000.
