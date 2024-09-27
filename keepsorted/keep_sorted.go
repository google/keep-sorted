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

package keepsorted

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/Workiva/go-datastructures/augmentedtree"
)

const (
	errorUnordered = "These lines are out of order."
)

// Fixer runs the business logic of keep-sorted.
type Fixer struct {
	ID string

	defaultOptions blockOptions
	startDirective string
	endDirective   string
}

// New creates a new fixer with the given string as its identifier.
// By default, id is "keep-sorted"
func New(id string, defaultOptions BlockOptions) *Fixer {
	return &Fixer{
		ID:             id,
		defaultOptions: defaultOptions.opts,
		startDirective: id + " start",
		endDirective:   id + " end",
	}
}

func (f *Fixer) errorMissingStart() string {
	return fmt.Sprintf("This instruction doesn't have matching '%s' line", f.startDirective)
}

func (f *Fixer) errorMissingEnd() string {
	return fmt.Sprintf("This instruction doesn't have matching '%s' line", f.endDirective)
}

// Fix all of the findings on contents to make keep-sorted happy.
func (f *Fixer) Fix(filename, contents string, modifiedLines []LineRange) (fixed string, alreadyCorrect bool, warnings []*Finding) {
	lines := strings.Split(contents, "\n")
	fs := f.findings(filename, lines, modifiedLines)
	if len(fs) == 0 {
		return contents, true, nil
	}

	var s strings.Builder
	startLine := 1
	for _, f := range fs {
		if len(f.Fixes) == 0 {
			warnings = append(warnings, f)
			continue
		}

		repl := f.Fixes[0].Replacements[0]
		endLine := repl.Lines.Start

		// -1 to convert line number to index number.
		s.WriteString(linesToString(lines[startLine-1 : endLine-1]))
		s.WriteString(repl.NewContent)

		startLine = repl.Lines.End + 1
	}
	s.WriteString(strings.Join(lines[startLine-1:], "\n"))

	return s.String(), false, warnings
}

// Findings returns a slice of things that need to be addressed in the file to
// make keep-sorted happy.
//
// If modifiedLines is non-nil, we only report findings for issues within the
// modified lines. Otherwise, we report all findings.
func (f *Fixer) Findings(filename, contents string, modifiedLines []LineRange) []*Finding {
	return f.findings(filename, strings.Split(contents, "\n"), modifiedLines)
}

// Finding is something that keep-sorted thinks is wrong with a particular file.
type Finding struct {
	// The name of the file that this finding is for.
	Path string `json:"path"`
	// The lines that this finding applies to.
	Lines LineRange `json:"lines"`
	// A human-readable message about what the finding is.
	Message string `json:"message"`
	// Possible fixes that could be applied to resolve the problem.
	// Each fix in this slice would independently fix the problem, they do not
	// and should not all be applied.
	Fixes []Fix `json:"fixes"`
}

// LineRange is a 1-based range of continuous lines within a file.
// Both start and end are inclusive.
// You can designate a single line by setting start and end to the same line number.
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Fix is a set of changes that could be made to resolve a Finding.
type Fix struct {
	// The changes that should be made to the file to resolve the Finding.
	// All of these changes need to be made.
	Replacements []Replacement `json:"replacements"`
}

// Replacement is a single substitution to apply to a file.
type Replacement struct {
	// The lines that should be replaced with NewContent.
	Lines      LineRange `json:"lines"`
	NewContent string    `json:"new_content"`
}

func (f *Fixer) findings(filename string, contents []string, modifiedLines []LineRange) []*Finding {
	blocks, incompleteBlocks, warns := f.newBlocks(filename, contents, 1, includeModifiedLines(modifiedLines))

	var fs []*Finding
	fs = append(fs, warns...)
	for _, b := range blocks {
		if s, alreadySorted := b.sorted(); !alreadySorted {
			fs = append(fs, finding(filename, b.start+1, b.end-1, errorUnordered, replacement(b.start+1, b.end-1, linesToString(s))))
		}
	}
	for _, ib := range incompleteBlocks {
		var msg string
		switch ib.dir {
		case startDirective:
			msg = f.errorMissingEnd()
		case endDirective:
			msg = f.errorMissingStart()
		default:
			panic(fmt.Errorf("unknown directive type: %v", ib.dir))
		}
		fs = append(fs, finding(filename, ib.line, ib.line, msg, replacement(ib.line, ib.line, "")))
	}

	slices.SortFunc(fs, func(a, b *Finding) int {
		return cmp.Compare(startLine(a), startLine(b))
	})
	return fs
}

func includeModifiedLines(modifiedLines []LineRange) func(start, end int) bool {
	if modifiedLines == nil {
		return func(_, _ int) bool {
			return true
		}
	}
	t := augmentedtree.New(1)
	for _, lr := range modifiedLines {
		t.Add(lr)
	}
	return func(start, end int) bool {
		return len(t.Query(LineRange{start, end})) != 0
	}
}

// linesToString converts the string slice of lines into a single string.
// This function assumes that every line should end with "\n", including the
// last line.
func linesToString(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

func finding(filename string, start, end int, msg string, fixes ...Fix) *Finding {
	return &Finding{
		Path:    filename,
		Lines:   lineRange(start, end),
		Message: msg,
		Fixes:   fixes,
	}
}

func replacement(start, end int, s string) Fix {
	return Fix{
		Replacements: []Replacement{
			{
				Lines:      lineRange(start, end),
				NewContent: s,
			},
		},
	}
}

func lineRange(start, end int) LineRange {
	return LineRange{
		Start: start,
		End:   end,
	}
}

func startLine(f *Finding) int {
	return f.Lines.Start
}

var _ augmentedtree.Interval = LineRange{}

func (lr LineRange) LowAtDimension(uint64) int64 {
	return int64(lr.Start)
}

func (lr LineRange) HighAtDimension(uint64) int64 {
	return int64(lr.End)
}

func (lr LineRange) OverlapsAtDimension(i augmentedtree.Interval, d uint64) bool {
	return lr.HighAtDimension(d) >= i.LowAtDimension(d) ||
		lr.LowAtDimension(d) <= i.HighAtDimension(d)
}

func (lr LineRange) ID() uint64 {
	// Use the cantor pairing function to embed int x int into int.
	return uint64((lr.Start+lr.End)*(lr.Start+lr.End+1)/2 + lr.End)
}
