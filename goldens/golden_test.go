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
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var (
	dir, gitDir string
)

func init() {
	_, fn, _, _ := runtime.Caller(0)
	dir = filepath.Dir(fn)
	var err error
	gitDir, err = showTopLevel(dir)
	if err != nil {
		panic(fmt.Errorf("could not find root git dir: %w", err))
	}
}

func TestGoldens(t *testing.T) {
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

	if len(tcs) == 0 {
		t.Fatalf("Did not find any golden files.")
	}

	needsRegen := make(chan string, 2*len(tcs))
	// The outer t.Run doesn't return until all the parallel tests have completed.
	t.Run("parallelTests", func(t *testing.T) {
		for _, tc := range tcs {
			t.Run(tc, func(t *testing.T) {
				t.Parallel()
				inFile := filepath.Join(dir, tc+".in")
				in, err := os.Open(inFile)
				if err != nil {
					t.Fatalf("Could not open .in file: %v", err)
				}

				wantOut, err := os.ReadFile(filepath.Join(dir, tc+".out"))
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						needsRegen <- inFile
					}
					t.Fatalf("Could not read .out file: %v", err)
				}
				wantErr, err := os.ReadFile(filepath.Join(dir, tc+".err"))
				if err != nil {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("Could not read .err file: %v", err)
					}
				}
				// stderr should only ever use "\n" for line endings.
				wantErr = []byte(strings.ReplaceAll(string(wantErr), "\r\n", "\n"))
				wantErr = []byte(strings.ReplaceAll(string(wantErr), "\r", "\n"))

				gotOut, gotErr, err := runKeepSorted(in)
				if err != nil {
					t.Errorf("Had trouble running keep-sorted: %v", err)
				}
				if diff := cmp.Diff(string(wantOut), gotOut); diff != "" {
					t.Errorf("keep-sorted stdout diff (-want +got):\n%s", diff)
					needsRegen <- inFile
				}
				if diff := cmp.Diff(string(wantErr), gotErr); diff != "" {
					t.Errorf("keep-sorted stderr diff (-want +got):\n%s", diff)
					needsRegen <- inFile
				}

				gotOut2, _, err := runKeepSorted(strings.NewReader(gotOut))
				if err != nil {
					t.Errorf("Had trouble running keep-sorted on keep-sorted output: %v", err)
				}
				if diff := cmp.Diff(gotOut, gotOut2); diff != "" {
					t.Errorf("keep-sorted diff on keep-sorted output (should be idempotent) (-want +got)\n%s", diff)
				}
			})
		}
	})

	close(needsRegen)
	files := make(map[string]bool)
	for f := range needsRegen {
		files[f] = true
	}

	if len(files) != 0 {
		t.Logf("Run the following to fix: %s %s", filepath.Join(gitDir, "goldens/generate-goldens.sh"), strings.Join(slices.Sorted(maps.Keys(files)), " "))
	}
}

func showTopLevel(dir string) (string, error) {
	b, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(b)), err
}

func runKeepSorted(stdin io.Reader) (stdout, stderr string, err error) {
	cmd := exec.Command("go", "run", gitDir, "--id=keep-sorted-test", "--omit-timestamps", "-")
	cmd.Stdin = stdin
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("could not create stdout pipe: %w", err)
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("could not create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("could not start keep-sorted: %w", err)
	}

	var errs []error
	gotOut, err := io.ReadAll(outPipe)
	if err != nil {
		errs = append(errs, fmt.Errorf("could not read keep-sorted stdout: %w", err))
	}

	gotErr, err := io.ReadAll(errPipe)
	if err != nil {
		errs = append(errs, fmt.Errorf("could not read keep-sorted stderr: %w", err))
	}

	if err := cmd.Wait(); err != nil {
		errs = append(errs, fmt.Errorf("keep-sorted failed: %w", err))
	}

	return string(gotOut), string(gotErr), errors.Join(errs...)
}
