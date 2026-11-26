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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestRecordGuardRejectsBeforeProduceIncludingRetries(t *testing.T) {
	for _, attempt := range []int{1, 2} {
		t.Run(string(rune('0'+attempt)), func(t *testing.T) {
			r := NewKafMirrorImplForTest(nil, map[string]string{"source": "target"})
			hooks, checks, callbacks := 0, 0, 0
			r.SetRecordHook(func(string, []byte, []byte) { hooks++ })
			blocked := errors.New("replication halted: source encryption")
			r.SetRecordGuard(func() error { checks++; return blocked })
			r.handleRecord(context.Background(), &kgo.Record{Topic: "source"}, attempt, func(err error) {
				callbacks++
				require.ErrorIs(t, err, blocked)
			})
			require.Equal(t, 1, checks)
			require.Equal(t, 1, callbacks)
			if attempt == 1 {
				require.Equal(t, 1, hooks)
			} else {
				require.Zero(t, hooks, "retries must not count the payload twice")
			}
		})
	}
}

func TestCancelledRecordCannotProduceAfterInspection(t *testing.T) {
	r := NewKafMirrorImplForTest(nil, map[string]string{"source": "target"})
	ctx, cancel := context.WithCancel(context.Background())
	r.SetRecordHook(func(string, []byte, []byte) { cancel() })
	called := false
	r.handleRecord(ctx, &kgo.Record{Topic: "source"}, 1, func(err error) {
		called = true
		require.ErrorIs(t, err, context.Canceled)
	})
	require.True(t, called)
}
