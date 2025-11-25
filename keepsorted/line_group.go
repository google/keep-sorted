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
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/rs/zerolog/log"
)

// lineGroup is a logical unit of source code. It's one or more lines combined
// with zero or more comment lines about the source code lines.
type lineGroup struct {
	opts        blockOptions
	prefixOrder func() *prefixOrder

	// The actual content of the lineGroup.
	lineGroupContent

	// Track which methods are used during sorting so we can filter debugging
	// output to just the parts that are relevant.
	access accessRecorder
}

var compareLineGroups = comparingFunc((*lineGroup).commentOnly, falseFirst()).
	andThen(comparingFunc((*lineGroup).regexTokens, lexicographically(compareRegexTokens))).
	andThen(comparing((*lineGroup).joinedLines)).
	andThen(comparing((*lineGroup).joinedComment))

var compareRegexTokens = comparingFunc(func(t regexToken) bool { return t == nil }, falseFirst()).
	andThen(comparingFunc(func(t regexToken) []*captureGroupToken { return t }, lexicographically(compareCaptureGroupTokens)))

var compareCaptureGroupTokens = comparingFunc((*captureGroupToken).prefix, orderedPrefix.compare).
	andThen(comparingFunc((*captureGroupToken).transform, numericTokens.compare))

type lineGroupContent struct {
	comment []string
	lines   []string
}

type accessRecorder struct {
	commentOnly   bool
	regexTokens   []regexTokenAccessRecorder
	joinedLines   bool
	joinedComment bool
}

// groupLines splits lines into one or more lineGroups based on the provided options.
func groupLines(lines []string, metadata blockMetadata) []*lineGroup {
	var groups []*lineGroup
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

	prefixOrder := sync.OnceValue(func() *prefixOrder { return newPrefixOrder(metadata.opts) })

	if metadata.opts.Group {
		indents = calculateIndents(lines)
	}

	countStartDirectives := func(l string) {
		if strings.Contains(l, metadata.startDirective) {
			numUnmatchedStartDirectives++
		} else if strings.Contains(l, metadata.endDirective) {
			numUnmatchedStartDirectives--
		}
	}

	// append a line to both lineRange, and block, if necessary.
	appendLine := func(i int, l string) {
		lineRange.append(i)
		if metadata.opts.Block {
			block.append(l, metadata.opts)
		}
		if metadata.opts.Group {
			countStartDirectives(l)
		}

		if metadata.opts.Group && initialIndent == nil {
			initialIndent = &indents[i]
			log.Printf("initialIndent: %d", *initialIndent)
		}
	}
	// finish an outstanding lineGroup and reset our state to prepare for a new lineGroup.
	finishGroup := func() {
		groups = append(groups, &lineGroup{
			opts:             metadata.opts,
			prefixOrder:      prefixOrder,
			lineGroupContent: lineGroupContent{comment: slice(lines, commentRange), lines: slice(lines, lineRange)},
		})
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

			commentRange.append(i)
			if metadata.opts.Group {
				// Note: This will not count end directives. If this call ever finds a
				// start directive, it will set numUnmatchedStartDirectives > 0 and then
				// we will enter the branch above where we'll count end directives via
				// its appendLine call.
				countStartDirectives(l)
			}
		} else if len(metadata.opts.GroupDelimiterRegexes) != 0 {
 		        appendLine(i, l)
			for _, match := range metadata.opts.matchRegexes(l, metadata.opts.GroupDelimiterRegexes) {
				if match == nil {
					continue
				}
				if !lineRange.empty() {
					finishGroup()
				}
				break
			}
		} else {
			if !lineRange.empty() {
				finishGroup()
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

func (lg *lineGroup) append(s string) {
	lg.access = accessRecorder{}
	lg.lines[len(lg.lines)-1] = lg.lines[len(lg.lines)-1] + s
}

func (lg *lineGroup) hasSuffix(s string) bool {
	return len(lg.lines) > 0 && strings.HasSuffix(lg.lines[len(lg.lines)-1], s)
}

func (lg *lineGroup) trimSuffix(s string) {
	lg.access = accessRecorder{}
	lg.lines[len(lg.lines)-1] = strings.TrimSuffix(lg.lines[len(lg.lines)-1], s)
}

func (lg *lineGroup) commentOnly() bool {
	lg.access.commentOnly = true
	return len(lg.lines) == 0
}

func (lg *lineGroup) regexTokens() []regexToken {
	// TODO: jfaer - Should we match regexes on the original content?
	regexMatches := lg.opts.matchRegexes(lg.internalJoinedLines(), lg.opts.ByRegex)
	ret := make([]regexToken, len(regexMatches))
	if lg.access.regexTokens == nil {
		lg.access.regexTokens = make([]regexTokenAccessRecorder, len(regexMatches))
	}
	for i, match := range regexMatches {
		if match == nil {
			// Regex did not match.
			continue
		}

		ret[i] = make(regexToken, len(match))
		if lg.access.regexTokens[i] == nil {
			lg.access.regexTokens[i] = make(regexTokenAccessRecorder, len(match))
		}
		for j, s := range match {
			order := lg.prefixOrder
			if j != 0 {
				// Only try to match PrefixOrder on the first capture group in a regex.
				// TODO: jfaer - Should this just be the first capture group in the first regex match?
				order = func() *prefixOrder { return nil }
			}
			ret[i][j] = &captureGroupToken{
				opts:        &lg.opts,
				prefixOrder: order,
				raw:         s,
				access:      &lg.access.regexTokens[i][j],
			}
		}
	}
	return ret
}

// internalJoinedLines calculates the same thing as joinedLines, except it
// doesn't record that it was used in the accessRecorder.
func (lg *lineGroup) internalJoinedLines() string {
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

func (lg *lineGroup) joinedLines() string {
	lg.access.joinedLines = true
	return lg.internalJoinedLines()
}

func (lg *lineGroup) joinedComment() string {
	lg.access.joinedComment = true
	if len(lg.comment) == 0 {
		return ""
	}
	return strings.Join(lg.comment, "\n")
}

func (lg *lineGroup) DebugString() string {
	var s strings.Builder
	s.WriteString("LineGroup{\n")
	if len(lg.comment) > 0 {
		s.WriteString("comment=\n")
		for _, c := range lg.comment {
			fmt.Fprintf(&s, "  %#v\n", c)
		}
	}
	if len(lg.lines) > 0 {
		s.WriteString("lines=\n")
		for _, l := range lg.lines {
			fmt.Fprintf(&s, "  %#v\n", l)
		}
	}
	if lg.access.commentOnly {
		fmt.Fprintf(&s, "commentOnly=%t\n", lg.commentOnly())
	}
	if lg.access.regexTokens != nil {
		for i, regex := range lg.regexTokens() {
			if regex.wasUsed() {
				fmt.Fprintf(&s, "regex[%d]=%s\n", i, regex.DebugString())
			}
		}
	}
	if lg.access.joinedLines {
		if len(lg.lines) > 1 {
			// Only print the joinedLines when they're meaningfully different from the
			// raw lines above.
			fmt.Fprintf(&s, "joinedLines=%#v\n", lg.joinedLines())
		} else if !lg.access.joinedComment {
			s.WriteString("linesTiebreaker=true\n")
		}
	}
	if lg.access.joinedComment {
		s.WriteString("commentTiebreaker=true\n")
	}
	s.WriteString("}")
	return s.String()
}

func (lg *lineGroup) allLines() []string {
	var all []string
	all = append(all, lg.comment...)
	all = append(all, lg.lines...)
	return all
}

func (lg *lineGroup) String() string {
	return strings.Join(lg.allLines(), "\n")
}

type regexToken []*captureGroupToken

type regexTokenAccessRecorder []captureGroupTokenAccessRecorder

func (t regexToken) wasUsed() bool {
	if t == nil {
		// Report that the regex didn't match.
		return true
	}
	for _, cg := range t {
		if cg.wasUsed() {
			return true
		}
	}
	return false
}

func (t regexToken) DebugString() string {
	if t == nil {
		return "<did not match>"
	}

	captureGroups := make([]string, len(t))
	for i, cg := range t {
		if cg.wasUsed() {
			captureGroups[i] = cg.DebugString()
		} else {
			captureGroups[i] = "<unused>"
		}
	}

	if len(captureGroups) == 1 {
		return captureGroups[0]
	}
	return fmt.Sprintf("%v", captureGroups)
}

type captureGroupToken struct {
	opts        *blockOptions
	prefixOrder func() *prefixOrder

	raw string

	access *captureGroupTokenAccessRecorder
}

type captureGroupTokenAccessRecorder struct {
	prefix    bool
	transform bool
}

func (t *captureGroupToken) prefix() orderedPrefix {
	ord := t.prefixOrder()
	if ord == nil {
		return orderedPrefix{}
	}
	t.access.prefix = true
	return ord.match(t.raw)
}

func (t *captureGroupToken) transform() numericTokens {
	t.access.transform = true
	// Combinations of switches (for example, case-insensitive and numeric
	// ordering) which must be applied to create a single comparison key,
	// otherwise a sub-ordering can preempt a total ordering:
	//   Foo_45
	//   foo_123
	//   foo_6
	// would be sorted as either (numeric but not case-insensitive)
	//   Foo_45
	//   foo_6
	//   foo_123
	// or (case-insensitive but not numeric)
	//   foo_123
	//   Foo_45
	//   foo_6
	// but should be (case-insensitive and numeric)
	//   foo_6
	//   Foo_45
	//   foo_123
	s := t.opts.trimIgnorePrefix(t.raw)
	if !t.opts.CaseSensitive {
		s = strings.ToLower(s)
	}
	return t.opts.maybeParseNumeric(s)
}

func (t captureGroupToken) wasUsed() bool {
	return t.access.prefix || t.access.transform
}

func (t captureGroupToken) DebugString() string {
	var s []string
	if t.access.prefix {
		s = append(s, fmt.Sprintf("prefix:%q", t.prefix().prefix))
	}
	if t.access.transform {
		var tokens strings.Builder
		if len(s) > 0 {
			tokens.WriteString("tokens:")
		}
		fmt.Fprintf(&tokens, "%s", t.transform().DebugString())
		s = append(s, tokens.String())
	}

	ret := strings.Join(s, " ")
	if len(s) > 1 {
		ret = "[" + ret + "]"
	}
	return ret
}
