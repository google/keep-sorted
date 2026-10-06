// Copyright 2026 Google LLC
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

package cmd_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAllowNoFiles(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "keep-sorted")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", binary, "..").CombinedOutput(); err != nil {
		t.Fatalf("build keep-sorted: %v\n%s", err, out)
	}

	// Split markers so keep-sorted does not treat test fixtures as directives.
	const sorted = "// keep-" + "sorted start\na\nb\n// keep-" + "sorted end\n"
	const unsorted = "// keep-" + "sorted start\nb\na\n// keep-" + "sorted end\n"
	file := filepath.Join(dir, "sorted.txt")
	if err := os.WriteFile(file, []byte(sorted), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")

	for _, mode := range []string{"fix", "lint"} {
		stdinCode, stdinOut := 0, sorted
		if mode == "lint" {
			stdinCode, stdinOut = 1, ""
		}
		for _, tc := range []struct {
			name         string
			args         []string
			stdin        string
			wantCode     int
			wantStdout   string
			wantError    string
			wantFindings bool
		}{
			{name: "default rejects no files", wantCode: 1, wantError: "must pass one or more filenames"},
			{name: "enabled accepts no files", args: []string{"--allow-no-files"}},
			{name: "explicit false rejects no files", args: []string{"--allow-no-files=false"}, wantCode: 1, wantError: "must pass one or more filenames"},
			{name: "missing file still fails", args: []string{"--allow-no-files", missing}, wantCode: 1, wantError: "missing.txt"},
			{name: "configuration still validated", args: []string{"--allow-no-files", "--id="}, wantCode: 1, wantError: "id cannot be empty"},
			{name: "named file still processed", args: []string{"--allow-no-files", file}},
			{name: "stdin still processed", args: []string{"--allow-no-files", "-"}, stdin: unsorted, wantCode: stdinCode, wantStdout: stdinOut, wantFindings: mode == "lint"},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				command := exec.Command(binary, append([]string{"--color=never", "--omit-timestamps", "--mode=" + mode}, tc.args...)...)
				command.Stdin = strings.NewReader(tc.stdin)
				var stdout, stderr strings.Builder
				command.Stdout, command.Stderr = &stdout, &stderr
				code := 0
				if err := command.Run(); err != nil {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) {
						t.Fatal(err)
					}
					code = exitErr.ExitCode()
				}
				if code != tc.wantCode || (!tc.wantFindings && stdout.String() != tc.wantStdout) {
					t.Errorf("got exit %d, stdout %q; want exit %d, stdout %q; stderr %q", code, stdout.String(), tc.wantCode, tc.wantStdout, stderr.String())
				}
				if tc.wantFindings && !strings.Contains(stdout.String(), "These lines are out of order.") {
					t.Errorf("expected sorting finding in stdout, got %q", stdout.String())
				}
				if tc.wantError == "" {
					if stderr.Len() != 0 {
						t.Errorf("unexpected stderr: %q", stderr.String())
					}
				} else if !strings.Contains(stderr.String(), tc.wantError) {
					t.Errorf("stderr %q does not contain %q", stderr.String(), tc.wantError)
				}
			})
		}
	}
}
