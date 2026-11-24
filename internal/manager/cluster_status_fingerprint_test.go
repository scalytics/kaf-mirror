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

package manager

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJoinOrStartClusterFlightFailsClosedWithoutFingerprint(t *testing.T) {
	jm := &JobManager{}
	first, joined, gen1 := jm.joinOrStartClusterFlight("c", "", false)
	require.False(t, joined)
	require.NotNil(t, first)
	second, joined, gen2 := jm.joinOrStartClusterFlight("c", "", false)
	require.False(t, joined)
	require.NotSame(t, first, second)
	require.NotEqual(t, gen1, gen2)

	third, joined, gen3 := jm.joinOrStartClusterFlight("c", "same", true)
	require.False(t, joined)
	fourth, joined, gen4 := jm.joinOrStartClusterFlight("c", "same", true)
	require.True(t, joined)
	require.Same(t, third, fourth)
	require.Equal(t, gen3, gen4)
	require.NotEqual(t, gen2, gen3)
}

func TestJoinOrStartClusterFlightDoesNotJoinMismatchedFingerprint(t *testing.T) {
	jm := &JobManager{}
	first, joined, gen1 := jm.joinOrStartClusterFlight("c", "a", true)
	require.False(t, joined)
	second, joined, gen2 := jm.joinOrStartClusterFlight("c", "b", true)
	require.False(t, joined)
	require.NotSame(t, first, second)
	require.NotEqual(t, gen1, gen2)
}
