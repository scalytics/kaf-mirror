# What a copied record keeps

kaf-mirror copies every record field a producer can set:

*   **Key and value:** copied as is, including null keys and tombstones (null values).
*   **Headers:** all headers, in their original order.
*   **Partition:** a record from source partition N is written to partition N of the target topic. See "Partition counts" below for a target with fewer partitions.
*   **Timestamp:** the source record timestamp becomes the target record timestamp. If the source topic uses `message.timestamp.type=LogAppendTime`, that is the time the source broker appended the record. A record without a source timestamp gets the time of the copy.

Offsets are not kept. The target broker assigns its own offsets.

Because the target keeps the source timestamp, time-based retention on the target counts from the source time. A record older than the target `retention.ms` is deleted from the target soon after the copy, the same way it would be on the source.

The target topic keeps the source timestamp only with `message.timestamp.type=CreateTime`, which is the Kafka default. With `LogAppendTime` the target broker replaces it with its own append time. The target broker also rejects records whose timestamp is outside its `message.timestamp.before.max.ms` and `message.timestamp.after.max.ms` limits, so check these before you mirror old data into a topic with tight limits.

**Partition counts.** When a job starts, every source topic and its target topic must have the same number of partitions, otherwise the job does not start. Two cases can still leave the target with fewer partitions while a job runs: partitions are added to the source topic, or topic discovery maps a new source topic to a target topic that already exists with fewer partitions. Records from a source partition the target does not have are then placed the way the Kafka default partitioner does it: records with a key go to the partition of the key hash (murmur2), and records without a key stay on one target partition until the producer starts a new batch. kaf-mirror logs one warning per source partition when this happens. To go back to one-to-one placement, add the same number of partitions to the target topic. The producer picks them up on its next metadata refresh, within about five minutes.
