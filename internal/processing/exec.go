// Copyright 2021 - 2023 Crunchy Data Solutions, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package processing

import (
	"bytes"
	"fmt"
	"io"
)

// Executor calls commands
type Executor func(
	stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error

// PgBackRestInfo defines a pgBackRest info command with relevant flags set
func (exec Executor) PgBackRestInfo(output, repoNum string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	var command string

	command = "pgbackrest info --output=" + output
	if repoNum != "" {
		command += " --repo=" + repoNum
	}
	err := exec(nil, &stdout, &stderr, "bash", "-ceu", "--", command)

	return stdout.String(), stderr.String(), err
}

// ListPGLogFiles returns the full path of numLogs log files.
func (exec Executor) ListPGLogFiles(numLogs int) (string, string, error) {
	var stdout, stderr bytes.Buffer

	command := fmt.Sprintf("ls -1dt pgdata/pg[0-9][0-9]/log/* | head -%d", numLogs)
	err := exec(nil, &stdout, &stderr, "bash", "-ceu", "--", command)

	return stdout.String(), stderr.String(), err
}

// CatFile takes the full path of a file and returns the contents
// of that file
func (exec Executor) CatFile(filePath string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	command := fmt.Sprintf("cat %s", filePath)
	err := exec(nil, &stdout, &stderr, "bash", "-ceu", "--", command)

	return stdout.String(), stderr.String(), err
}

// Patronictl takes a Patronictl subcommand and returns the output of that command
func (exec Executor) Patronictl(cmd string) (string, string, error) {
	var stdout, stderr bytes.Buffer

	command := "Patronictl " + cmd
	err := exec(nil, &stdout, &stderr, "bash", "-ceu", "--", command)

	return stdout.String(), stderr.String(), err
}

// Processes returns the output of a ps command
func (exec Executor) Processes() (string, string, error) {
	var stdout, stderr bytes.Buffer

	command := "ps aux --width 500"
	err := exec(nil, &stdout, &stderr, "bash", "-ceu", "--", command)

	return stdout.String(), stderr.String(), err
}
