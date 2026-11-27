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
	"bytes"
	"context"
	"github.com/scalytics/kaf-mirror/v2/internal/kafka"
	"github.com/scalytics/kaf-mirror/v2/pkg/logger"
	"github.com/scalytics/kaf-mirror/v2/tests/mocks"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRecordSizeAnalysisDoesNotLogPerRecordAtInfo(t *testing.T) {
	var out bytes.Buffer
	logger.SetOutput(&out)
	logger.SetLevel(logger.INFO)
	t.Cleanup(func() {
		logger.SetOutput(io.Discard)
		logger.SetLevel(logger.INFO)
	})

	km := kafka.NewKafMirrorImplForTest(&kafka.Producer{Client: &mocks.MockKgoClient{
		ProduceFunc: func(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) { f(r, nil) },
	}}, map[string]string{"orders": "orders", "events": "events"})

	small := []byte("small-payload")
	large := bytes.Repeat([]byte("x"), 6000)
	for offset := int64(0); offset < 300; offset++ {
		require.NoError(t, km.HandleRecordForTest(&kgo.Record{Topic: "orders", Offset: offset, Key: []byte("k"), Value: small}))
		require.NoError(t, km.HandleRecordForTest(&kgo.Record{Topic: "events", Offset: offset, Value: large}))
	}

	var info []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "INFO") && strings.Contains(line, "[AI:") {
			info = append(info, line)
		}
	}
	assert.Len(t, info, 1, "INFO lines: %v", info)
	assert.NotContains(t, out.String(), "Message analysis")
	if len(info) == 1 {
		assert.Contains(t, info[0], "Topic events has records larger than 2 KB")
	}
}
