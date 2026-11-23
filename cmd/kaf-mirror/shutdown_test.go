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

package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWaitAndShutdownRunsEveryStepAtOnceWithOneDeadline(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	var both sync.WaitGroup
	both.Add(2)
	deadlines := make(chan time.Time, 2)
	step := func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("step context has no deadline")
		}
		deadlines <- deadline
		both.Done()
		both.Wait()
		return nil
	}

	if err := waitAndShutdown(signals, time.Minute, step, step); err != nil {
		t.Fatal(err)
	}
	first, second := <-deadlines, <-deadlines
	if !first.Equal(second) {
		t.Fatalf("steps got different deadlines %s and %s", first, second)
	}
}

func TestWaitAndShutdownWaitsForTheSignal(t *testing.T) {
	signals := make(chan os.Signal)
	ran := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- waitAndShutdown(signals, time.Minute, func(context.Context) error {
			ran <- struct{}{}
			return nil
		})
	}()
	select {
	case <-ran:
		t.Fatal("a step ran before the signal")
	default:
	}
	signals <- syscall.SIGINT
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 {
		t.Fatal("the step did not run after the signal")
	}
}

func TestWaitAndShutdownReturnsStepErrors(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	err := waitAndShutdown(signals, time.Minute,
		func(context.Context) error { return errors.New("server failed") },
		func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitAndShutdownIsBoundedByTheTimeout(t *testing.T) {
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	stuck := make(chan struct{})
	defer close(stuck)

	err := waitAndShutdown(signals, time.Millisecond, func(context.Context) error {
		<-stuck
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "shutdown did not finish within 1ms") {
		t.Fatalf("got %v", err)
	}
}
