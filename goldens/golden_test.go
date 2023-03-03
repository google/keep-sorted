// Copyright 2023 Google LLC
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

package golden_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGoldens(t *testing.T) {
	_, fn, _, _ := runtime.Caller(0)
	dir := filepath.Dir(fn)
	gitDir, err := showTopLevel(dir)
	if err != nil {
		t.Fatalf("Could not find root git dir: %v", err)
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Could not read goldens/ directory: %v", err)
	}

	var tcs []string
	for _, de := range des {
		if n, ok := strings.CutSuffix(de.Name(), ".in"); ok {
			tcs = append(tcs, n)
		}
	}

	needsRegen := make(chan string, len(tcs))
	t.Run("group", func(t *testing.T) {
		for _, tc := range tcs {
			tc := tc
			t.Run(tc, func(t *testing.T) {
				t.Parallel()
				inFile := filepath.Join(dir, tc+".in")
				in, err := os.Open(inFile)
				if err != nil {
					t.Fatalf("Could not open .in file: %v", err)
				}

				out, err := os.Open(filepath.Join(dir, tc+".out"))
				if err != nil {
					t.Fatalf("Could not open .out file: %v", err)
				}
				want, err := ioutil.ReadAll(out)
				if err != nil {
					t.Fatalf("Could not read .out file: %v", err)
				}

				cmd := exec.Command("go", "run", gitDir, "--id=keep-sorted-test", "-")
				cmd.Stdin = in
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatalf("Could not create stdout pipe: %v", err)
				}
				stderr, err := cmd.StderrPipe()
				if err != nil {
					t.Fatalf("Could not create stderr pipe: %v", err)
				}
				if err := cmd.Start(); err != nil {
					t.Errorf("could not start keep-sorted: %v", err)
				}

				if stderr, err := ioutil.ReadAll(stderr); err != nil {
					t.Errorf("could not read keep-sorted stderr: %v", err)
				} else if len(stderr) != 0 {
					t.Errorf("keep-sorted stderr: %s", string(stderr))
				}

				if got, err := ioutil.ReadAll(stdout); err != nil {
					t.Errorf("could not read keep-sorted stdout: %v", err)
				} else if diff := cmp.Diff(strings.Split(string(want), "\n"), strings.Split(string(got), "\n")); diff != "" {
					t.Errorf("keep-sorted stdout diff (-want +got):\n%s", diff)
					needsRegen <- inFile
				}

				if err := cmd.Wait(); err != nil {
					t.Errorf("keep-sorted failed: %v", err)
				}
			})
		}
	})

	close(needsRegen)
	var files []string
	for f := range needsRegen {
		files = append(files, f)
	}

	if len(files) != 0 {
		t.Logf("Run the following to fix: %s %s", filepath.Join(gitDir, "goldens/generate-goldens.sh"), strings.Join(files, " "))
	}
}

func showTopLevel(dir string) (string, error) {
	b, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(b)), err
}
