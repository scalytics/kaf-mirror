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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/scalytics/kaf-mirror/v2/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadTLSFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.fifo")
	require.NoError(t, syscall.Mkfifo(path, 0600))

	done := make(chan error, 1)
	go func() {
		_, err := readTLSFile(path, "ca_file")
		done <- err
	}()

	timeout := time.Second
	if deadline, ok := t.Deadline(); ok {
		if remain := time.Until(deadline) - 100*time.Millisecond; remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ca_file")
		assert.Contains(t, err.Error(), "not a regular file")
		assert.NotContains(t, err.Error(), "\x00")
	case <-time.After(timeout):
		t.Fatal("readTLSFile blocked on a FIFO")
	}
}

func TestReadTLSFileRejectsDevice(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("/dev/zero is not present")
	}
	_, err := readTLSFile("/dev/zero", "ca_file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ca_file")
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestReadTLSFileRejectsDirectory(t *testing.T) {
	_, err := readTLSFile(t.TempDir(), "cert_file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cert_file")
	assert.Contains(t, err.Error(), "not a regular file")
}

func TestReadTLSFileRejectsOversizedRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(path, make([]byte, MaxTLSFileSize+1), 0600))
	_, err := readTLSFile(path, "ca_file")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ca_file")
	assert.Contains(t, err.Error(), "1 MiB")
	assert.False(t, strings.Contains(err.Error(), strings.Repeat("\x00", 16)))
}

func TestReadTLSFileFollowsSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "ca.crt")
	payload := []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n")
	require.NoError(t, os.WriteFile(target, payload, 0600))
	link := filepath.Join(dir, "ca-link.crt")
	require.NoError(t, os.Symlink(target, link))
	got, err := readTLSFile(link, "ca_file")
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestValidateTLSFileAllowsMissingPath(t *testing.T) {
	err := validateTLSFile(filepath.Join(t.TempDir(), "missing.crt"), "ca_file")
	require.NoError(t, err)
}

func TestProbeFingerprintFailsClosedWhenTLSFileIsRejected(t *testing.T) {
	cfg := config.ClusterConfig{
		Brokers:  "broker-1:9093",
		Security: config.SecurityConfig{Protocol: "SSL", CAFile: t.TempDir()},
	}
	fp, ok := ProbeFingerprint(cfg)
	require.False(t, ok)
	require.Empty(t, fp)

	fifo := filepath.Join(t.TempDir(), "ca.fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0600))
	cfg.Security.CAFile = fifo
	done := make(chan struct {
		fp string
		ok bool
	}, 1)
	go func() {
		fp, ok := ProbeFingerprint(cfg)
		done <- struct {
			fp string
			ok bool
		}{fp, ok}
	}()
	select {
	case got := <-done:
		require.False(t, got.ok)
		require.Empty(t, got.fp)
	case <-time.After(time.Second):
		t.Fatal("ProbeFingerprint blocked on a FIFO")
	}
}
