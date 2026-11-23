// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafka

import (
	"kaf-mirror/pkg/logger"

	"github.com/twmb/franz-go/pkg/kgo"
)

type sourcePartitioner struct {
	fallback kgo.Partitioner
}

type sourceTopicPartitioner struct {
	topic    string
	fallback kgo.TopicPartitioner
	warned   map[int32]bool
}

func NewSourcePartitioner() kgo.Partitioner {
	return &sourcePartitioner{fallback: kgo.StickyKeyPartitioner(nil)}
}

func (p *sourcePartitioner) ForTopic(topic string) kgo.TopicPartitioner {
	return &sourceTopicPartitioner{
		topic:    topic,
		fallback: p.fallback.ForTopic(topic),
		warned:   make(map[int32]bool),
	}
}

func (*sourceTopicPartitioner) RequiresConsistency(*kgo.Record) bool { return true }

func (p *sourceTopicPartitioner) Partition(r *kgo.Record, n int) int {
	if r.Partition >= 0 && int(r.Partition) < n {
		return int(r.Partition)
	}
	if !p.warned[r.Partition] {
		p.warned[r.Partition] = true
		logger.Warn("Source partition %d does not exist on target topic %s with %d partitions; records from it are placed by the Kafka default partitioner instead", r.Partition, p.topic, n)
	}
	return p.fallback.Partition(r, n)
}

func (p *sourceTopicPartitioner) OnNewBatch() {
	if b, ok := p.fallback.(kgo.TopicPartitionerOnNewBatch); ok {
		b.OnNewBatch()
	}
}
