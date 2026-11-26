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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareRevokeWaitTracksAssignments(t *testing.T) {
	c := &Consumer{}
	// Watching/fetching a topic does not mean that this member owns partitions.
	c.watchTopics("kin")
	c.AssignPartitions(map[string][]int32{"other": {0}, "kin": {}})
	revoked := c.prepareRevokeWait("kin")
	select {
	case <-revoked:
	default:
		t.Fatal("a member without partitions must not wait for a revoke")
	}
	require.Nil(t, c.topicIDs.revokeWait)

	c.AssignPartitions(map[string][]int32{"kin": {0, 1}})
	revoked = c.prepareRevokeWait("kin")
	select {
	case <-revoked:
		t.Fatal("an owner must wait for its topic to be revoked")
	default:
	}
	c.RevokePartitions(map[string][]int32{"other": {0}})
	require.Equal(t, revoked, c.topicIDs.revokeWait)
	c.RevokePartitions(map[string][]int32{"kin": {0, 1}})
	<-revoked
	require.Nil(t, c.topicIDs.revokeWait)
	require.Empty(t, c.topicIDs.revokeTopic)
	<-c.prepareRevokeWait("kin")

	// Reassignment must restore ownership after a loss/revoke.
	c.AssignPartitions(map[string][]int32{"kin": {1}})
	revoked = c.prepareRevokeWait("kin")
	require.Equal(t, revoked, c.topicIDs.revokeWait)
	c.clearRevokeWait(revoked)
	require.Nil(t, c.topicIDs.revokeWait)
}
