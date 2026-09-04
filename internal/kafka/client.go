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

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// KgoClient is an interface that abstracts the methods we use from kgo.Client.
// This allows us to mock the client in tests.
type KgoClient interface {
	Request(context.Context, kmsg.Request) (kmsg.Response, error)
	PollFetches(context.Context) kgo.Fetches
	Produce(context.Context, *kgo.Record, func(*kgo.Record, error))
	CommitRecords(context.Context, ...*kgo.Record) error
	AddConsumeTopics(...string)
	Close()
}

// Ensure kgo.Client satisfies our interface.
var _ KgoClient = (*kgo.Client)(nil)
