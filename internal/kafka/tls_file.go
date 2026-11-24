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
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const MaxTLSFileSize = 1 << 20

func readTLSFile(path, setting string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", setting, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", setting, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", setting)
	}
	if info.Size() > MaxTLSFileSize {
		return nil, fmt.Errorf("%s is larger than 1 MiB", setting)
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxTLSFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", setting, err)
	}
	if int64(len(data)) > MaxTLSFileSize {
		return nil, fmt.Errorf("%s is larger than 1 MiB", setting)
	}
	return data, nil
}

func validateTLSFile(path, setting string) error {
	if path == "" {
		return nil
	}
	_, err := readTLSFile(path, setting)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
