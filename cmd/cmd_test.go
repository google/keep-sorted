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

package cmd

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/keep-sorted/keepsorted"
)

func TestRun(t *testing.T) {
	operationError := errors.New("operation failed")
	for _, tc := range []struct {
		name           string
		allowNoFiles   bool
		invalidID      bool
		files          []string
		operationOK    bool
		operationError error
		wantCalled     bool
		wantOK         bool
		wantError      string
	}{
		{name: "default rejects no files", wantError: "must pass one or more filenames"},
		{name: "enabled accepts no files without calling operation", allowNoFiles: true, wantOK: true},
		{name: "configuration still validated", allowNoFiles: true, invalidID: true, wantError: "id cannot be empty"},
		{name: "default delegates named files", files: []string{"file.txt"}, operationOK: true, wantCalled: true, wantOK: true},
		{name: "enabled delegates named files", allowNoFiles: true, files: []string{"file.txt"}, operationOK: true, wantCalled: true, wantOK: true},
		{name: "enabled delegates stdin", allowNoFiles: true, files: []string{"-"}, operationOK: true, wantCalled: true, wantOK: true},
		{name: "propagates operation result", allowNoFiles: true, files: []string{"file.txt"}, wantCalled: true},
		{name: "propagates operation error", allowNoFiles: true, files: []string{"file.txt"}, operationError: operationError, wantCalled: true, wantError: "operation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := &Config{
				id:             "keep-sorted",
				defaultOptions: keepsorted.DefaultBlockOptions(),
				allowNoFiles:   tc.allowNoFiles,
				operation: func(_ *keepsorted.Fixer, files []string, _ []keepsorted.LineRange) (bool, error) {
					calls++
					if !slices.Equal(files, tc.files) {
						t.Errorf("operation files = %v, want %v", files, tc.files)
					}
					return tc.operationOK, tc.operationError
				},
			}
			if tc.invalidID {
				c.id = ""
			}
			ok, err := Run(c, tc.files)
			wantCalls := 0
			if tc.wantCalled {
				wantCalls = 1
			}
			if calls != wantCalls || ok != tc.wantOK {
				t.Errorf("Run() calls = %d, ok = %v; want calls = %d, ok = %v", calls, ok, wantCalls, tc.wantOK)
			}
			if tc.wantError == "" {
				if err != nil {
					t.Errorf("Run() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("Run() error = %v, want error containing %q", err, tc.wantError)
			}
		})
	}
}
