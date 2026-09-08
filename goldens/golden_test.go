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
	"bytes"
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

	"github.com/bluekeyes/go-gitdiff/gitdiff"
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
				in, err := os.ReadFile(inFile)
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
				// stderr should only ever use "\n" for line endings, but the golden
				// file we read might have OS-specific line endings thanks to Git.
				wantErr = []byte(strings.ReplaceAll(string(wantErr), "\r\n", "\n"))
				wantErr = []byte(strings.ReplaceAll(string(wantErr), "\r", "\n"))

				gotOut, gotErr, exitCode, err := runKeepSorted(bytes.NewReader(in), "fix")
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

				testDiffMode(t, in, wantOut)

				gotOut2, _, exitCode2, err := runKeepSorted(strings.NewReader(gotOut), "fix")
				if err != nil {
					t.Errorf("Had trouble running keep-sorted on keep-sorted output: %v", err)
				}
				if exitCode != exitCode2 {
					t.Errorf("Running keep-sorted on keep-sorted output returned a different exit code (should be idempotent): got %d want %d", exitCode2, exitCode)
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

func testDiffMode(t *testing.T, in []byte, wantOut []byte) {
	t.Run("diff", func(t *testing.T) {
		t.Parallel()
		gotDiff, _, _, err := runKeepSorted(bytes.NewReader(in), "diff")
		if err != nil {
			t.Fatalf("Had trouble running keep-sorted --mode diff: %v", err)
		}
		files, _, err := gitdiff.Parse(strings.NewReader(gotDiff))
		if err != nil {
			t.Fatalf("Had trouble parsing diff: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("Exactly one file is expected in diff, got %d", len(files))
		}
		var b strings.Builder
		err = gitdiff.Apply(&b, bytes.NewReader(in), files[0])
		if err != nil {
			t.Fatalf("Had trouble applying diff: %v", err)
		}
		if diff := cmp.Diff(string(wantOut), b.String()); diff != "" {
			t.Fatalf("Diff applied to the input didn't match expected out:\n%s", diff)
		}
	})
	t.Run("diff after fix", func(t *testing.T) {
		t.Parallel()
		gotDiff, _, _, err := runKeepSorted(bytes.NewReader(wantOut), "diff")
		if err != nil {
			t.Fatalf("Had trouble running keep-sorted --mode diff: %v", err)
		}
		if gotDiff != "" {
			t.Errorf("Non-empty diff produced:\n%s", gotDiff)
		}
	})
}

func showTopLevel(dir string) (string, error) {
	b, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	return strings.TrimSpace(string(b)), err
}

func runKeepSorted(stdin io.Reader, mode string) (stdout, stderr string, exitCode int, err error) {
	cmd := exec.Command("go", "run", gitDir, "--id=keep-sorted-test", "--mode="+mode, "--omit-timestamps", "-")
	cmd.Stdin = stdin
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", -1, fmt.Errorf("could not create stdout pipe: %w", err)
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", -1, fmt.Errorf("could not create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", -1, fmt.Errorf("could not start keep-sorted: %w", err)
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
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else {
			errs = append(errs, fmt.Errorf("keep-sorted failed: %w", err))
		}
	}

	return string(gotOut), string(gotErr), exitCode, errors.Join(errs...)
}
