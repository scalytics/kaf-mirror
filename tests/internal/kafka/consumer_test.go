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

package kafka_test

import (
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsumerMetricsLagIncludesUnconsumedPartitions(t *testing.T) {
	consumer := kafka.NewConsumerForTest(
		map[string]map[int32]int64{
			"orders": {
				0: 10,
				1: 5,
			},
		},
		map[string]map[int32]int64{
			"orders": {
				0: 7,
			},
		},
	)

	metrics := consumer.GetMetrics()
	assert.Equal(t, int64(7), metrics.ConsumerLag)
}

func TestConsumerMetricsLagClampsNegative(t *testing.T) {
	consumer := kafka.NewConsumerForTest(
		map[string]map[int32]int64{
			"orders": {
				0: 10,
			},
		},
		map[string]map[int32]int64{
			"orders": {
				0: 10,
			},
		},
	)

	metrics := consumer.GetMetrics()
	assert.Equal(t, int64(0), metrics.ConsumerLag)
}
