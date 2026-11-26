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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const noTerminalShellMessage = "Error: the interactive shell needs a terminal on stdin and stdout. Run a command instead, for example: mirror-cli --help"

var (
	terminalRestorer func()
	exitProcess      = os.Exit
)

func setTerminalRestorer(restore func()) {
	terminalRestorer = restore
}

func saveTerminalState(f *os.File) func() {
	fd := int(f.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return func() {}
	}
	return func() {
		_ = term.Restore(fd, state)
	}
}

func exitCLI(code int) {
	if terminalRestorer != nil {
		terminalRestorer()
	}
	exitProcess(code)
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func stdioIsTerminal() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stdout)
}

func wantsInteractiveShell(rootCmd *cobra.Command, args []string) bool {
	if err := rootCmd.ParseFlags(args); err != nil {
		return false
	}
	if version, _ := rootCmd.Flags().GetBool("version"); version {
		return false
	}
	return len(rootCmd.Flags().Args()) == 0
}

func readSecret(message string) (string, error) {
	if isTerminal(os.Stdin) {
		var secret string
		err := survey.AskOne(&survey.Password{Message: message}, &secret)
		return secret, err
	}
	return readSecretLine(os.Stdin)
}

func readSecretLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	secret := strings.TrimRight(line, "\r\n")
	if secret == "" {
		return "", fmt.Errorf("no password on stdin")
	}
	return secret, nil
}

func writePrivateFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writePasswordFile(path, password string) (string, error) {
	created := false
	if path == "" {
		f, err := os.CreateTemp("", "kaf-mirror-password-*.txt")
		if err != nil {
			return "", err
		}
		path = f.Name()
		created = true
		if err := f.Close(); err != nil {
			os.Remove(path)
			return "", err
		}
	}
	if err := writePrivateFile(path, password+"\n"); err != nil {
		if created {
			os.Remove(path)
		}
		return "", err
	}
	return path, nil
}

func addYesFlag(cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.Flags().Bool("yes", false, "Confirm without a prompt, for scripts and runs without a terminal")
	}
}

func confirmAction(cmd *cobra.Command, message string) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true, nil
	}
	if !isTerminal(os.Stdin) {
		return false, fmt.Errorf("no terminal to confirm %q, pass --yes to confirm", message)
	}
	confirm := false
	if err := survey.AskOne(&survey.Confirm{Message: message}, &confirm); err != nil {
		return false, err
	}
	return confirm, nil
}

func abortCancelled() {
	fmt.Println("Operation cancelled.")
	exitCLI(1)
}

func abortLogin(err error) {
	if err != nil {
		fmt.Printf("Login cancelled: %v\n", err)
	} else {
		fmt.Println("Login cancelled.")
	}
	exitCLI(1)
}

var secretConfigKeys = map[string]bool{
	"token":             true,
	"api_secret":        true,
	"api_key":           true,
	"hec_token":         true,
	"password":          true,
	"connection_string": true,
}

func isSecretConfigPath(path string) bool {
	parts := strings.Split(path, ".")
	return secretConfigKeys[strings.ToLower(parts[len(parts)-1])]
}

func displayConfigValue(path, value string) string {
	if isSecretConfigPath(path) {
		return maskSensitiveValue(value)
	}
	return value
}

var jobSelectionHasTerminal = func() bool {
	return isTerminal(os.Stdin)
}

var askJobSelection = func(prompt *survey.Select, answer *string) error {
	return survey.AskOne(prompt, answer)
}

func selectJob(prompt *survey.Select) (string, error) {
	if !jobSelectionHasTerminal() {
		return "", errors.New("no terminal to select a job, pass the job id")
	}
	var selected string
	if err := askJobSelection(prompt, &selected); err != nil {
		if errors.Is(err, terminal.InterruptErr) {
			return "", errors.New("job selection aborted")
		}
		return "", fmt.Errorf("job selection failed: %w", err)
	}
	if selected == "" {
		return "", errors.New("no job selected, pass the job id")
	}
	return selected, nil
}
