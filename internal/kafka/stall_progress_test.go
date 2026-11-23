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

import "testing"

func TestStallProgressRequiresLagAndSixIdleTicks(t *testing.T) {
	var s stallProgress
	for i := 0; i < 6; i++ {
		if s.stalled(0, 0, 10) {
			t.Fatalf("stalled at tick %d", i+1)
		}
	}
	if !s.stalled(0, 0, 10) {
		t.Fatal("expected stall on the 7th idle tick with lag")
	}
	if s.stalled(0, 0, 0) {
		t.Fatal("idle topic with no lag must not stay stalled")
	}

	s = stallProgress{}
	for i := 0; i < 8; i++ {
		if s.stalled(0, 0, 0) {
			t.Fatalf("idle with no lag stalled at tick %d", i+1)
		}
	}

	s = stallProgress{}
	for i := 1; i <= 8; i++ {
		if s.stalled(int64(i), int64(i), 10) {
			t.Fatalf("progress with lag stalled at tick %d", i)
		}
	}

	s = stallProgress{}
	if s.stalled(20, 20, 10) {
		t.Fatal("first sample must not stall")
	}
	for i := 0; i < 6; i++ {
		if s.stalled(20, 20, 10) {
			t.Fatalf("stalled at idle tick %d", i+2)
		}
	}
	if !s.stalled(20, 20, 10) {
		t.Fatal("expected stall after 7 further idle ticks")
	}
	if s.stalled(25, 20, 10) {
		t.Fatal("consumed progress must clear the stall")
	}

	s = stallProgress{}
	_ = s.stalled(50, 50, 10)
	if s.stalled(0, 0, 10) {
		t.Fatal("a counter reset must not stall")
	}
	for i := 0; i < 6; i++ {
		if s.stalled(0, 0, 10) {
			t.Fatalf("reset then idle stalled at tick %d", i+2)
		}
	}
	if !s.stalled(0, 0, 10) {
		t.Fatal("expected stall after a reset and 7 idle ticks with lag")
	}
}
