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

package mocks

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// MockKgoClient is a mock implementation of the KgoClient interface.
type MockKgoClient struct {
	RequestFunc          func(context.Context, kmsg.Request) (kmsg.Response, error)
	PollFetchesFunc      func(context.Context) kgo.Fetches
	ProduceFunc          func(context.Context, *kgo.Record, func(*kgo.Record, error))
	CommitRecordsFunc    func(context.Context, ...*kgo.Record) error
	AddConsumeTopicsFunc func(...string)
	CloseFunc            func()
}

func (m *MockKgoClient) Request(ctx context.Context, req kmsg.Request) (kmsg.Response, error) {
	if m.RequestFunc != nil {
		return m.RequestFunc(ctx, req)
	}
	return nil, nil
}

func (m *MockKgoClient) PollFetches(ctx context.Context) kgo.Fetches {
	if m.PollFetchesFunc != nil {
		return m.PollFetchesFunc(ctx)
	}
	return kgo.Fetches{}
}

func (m *MockKgoClient) Produce(ctx context.Context, r *kgo.Record, f func(*kgo.Record, error)) {
	if m.ProduceFunc != nil {
		m.ProduceFunc(ctx, r, f)
	}
}

func (m *MockKgoClient) CommitRecords(ctx context.Context, rs ...*kgo.Record) error {
	if m.CommitRecordsFunc != nil {
		return m.CommitRecordsFunc(ctx, rs...)
	}
	return nil
}

func (m *MockKgoClient) AddConsumeTopics(topics ...string) {
	if m.AddConsumeTopicsFunc != nil {
		m.AddConsumeTopicsFunc(topics...)
	}
}

func (m *MockKgoClient) Close() {
	if m.CloseFunc != nil {
		m.CloseFunc()
	}
}
