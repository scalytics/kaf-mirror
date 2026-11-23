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
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const noTerminalShellMessage = "Error: the interactive shell needs a terminal on stdin and stdout. Run a command instead, for example: mirror-cli --help"

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
	if path == "" {
		f, err := os.CreateTemp("", "kaf-mirror-password-*.txt")
		if err != nil {
			return "", err
		}
		path = f.Name()
		if err := f.Close(); err != nil {
			return "", err
		}
	}
	if err := writePrivateFile(path, password+"\n"); err != nil {
		return "", err
	}
	return path, nil
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
