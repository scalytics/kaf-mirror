# Back up to S3 with KafScale

A replica copies deletes, bad writes and anything done with stolen producer
credentials. For a backup, mirror into a separate target on its own credentials.
[KafScale](https://github.com/KafScale/platform) is one option: a
Kafka-compatible broker that stores immutable segments in S3. Any other
Kafka-compatible target is set up the same way. More on the pattern:
[KafShield, Kafka backup and disaster recovery on S3](https://www.scalytics.io/kafshield).

Prerequisites: Go, Docker (for the local MinIO bucket), `make`, the Apache Kafka
CLI (`kafka-topics.sh` on your `PATH`), and a reachable source Kafka cluster.

**1. Start KafScale with a local S3 bucket (MinIO)**

```bash
git clone https://github.com/KafScale/platform.git && cd platform
make demo
```

The broker listens on `127.0.0.1:39092`. Segments land in the MinIO bucket
`kafscale`. Leave this terminal running.

Before adding the target cluster to kaf-mirror, open another terminal and
pre-create **every target topic** with the same partition count as its source.
KafScale v2.1.0's `make demo` forces auto-create on with one partition, ignoring
`KAFSCALE_AUTO_CREATE_TOPICS=false` from the environment. If a missing target is
first discovered through Metadata, it is created with one partition and a
multi-partition source fails the job's partition-count check
([KafScale issue 446](https://github.com/KafScale/platform/issues/446)).

For example, inspect the source topic `orders` (replace the source address and
add `--command-config /path/to/client.properties` if it needs authentication):

```bash
kafka-topics.sh --bootstrap-server source-kafka:9092 --describe --topic orders
```

If the output says `PartitionCount: 3`, create the target like this:

```bash
kafka-topics.sh --bootstrap-server 127.0.0.1:39092 --create --topic orders \
  --partitions 3 --replication-factor 1
kafka-topics.sh --bootstrap-server 127.0.0.1:39092 --describe --topic orders
```

Repeat for each topic you will mirror, using its source partition count and its
mapped target name if you rename topics. Confirm each target's `PartitionCount`
matches before continuing. If an earlier attempt already auto-created a target
with too few partitions, increase it to the source count with `kafka-topics.sh
--bootstrap-server 127.0.0.1:39092 --alter --topic orders --partitions 3` (adjust
the name and count). Pre-create newly added target topics before adding them to
the job as well. Pre-create every target topic on KafScale v2.1.0 regardless of
`KAFSCALE_AUTO_CREATE_TOPICS`. A fresh broker also seeds a one-partition `orders`
entry that can appear in named Metadata requests but not in the full topic list.
Do not rely on kaf-mirror creating `orders` on a fresh broker: explicitly creating
the topic with the source partition count avoids this startup failure.

**2. Build and start kaf-mirror**

In a second terminal, from this repo:

```bash
mkdir -p bin
go build -o bin/kaf-mirror ./cmd/kaf-mirror
go build -o bin/mirror-cli ./cmd/mirror-cli
./bin/kaf-mirror
```

kaf-mirror with idempotent writes on, the default, is verified to mirror into and
out of [KafScale v2.1.0](https://github.com/KafScale/platform/releases/tag/v2.1.0)
and later. Set `KAFMIRROR_DISABLE_IDEMPOTENT_WRITES=true` only for KafScale before v2.1.0.

On the first start kaf-mirror creates `admin@localhost`
with a random password and writes it to `data/kaf-mirror-bootstrap-<random>`,
next to the database. The path is printed on stdout as `Password File`. In a
third terminal, read the password, then delete the file:

```bash
cat data/kaf-mirror-bootstrap-*
rm data/kaf-mirror-bootstrap-*
```

**3. Add the clusters and start a job**

```bash
./bin/mirror-cli login admin@localhost   # asks for the password
./bin/mirror-cli clusters add   # your source cluster
./bin/mirror-cli clusters add   # target: brokers 127.0.0.1:39092
./bin/mirror-cli jobs add       # pick source, target and topics
./bin/mirror-cli jobs start
./bin/mirror-cli jobs status full
```

`clusters add` and `jobs add` are interactive. Source offsets are committed only
after KafScale acknowledges the write, so a stopped job resumes where it left
off. For production, run KafScale from its Helm chart against a real S3 bucket
and consider [replication halt](protection.md), which freezes jobs if the
source looks compromised.
