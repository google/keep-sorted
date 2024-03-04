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
	"regexp"
	"strings"
	"unicode"
)

// lineGroup is a logical unit of source code. It's one or more lines combines
// with zero or more comment lines about the source code lines.
type lineGroup struct {
	comment []string
	lines   []string
}

// groupLines splits lines into one or more lineGroups based on the provided options.
func groupLines(lines []string, metadata blockMetadata) []lineGroup {
	var groups []lineGroup
	// Tracks which subsection of lines contains the comments for the current lineGroup.
	var commentRange indexRange
	// Tracks which subsection of lines contains the content for the current lineGroup.
	var lineRange indexRange

	// group=yes and block=no, these pieces of information are used to determine
	// when we group lines together into a single group.

	// Indent: All lines indented further than the first line are grouped together.
	// Edge case: Whitespace-only lines are included in the group based on the
	// indentation of the next non-empty line after the whitespace-only line.
	var indents []int
	var initialIndent *int
	// Counts the number of unmatched start directives we've seen in the current group.
	// We will include entire keep-sorted blocks as grouped lines to avoid
	// breaking nested keep-sorted blocks that don't have indentation.
	var numUnmatchedStartDirectives int

	// block=yes: The code block that we're constructing until we have matched braces and quotations.
	var block codeBlock

	if metadata.opts.Group {
		indents = calculateIndents(lines)
	}

	// append a line to both lineRange, and block, if necessary.
	appendLine := func(i int, l string) {
		lineRange.append(i)
		if metadata.opts.Block {
			block.append(l, metadata.opts)
		} else if metadata.opts.Group {
			if strings.Contains(l, metadata.startDirective) {
				numUnmatchedStartDirectives++
			} else if strings.Contains(l, metadata.endDirective) {
				numUnmatchedStartDirectives--
			}
		}
	}
	// finish an outstanding lineGroup and reset our state to prepare for a new lineGroup.
	finishGroup := func() {
		groups = append(groups, lineGroup{comment: slice(lines, commentRange), lines: slice(lines, lineRange)})
		commentRange = indexRange{}
		lineRange = indexRange{}
		block = codeBlock{}
	}
	for i, l := range lines {
		if metadata.opts.Block && !lineRange.empty() && block.expectsContinuation() {
			appendLine(i, l)
		} else if metadata.opts.Group && (!lineRange.empty() && initialIndent != nil && indents[i] > *initialIndent || numUnmatchedStartDirectives > 0) {
			appendLine(i, l)
		} else if metadata.opts.Group && metadata.opts.hasGroupPrefix(l) {
			appendLine(i, l)
		} else if metadata.opts.hasStickyPrefix(l) {
			if !lineRange.empty() {
				finishGroup()
			}

			if !metadata.opts.Block && metadata.opts.Group && strings.Contains(l, metadata.startDirective) {
				// We don't need to check for end directives here because this makes
				// numUnmatchedStartDirectives > 0, so we'll take the code path above through appendLine.
				appendLine(i, l)
			} else {
				commentRange.append(i)
			}
		} else {
			if !lineRange.empty() {
				finishGroup()
			}
			if metadata.opts.Group && initialIndent == nil {
				initialIndent = &indents[i]
			}
			appendLine(i, l)
		}
	}
	if !commentRange.empty() || !lineRange.empty() {
		finishGroup()
	}
	return groups
}

// calculateIndents precalculates the indentation for each line.
// We do this precalculation so that we don't get bad worst-case behavior if
// someone had a bunch of newlines in a group=yes block.
func calculateIndents(lines []string) []int {
	ret := make([]int, len(lines))
	for i, l := range lines {
		indent, ok := countIndent(l)
		if !ok {
			indent = -1
		}
		ret[i] = indent
	}

	// Allow for newlines to have an indent if the next non-empty line has hanging
	// indent.
	// Go backwards through the indent list so that it's harder to accidentally
	// get O(n^2) behavior for a long section of newlines.
	indent := -1
	for i := len(ret) - 1; i >= 0; i-- {
		if ret[i] == -1 {
			ret[i] = indent
			continue
		}

		indent = ret[i]
	}

	return ret
}

// countIndent counts how many space characters occur at the beginning of s.
func countIndent(s string) (indent int, hasNonSpaceCharacter bool) {
	c := 0
	for _, ch := range s {
		if unicode.IsSpace(ch) {
			c++
			continue
		}
		break
	}
	if c == len(s) {
		return 0, false
	}
	return c, true
}

// indexRange is a helper struct that let us gradually figure out how big a
// lineGroup is without having to re-slice the underlying data multiple times.
type indexRange struct {
	start, end int
	init       bool
}

func (r *indexRange) empty() bool {
	return !r.init || r.start == r.end
}

func (r *indexRange) append(i int) {
	if !r.init {
		r.start = i
		r.end = i + 1
		r.init = true
		return
	}

	if r.end != i {
		panic(fmt.Errorf("cannot append %d to %#v because end is %d", i, r, r.end))
	}
	r.end = i + 1
}

func slice(s []string, r indexRange) []string {
	if r.empty() {
		return nil
	}
	return s[r.start:r.end]
}

var (
	braces = []struct {
		open  string
		close string
	}{
		{"{", "}"},
		{"[", "]"},
		{"(", ")"},
	}
	quotes = []string{
		`"""`, `'''`, "```",
		`"`, `'`, "`",
	}
)

// codeBlock is a helper struct that let us try to understand if a section of
// code expects more lines to be "complete".
type codeBlock struct {
	braceCounts   map[string]int
	expectedQuote string
}

// expectsContinuation determines whether it seems like the lines seen so far
// expect a continuation of characters.
//
// Current naive definition of this is to just see if the typically balanced
// symbols (parenthesis, square brackets, braces, and quotes) are balanced. If
// not, we'll assume the next line is a continuation. Quotation marks within
// strings are ignored. This could be extended in the future (and possibly
// controlled by further options).
//
// Known limitations:
// - Parenthesis, square brackets, and braces could appear in any order
// - Parenthesis, square brackets, and braces within strings aren't ignored
func (cb *codeBlock) expectsContinuation() bool {
	for _, b := range braces {
		if cb.braceCounts[b.open] != cb.braceCounts[b.close] {
			return true
		}
	}

	return cb.expectedQuote != ""
}

// append the given line to this codeblock, and update expectsContinuation appropriately.
func (cb *codeBlock) append(s string, opts blockOptions) {
	if cb.braceCounts == nil {
		cb.braceCounts = make(map[string]int)
	}

	// TODO(jfalgout): Does this need to handle runes more correctly?
	for i := 0; i < len(s); {
		if cb.expectedQuote == "" {
			// We do not appear to be inside a string literal.
			// Treat braces as part of the syntax.
			for _, b := range braces {
				if s[i:i+1] == b.open {
					cb.braceCounts[b.open]++
				}
				if s[i:i+1] == b.close {
					cb.braceCounts[b.close]++
				}
			}
			// Ignore trailing comments (rest of the line).
			if cm := opts.commentMarker; cm != "" && len(s[i:]) >= len(cm) && s[i:i+len(cm)] == cm {
				break
			}
		}
		if q := findQuote(s, i); cb.expectedQuote == "" && q != "" {
			cb.expectedQuote = q
			i += len(q)
			continue
		} else if cb.expectedQuote != "" && q == cb.expectedQuote {
			cb.expectedQuote = ""
			i += len(q)
			continue
		}

		i++
	}
}

// findQuote looks for one of the quotes in s at position i, returning which
// quote was found if one was found.
func findQuote(s string, i int) string {
	for _, q := range quotes {
		if len(s[i:]) < len(q) {
			continue
		}
		if len(q) == 1 && i > 0 && string(s[i-1]) == `\` {
			// Ignore quote literals (\", \', \`)
			continue
		}
		if s[i:i+len(q)] == q {
			return q
		}
	}
	return ""
}

func (lg lineGroup) append(s string) {
	lg.lines[len(lg.lines)-1] = lg.lines[len(lg.lines)-1] + s
}

func (lg lineGroup) hasPrefix(s string) bool {
	return strings.HasPrefix(lg.joinedLines(), s)
}

func (lg lineGroup) hasSuffix(s string) bool {
	return len(lg.lines) > 0 && strings.HasSuffix(lg.lines[len(lg.lines)-1], s)
}

func (lg lineGroup) trimSuffix(s string) {
	lg.lines[len(lg.lines)-1] = strings.TrimSuffix(lg.lines[len(lg.lines)-1], s)
}

func (lg lineGroup) joinedLines() string {
	// TODO(jfalgout): This is a good candidate for caching. Make sure we
	// invalidate it if this line group gets modified, though (like it does when
	// we handle trailing commas correctly).
	if len(lg.lines) == 0 {
		return ""
	}

	endsWithWordChar := regexp.MustCompile(`\w$`)
	startsWithWordChar := regexp.MustCompile(`^\w`)
	var s strings.Builder
	var last string
	for _, l := range lg.lines {
		l := strings.TrimLeftFunc(l, unicode.IsSpace)
		if len(last) > 0 && len(l) > 0 && endsWithWordChar.MatchString(last) && startsWithWordChar.MatchString(l) {
			s.WriteString(" ")
		}
		s.WriteString(l)
		last = l
	}
	return s.String()
}

func (lg lineGroup) less(other lineGroup) int {
	if c := strings.Compare(lg.joinedLines(), other.joinedLines()); c != 0 {
		return c
	}
	return cmp.Compare(strings.Join(lg.comment, "\n"), strings.Join(other.comment, "\n"))
}

func (lg lineGroup) GoString() string {
	var comment strings.Builder
	for _, c := range lg.comment {
		comment.WriteString(fmt.Sprintf("  %#v\n", c))
	}
	var lines strings.Builder
	for _, l := range lg.lines {
		lines.WriteString(fmt.Sprintf("  %#v\n", l))
	}
	return fmt.Sprintf("LineGroup{\ncomment=\n%slines=\n%s}", comment.String(), lines.String())
}

func (lg lineGroup) allLines() []string {
	var all []string
	all = append(all, lg.comment...)
	all = append(all, lg.lines...)
	return all
}

func (lg lineGroup) String() string {
	return strings.Join(lg.allLines(), "\n")
}
