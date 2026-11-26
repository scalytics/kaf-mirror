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

//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal available: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Fatal(err)
	}
	return master, slave
}

func termiosOf(t *testing.T, f *os.File) *unix.Termios {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return tio
}

func assertCooked(t *testing.T, f *os.File) {
	t.Helper()
	tio := termiosOf(t, f)
	if tio.Lflag&unix.ECHO == 0 || tio.Lflag&unix.ICANON == 0 {
		t.Fatalf("terminal is still in raw mode, lflag %#x", tio.Lflag)
	}
}

func makeRaw(t *testing.T, f *os.File) {
	t.Helper()
	tio := termiosOf(t, f)
	tio.Lflag &^= unix.ECHO | unix.ICANON
	if err := unix.IoctlSetTermios(int(f.Fd()), unix.TCSETS, tio); err != nil {
		t.Fatal(err)
	}
}

func TestSaveTerminalStateRestoresTheOriginalMode(t *testing.T) {
	_, slave := openPTY(t)
	assertCooked(t, slave)
	restore := saveTerminalState(slave)
	makeRaw(t, slave)
	restore()
	assertCooked(t, slave)
}

func TestSaveTerminalStateWithoutTerminalIsHarmless(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	saveTerminalState(file)()
}

func TestExitRestoresTheTerminalBeforeLeaving(t *testing.T) {
	_, slave := openPTY(t)
	restore := saveTerminalState(slave)
	makeRaw(t, slave)
	setTerminalRestorer(restore)
	code := -1
	oldExit := exitProcess
	exitProcess = func(c int) { code = c }
	t.Cleanup(func() {
		exitProcess = oldExit
		setTerminalRestorer(nil)
	})
	abortCancelled()
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	assertCooked(t, slave)
	makeRaw(t, slave)
	abortLogin(nil)
	assertCooked(t, slave)
}

func TestShellExitRequested(t *testing.T) {
	cases := []struct {
		in        string
		breakline bool
		want      bool
	}{
		{"exit", true, true},
		{"  exit  ", true, true},
		{"exit", false, false},
		{"exit now", true, false},
		{"", true, false},
		{"clusters", true, false},
	}
	for _, c := range cases {
		if got := shellExitRequested(c.in, c.breakline); got != c.want {
			t.Fatalf("shellExitRequested(%q, %v) = %v", c.in, c.breakline, got)
		}
	}
}

func TestExitCommandDoesNotEndTheProcess(t *testing.T) {
	root := NewRootCmd()
	ctx := applyShellInput(root, []string{"clusters"}, "exit")
	if len(ctx) != 1 || ctx[0] != "clusters" {
		t.Fatalf("context after exit: %v", ctx)
	}
}

type ptyOutput struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	changed chan struct{}
}

func (o *ptyOutput) pump(master *os.File) {
	chunk := make([]byte, 4096)
	for {
		n, err := master.Read(chunk)
		o.mu.Lock()
		o.buf.Write(chunk[:n])
		o.mu.Unlock()
		select {
		case o.changed <- struct{}{}:
		default:
		}
		if err != nil {
			return
		}
	}
}

func (o *ptyOutput) snapshot() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

func (o *ptyOutput) waitFor(ctx context.Context, t *testing.T, ready func(string) bool) {
	t.Helper()
	for !ready(o.snapshot()) {
		select {
		case <-o.changed:
		case <-ctx.Done():
			t.Fatalf("terminal output never reached the expected state, output %q", o.snapshot())
		}
	}
}

type ptyStep struct {
	send string
	want string
}

func runShellOnPTY(t *testing.T, steps ...ptyStep) {
	t.Helper()
	master, slave := openPTY(t)
	raw, err := json.Marshal([]string{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), cliArgsEnv+"="+string(raw), "HOME="+t.TempDir(), "TERM=xterm")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	out := &ptyOutput{changed: make(chan struct{}, 1)}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go out.pump(master)
	out.waitFor(ctx, t, func(s string) bool { return strings.Contains(s, "kaf-mirror:>") })
	for _, step := range steps {
		before := len(out.snapshot())
		if _, err := master.Write([]byte(step.send)); err != nil {
			t.Fatal(err)
		}
		if step.want == "" {
			continue
		}
		out.waitFor(ctx, t, func(s string) bool { return strings.Contains(s[before:], step.want) })
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shell exited with %v, output %q", err, out.snapshot())
	}
	assertCooked(t, slave)
}

func TestShellLeavesTheTerminalUsableAfterExit(t *testing.T) {
	t.Run("exit command", func(t *testing.T) {
		runShellOnPTY(t, ptyStep{"exit", "exit"}, ptyStep{"\r", ""})
	})
	t.Run("ctrl+d", func(t *testing.T) {
		runShellOnPTY(t, ptyStep{"\x04", ""})
	})
	t.Run("ctrl+c then exit", func(t *testing.T) {
		runShellOnPTY(t, ptyStep{"\x03", "kaf-mirror:>"}, ptyStep{"exit", "exit"}, ptyStep{"\r", ""})
	})
	t.Run("group then exit", func(t *testing.T) {
		runShellOnPTY(t, ptyStep{"clusters", "clusters"}, ptyStep{"\r", "kaf-mirror:clusters>"}, ptyStep{"exit", "exit"}, ptyStep{"\r", ""})
	})
}
