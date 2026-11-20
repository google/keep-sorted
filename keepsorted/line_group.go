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
	"slices"
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

// matchesAnyRegex returns true if s matches one of the regexes.
func matchesAnyRegex(s string, regexes []*regexp.Regexp) bool {
	for _, regex := range regexes {
		if regex.FindStringSubmatch(s) != nil {
			return true
		}
	}
	return false
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

	// Determines whether the current block is still accepting additional lines.
	shouldAddToBlock := func() bool {
		return metadata.opts.Block && !lineRange.empty() && block.expectsContinuation()
	}
	// Determines whether the current group should accept the next line.
	shouldAddToGroup := func(i int, l string) bool {
		if !metadata.opts.Group {
			return false
		}

		increasedIndent := !lineRange.empty() && initialIndent != nil && indents[i] > *initialIndent
		return increasedIndent || numUnmatchedStartDirectives > 0 || metadata.opts.hasGroupPrefix(l)
	}
	// Determines whether the current line should be part of a regex-delimited
	// group including any prior lines already visited.
	// Returns another boolean indicating whether the group should be ending
	// after that line if so.
	shouldAddToRegexDelimitedGroup := func(l string) (addToGroup bool, finishGroupAfter bool) {
		if metadata.opts.GroupStartRegex != nil {
			// For GroupStartRegex, all non-regex-matching lines should be
			// part of the group including prior lines.
			return !matchesAnyRegex(l, metadata.opts.GroupStartRegex), false
		}
		if metadata.opts.GroupEndRegex != nil {
			// For GroupEndRegex, the line should always be included in the
			// group including prior lines, but possibly terminate it.
			return true, matchesAnyRegex(l, metadata.opts.GroupEndRegex)
		}
		return false, false
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
		// If the current lineRange ends with an extra empty line, remove it and place it in a separate group.
		// This is notably needed to support group_start_regex or group_end_regex being set at the same time as newline_separated.
		endingEmptyLines := 0
		for lineRange.size() > 1 && lines[lineRange.end-1] == "" {
			endingEmptyLines++
			lineRange.end--
		}
		groups = append(groups, &lineGroup{
			opts:             metadata.opts,
			prefixOrder:      prefixOrder,
			lineGroupContent: lineGroupContent{comment: slice(lines, commentRange), lines: slice(lines, lineRange)},
		})
		commentRange = indexRange{}
		lineRange = indexRange{}
		block = codeBlock{}
		for ; endingEmptyLines > 0; endingEmptyLines-- {
			groups = append(groups, &lineGroup{
				opts:             metadata.opts,
				prefixOrder:      prefixOrder,
				lineGroupContent: lineGroupContent{lines: []string{""}},
			})
		}
	}
	for i, l := range lines {
		if shouldAddToBlock() || shouldAddToGroup(i, l) {
			appendLine(i, l)
		} else if metadata.opts.hasStickyPrefix(l) {
			// Top-level comments break the current block/group.
			if !lineRange.empty() {
				finishGroup()
			}

			commentRange.append(i)
			if metadata.opts.Group {
				// Note: This line will not count end directives. If this call ever
				// finds a start directive, it will set numUnmatchedStartDirectives > 0
				// and then we will enter the shouldAddToGroup branch above where we'll
				// count end directives via its appendLine call.
				countStartDirectives(l)
			}
		} else if addToGroup, finishGroupAfter := shouldAddToRegexDelimitedGroup(l); addToGroup {
			appendLine(i, l)
			if finishGroupAfter {
				finishGroup()
			}
		} else {
			// Begin a new block or group.
			if !lineRange.empty() {
				finishGroup()
			}
			appendLine(i, l)
		}
	}
	if !commentRange.empty() || !lineRange.empty() {
		finishGroup()
	}
	if metadata.opts.Group && metadata.opts.GroupStartRegex == nil && metadata.opts.GroupEndRegex == nil {
		groups = mergeAdjacentStringLiterals(groups)
	}
	return groups
}

// mergeAdjacentStringLiterals keeps a string literal that's split over multiple
// lines together in a list of comma-separated elements.
//
// Formatters split long string literals into adjacent string literals, which C,
// C++, Objective-C, and Python concatenate, without indenting the continuation
// lines:
//
//	"aaa",
//	"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
//	"cccccccccccccccccccccccccccccccc",
//	"ddd",
//
// Sorting those lines separately would change the elements of the list. So if
// any line group ends with a comma, a line group that instead ends with a
// string literal is merged with the next line group if that one starts with a
// string literal.
func mergeAdjacentStringLiterals(groups []*lineGroup) []*lineGroup {
	if !slices.ContainsFunc(groups, (*lineGroup).hasTrailingComma) {
		return groups
	}

	merged := []*lineGroup{groups[0]}
	for _, lg := range groups[1:] {
		if prev := merged[len(merged)-1]; lg.startsWithStringLiteral() && prev.endsWithStringLiteral() {
			prev.lines = slices.Concat(prev.lines, lg.comment, lg.lines)
			continue
		}
		merged = append(merged, lg)
	}
	return merged
}

// endsWithStringLiteral reports whether the last code line of lg ends with a
// quote, ignoring any trailing comment.
func (lg *lineGroup) endsWithStringLiteral() bool {
	_, code, _ := lg.lastCodeLine()
	return strings.HasSuffix(code, `"`) || strings.HasSuffix(code, "'")
}

// stringLiteralStart matches a line that starts with a string literal,
// optionally with a C, C++, or Objective-C prefix (R, @, u8, u, U, L, u8R, LR,
// ...) or a Python prefix (r, b, f, t, u, rb, fr, ...).
var stringLiteralStart = regexp.MustCompile(`^\s*(?:(?:[R@]|(?:u8|[uUL])R?)"|[bfrtuBFRTU]{0,2}["'])`)

// startsWithStringLiteral reports whether the first line of lg starts with a
// string literal.
func (lg *lineGroup) startsWithStringLiteral() bool {
	return len(lg.lines) > 0 && stringLiteralStart.MatchString(lg.lines[0])
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

func (r *indexRange) size() int {
	if !r.init {
		return 0
	}
	return r.end - r.start
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
	// closingCommentMarkers maps the markers that start block comments to the
	// markers that end them.
	closingCommentMarkers = map[string]string{
		"/*":   "*/",
		"<!--": "-->",
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

	// TODO: jfaer - Does this need to handle runes more correctly?
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
		if len(q) == 1 && isEscaped(s, i) {
			// Ignore quote literals (\", \', \`)
			continue
		}
		if s[i:i+len(q)] == q {
			return q
		}
	}
	return ""
}

// isEscaped reports whether the byte at index i in s is escaped by an odd
// number of preceding backslashes.
func isEscaped(s string, i int) bool {
	escaped := false
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		escaped = !escaped
	}
	return escaped
}

// hasCode reports whether lg has at least one line with code (not just
// whitespace or comments).
func (lg *lineGroup) hasCode() bool {
	i, _, _ := lg.lastCodeLine()
	return i >= 0
}

// lastCodeLine finds the last line in lg.lines that contains code and splits it
// into the line index, its code, and the rest of the line (whitespace and any
// trailing comment). If lg has no code lines, index is -1.
//
// For example, `  "foo",  // comment` is split into `  "foo",` and
// `  // comment`. A block comment followed by more code on the same line, like
// `/* x */` in `FOO /* x */,`, is part of the code. Quoted strings and block
// comments can span multiple lines.
func (lg *lineGroup) lastCodeLine() (index int, code, tail string) {
	index = -1
	marker := lg.opts.commentMarker
	closer := closingCommentMarkers[marker]
	var expectedQuote string
	var inBlockComment bool
	for n, line := range lg.lines {
		commentStart := -1
		if inBlockComment {
			commentStart = 0
		}
		for i := 0; i < len(line); {
			if inBlockComment {
				j := strings.Index(line[i:], closer)
				if j < 0 {
					break
				}
				inBlockComment = false
				i += j + len(closer)
				continue
			}
			if expectedQuote != "" {
				if q := findQuote(line, i); q == expectedQuote {
					expectedQuote = ""
					i += len(q)
					continue
				}
				i++
				continue
			}
			// A comment marker only starts a comment at the start of the line or
			// after whitespace, so `foo#bar` or `a//b` isn't treated as a comment.
			if marker != "" && strings.HasPrefix(line[i:], marker) && (i == 0 || isBlank(line[i-1])) {
				if commentStart < 0 {
					commentStart = i
				}
				if closer == "" {
					// The rest of the line is a comment.
					break
				}
				inBlockComment = true
				i += len(marker)
				continue
			}
			if !isBlank(line[i]) {
				// Code follows the block comment, so it isn't a trailing comment.
				commentStart = -1
			}
			if q := findQuote(line, i); q != "" {
				expectedQuote = q
				i += len(q)
				continue
			}
			i++
		}
		withoutComment := line
		if commentStart >= 0 {
			withoutComment = line[:commentStart]
		}
		if c := strings.TrimRightFunc(withoutComment, unicode.IsSpace); c != "" {
			index, code, tail = n, c, line[len(c):]
		}
	}
	return index, code, tail
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t'
}

// hasTrailingComma reports whether the last code line of lg ends with a comma,
// ignoring any trailing comment.
func (lg *lineGroup) hasTrailingComma() bool {
	_, code, _ := lg.lastCodeLine()
	return strings.HasSuffix(code, ",")
}

// addTrailingComma adds a comma after the code on the last code line of lg,
// which must exist.
//
// If the line has a trailing comment, the comma replaces one of the spaces
// before the comment (unless there's only one) so that the comment doesn't
// move.
func (lg *lineGroup) addTrailingComma() {
	i, code, tail := lg.lastCodeLine()
	if strings.TrimSpace(tail) != "" && (strings.HasPrefix(tail, "  ") || strings.HasPrefix(tail, " \t")) {
		tail = tail[1:]
	}
	lg.setLine(i, code+","+tail)
}

// removeTrailingComma removes the comma after the code on the last code line
// of lg, which must exist.
//
// If the line has a trailing comment, the comma is replaced with a space so
// that the comment doesn't move.
func (lg *lineGroup) removeTrailingComma() {
	i, code, tail := lg.lastCodeLine()
	if strings.TrimSpace(tail) != "" {
		tail = " " + tail
	}
	lg.setLine(i, strings.TrimSuffix(code, ",")+tail)
}

// setLine replaces lg.lines[i] with s.
//
// lg.lines might share its backing array with block.lines, which must not be
// modified, so this modifies a copy.
func (lg *lineGroup) setLine(i int, s string) {
	lg.access = accessRecorder{}
	lg.lines = slices.Clone(lg.lines)
	lg.lines[i] = s
}

func (lg *lineGroup) commentOnly() bool {
	lg.access.commentOnly = true
	return len(lg.lines) == 0
}

func (lg *lineGroup) regexTokens() []regexToken {
	var regexMatches []regexMatch
	if len(lg.opts.ByRegex) == 0 {
		// We apply other options on top of what the regex extracts, but if the user
		// didn't set by_regex, we should fall back to the behavior they would've
		// expected before we started supporting by_regex.
		// Namely: We would apply the other options on top of the
		// internalJoinedLines() instead of the raw content.
		regexMatches = []regexMatch{{lg.internalJoinedLines()}}
	} else {
		regexMatches = lg.opts.matchRegexes(lg.regexJoinedLines())
		// We still want to apply the joinLines transform so that we get
		// "reasonable human" comparisons if the regex matches more than one line.
		for _, match := range regexMatches {
			for i, s := range match {
				match[i] = joinLines(strings.Split(s, "\n"))
			}
		}
	}

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

// internalJoinedLines attempts to concatenate all of this lineGroup's content
// the same way a reasonable human might.
//
// If the previous line ends with a "word" character and the current line starts
// with a "word" character, the two lines will be separated by a space.
// Otherwise, the lines are concatenated without any separation.
//
// Unlike joinedLines, this method does not record that it was used in the
// accessRecorder.
func (lg *lineGroup) internalJoinedLines() string {
	return joinLines(lg.lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	endsWithWordChar := regexp.MustCompile(`\w$`)
	startsWithWordChar := regexp.MustCompile(`^\w`)
	var s strings.Builder
	var last string
	for _, l := range lines {
		l := strings.TrimLeftFunc(l, unicode.IsSpace)
		if len(last) > 0 && len(l) > 0 && endsWithWordChar.MatchString(last) && startsWithWordChar.MatchString(l) {
			s.WriteString(" ")
		}
		s.WriteString(l)
		last = l
	}
	return s.String()
}

// regexJoinedLines concatenates all of this lineGroup's content in a way that's
// friendlier to regexes than internalJoinedLines.
//
// Primarily, this method still strips leading whitespace but it uses a real
// newline character to separate lines instead of the "word" character logic of
// internalJoinedLines.
func (lg *lineGroup) regexJoinedLines() string {
	if len(lg.lines) == 0 {
		return ""
	}
	lines := make([]string, len(lg.lines))
	for i, l := range lg.lines {
		lines[i] = strings.TrimLeftFunc(l, unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}

// joinedLines returns internalJoinedLines and records that it was called in the
// accessRecorder.
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
