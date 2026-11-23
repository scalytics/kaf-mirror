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
	"fmt"
	"os"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func addYesFlag(cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.Flags().Bool("yes", false, "Confirm without a prompt, for scripts and runs without a terminal")
	}
}

func confirmAction(cmd *cobra.Command, message string) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("no terminal to confirm %q, pass --yes to confirm", message)
	}
	confirm := false
	if err := survey.AskOne(&survey.Confirm{Message: message}, &confirm); err != nil {
		return false, err
	}
	return confirm, nil
}

func closeDB() {
	if db != nil {
		db.Close()
		db = nil
	}
}
