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
	"context"
	"fmt"
	"time"
)

type MirrorVerifyResult struct {
	JobConsumerGroup   string           `json:"job_consumer_group"`
	SourceConsumerLag  int64            `json:"source_consumer_lag"`
	LagByTopic         map[string]int64 `json:"lag_by_topic"`
	Topics             []TopicVerify    `json:"topics"`
	Warnings           []string         `json:"warnings"`
	Critical           []string         `json:"critical"`
	Method             string           `json:"method"`
	ComparedAt         time.Time        `json:"compared_at"`
	OffsetIdentityNote string           `json:"offset_identity_note"`
}

type TopicVerify struct {
	Source           string `json:"source"`
	Target           string `json:"target"`
	SourceExists     bool   `json:"source_exists"`
	TargetExists     bool   `json:"target_exists"`
	SourcePartitions int32  `json:"source_partitions"`
	TargetPartitions int32  `json:"target_partitions"`
	Regex            bool   `json:"regex"`
}

func VerifyMirror(ctx context.Context, source, target *AdminClient, topicMap map[string]string, consumerGroup string, concreteSources []string) (*MirrorVerifyResult, error) {
	result := &MirrorVerifyResult{
		JobConsumerGroup:   consumerGroup,
		LagByTopic:         map[string]int64{},
		Method:             "source_consumer_lag+topic_presence",
		ComparedAt:         time.Now().UTC(),
		OffsetIdentityNote: "Kafka offsets are cluster-local. Source vs target high watermarks are not comparable and are not used as a gap signal.",
	}

	sourceTopics, err := source.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list source topics: %w", err)
	}
	targetTopics, err := target.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list target topics: %w", err)
	}

	for src, dst := range topicMap {
		tv := TopicVerify{Source: src, Target: dst, Regex: IsTopicRegex(src)}
		if tv.Regex {
			result.Warnings = append(result.Warnings, fmt.Sprintf("mapping %s is a pattern; skipped name-level health", src))
			result.Topics = append(result.Topics, tv)
			continue
		}
		if info, ok := sourceTopics[src]; ok {
			tv.SourceExists = true
			tv.SourcePartitions = info.Partitions
		} else {
			result.Critical = append(result.Critical, fmt.Sprintf("source topic %s does not exist", src))
		}
		if info, ok := targetTopics[dst]; ok {
			tv.TargetExists = true
			tv.TargetPartitions = info.Partitions
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("target topic %s does not exist yet", dst))
		}
		if tv.SourceExists && tv.TargetExists && tv.SourcePartitions != tv.TargetPartitions {
			result.Warnings = append(result.Warnings, fmt.Sprintf("partition count differs for %s (%d) vs %s (%d)", src, tv.SourcePartitions, dst, tv.TargetPartitions))
		}
		result.Topics = append(result.Topics, tv)
	}

	if len(concreteSources) == 0 {
		for src := range topicMap {
			if !IsTopicRegex(src) {
				concreteSources = append(concreteSources, src)
			}
		}
	}
	if len(concreteSources) == 0 {
		return result, nil
	}

	hwms, err := source.GetTopicHighWaterMarks(ctx, concreteSources)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not read source end offsets: %v", err))
		return result, nil
	}
	committed, err := source.GetConsumerGroupOffsets(ctx, consumerGroup, concreteSources)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not read source consumer group offsets: %v", err))
		return result, nil
	}

	for topic, parts := range hwms {
		committedParts := map[int32]int64{}
		for _, c := range committed[topic] {
			committedParts[c.Partition] = c.Offset
		}
		var topicLag int64
		for _, p := range parts {
			at, ok := committedParts[p.Partition]
			if !ok {
				at = 0
			}
			lag := p.HighWaterMark - at
			if lag < 0 {
				lag = 0
			}
			topicLag += lag
		}
		result.LagByTopic[topic] = topicLag
		result.SourceConsumerLag += topicLag
	}
	return result, nil
}
