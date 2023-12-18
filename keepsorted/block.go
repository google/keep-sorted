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

	"github.com/rs/zerolog/log"
)

type block struct {
	opts blockOptions

	start, end int
	lines      []string

	nestedBlocks []block
}

type incompleteBlock struct {
	line int
	dir  directive
}

type directive int

const (
	startDirective directive = iota
	endDirective
)

// newBlocks finds all keep-sorted blocks in lines and returns them.
//
// block.start and block.end will be the index of the keep-sorted directives
// in lines, plus the additional offset (typically 1 to convert indexes to line numbers).
//
// incompleteBlocks are the index+offset of keep-sorted directives that
// don't have a matching start or end directive.
//
// include is a function that lets the caller determine if a particular block
// should be included in the result. Mostly useful for filtering keep-sorted
// blocks to just the ones that were modified by the currently CL.
func (f *Fixer) newBlocks(lines []string, offset int, include func(start, end int) bool) ([]block, []incompleteBlock) {
	var blocks []block
	var incompleteBlocks []incompleteBlock

	type startLine struct {
		index int
		line  string
	}
	// Stacks of startLines and nested blocks.
	var starts []startLine
	var nestedBlocks [][]block
	for i, l := range lines {
		if strings.Contains(l, f.startDirective) {
			starts = append(starts, startLine{i, l})
		} else if strings.Contains(l, f.endDirective) {
			if len(starts) == 0 {
				incompleteBlocks = append(incompleteBlocks, incompleteBlock{i + offset, endDirective})
				continue
			}
			start := starts[len(starts)-1]
			starts = starts[0 : len(starts)-1]
			endIndex := i

			// Keep any blank lines leading up to the end tag by simply excluding
			// them from being sorted (any at the beginning should already be sorted
			// at the top).
			// The original justification for this was better handling of markdown
			// lists (cr/423863898), but the markdown formatter doesn't seem to care
			// about the newlines anymore.
			// It's nice to keep this around so that users can add a little extra
			// formatting to their keep-sorted blocks.
			for endIndex > start.index && strings.TrimSpace(lines[endIndex-1]) == "" {
				endIndex--
			}

			if !include(start.index+offset, endIndex+offset) {
				continue
			}

			opts, err := f.parseBlockOptions(start.line)
			if err != nil {
				// TODO(b/250608236): Is there a better way to surface this error?
				log.Err(fmt.Errorf("keep-sorted block at index %d had bad start directive: %w", start.index+offset, err)).Msg("")
			}

			depth := len(starts)
			block := block{
				opts:  opts,
				start: start.index + offset,
				end:   endIndex + offset,
				lines: lines[start.index+1 : endIndex],
			}
			if len(nestedBlocks) == depth+1 {
				block.nestedBlocks = nestedBlocks[depth]
				nestedBlocks = nestedBlocks[0:depth]
			}
			if depth == 0 {
				blocks = append(blocks, block)
			} else {
				for len(nestedBlocks) < depth {
					nestedBlocks = append(nestedBlocks, nil)
				}
				nestedBlocks[depth-1] = append(nestedBlocks[depth-1], block)
			}
		}
	}
	if len(starts) > 0 {
		for _, st := range starts {
			incompleteBlocks = append(incompleteBlocks, incompleteBlock{st.index + offset, startDirective})
		}
		for _, nested := range nestedBlocks {
			blocks = append(blocks, nested...)
		}
	}

	return blocks, incompleteBlocks
}

func (b block) sorted() (sorted []string, alreadySorted bool) {
	groups := groupLines(b.lines, b.opts)
	log.Printf("%d groups for block at index %d are (options %#v)", len(groups), b.start, b.opts)
	for _, lg := range groups {
		log.Printf("%#v", lg)
	}

	trimTrailingComma := handleTrailingComma(groups)

	wasNewlineSeparated := true
	if b.opts.NewlineSeparated {
		wasNewlineSeparated = isNewlineSeparated(groups)
		var withoutNewlines []lineGroup
		for _, lg := range groups {
			if isNewline(lg) {
				continue
			}
			withoutNewlines = append(withoutNewlines, lg)
		}
		groups = withoutNewlines
	}

	removedDuplicate := false
	if b.opts.RemoveDuplicates {
		seen := map[string]bool{}
		var deduped []lineGroup
		for _, lg := range groups {
			if s := lg.joinedLines() + "\n" + strings.Join(lg.comment, "\n"); !seen[s] {
				seen[s] = true
				deduped = append(deduped, lg)
			} else {
				removedDuplicate = true
			}
		}
		groups = deduped
	}

	less := b.lessFn()

	if wasNewlineSeparated && !removedDuplicate && slices.IsSortedFunc(groups, less) {
		trimTrailingComma(groups)
		return b.lines, true
	}

	slices.SortStableFunc(groups, less)

	trimTrailingComma(groups)

	if b.opts.NewlineSeparated {
		var separated []lineGroup
		newline := lineGroup{lines: []string{""}}
		for _, lg := range groups {
			if separated != nil {
				separated = append(separated, newline)
			}
			separated = append(separated, lg)
		}
		groups = separated
	}

	l := make([]string, 0, len(b.lines))
	for _, g := range groups {
		l = append(l, g.allLines()...)
	}
	return l, false
}

// isNewlineSeparated determines if the given lineGroups are already NewlineSeparated.
//
// e.g.
// non-empty group
// newline group
// non-empty group
// newline group
// .
// .
// .
// non-empty group
func isNewlineSeparated(gs []lineGroup) bool {
	if len(gs) == 0 {
		return true
	}
	// There should be an odd number of groups.
	if len(gs)%2 != 1 {
		return false
	}
	for i := 0; i < (len(gs)-1)/2; i++ {
		if isNewline(gs[2*i]) || !isNewline(gs[2*i+1]) {
			return false
		}
	}
	return !isNewline(gs[len(gs)-1])
}

// isNewline determines if lg is just an empty line.
func isNewline(lg lineGroup) bool {
	return len(lg.comment) == 0 && len(lg.lines) == 1 && strings.TrimSpace(lg.lines[0]) == ""
}

// handleTrailingComma handles the special case that all lines of a sorted segment are terminated
// by a comma except for the final element; in this case, we add a ',' to the
// last linegroup and strip it again after sorting.
func handleTrailingComma(lgs []lineGroup) (trimTrailingComma func([]lineGroup)) {
	var dataGroups []lineGroup
	for _, lg := range lgs {
		if len(lg.lines) > 0 {
			dataGroups = append(dataGroups, lg)
		}
	}

	if n := len(dataGroups); n > 1 && allHaveSuffix(dataGroups[0:n-1], ",") && !dataGroups[n-1].hasSuffix(",") {
		dataGroups[n-1].append(",")

		return func(lgs []lineGroup) {
			for i := len(lgs) - 1; i >= 0; i-- {
				if len(lgs[i].lines) > 0 {
					lgs[i].trimSuffix(",")
					return
				}
			}
		}

	}

	return func([]lineGroup) {}
}

func allHaveSuffix(lgs []lineGroup, s string) bool {
	for _, lg := range lgs {
		if !lg.hasSuffix(s) {
			return false
		}
	}
	return true
}

func (b block) lessFn() func(a, b lineGroup) int {
	// Always put groups that are only comments last.
	commentOnlyBlock := comparingProperty(func(lg lineGroup) int {
		if len(lg.lines) > 0 {
			return 0
		}
		return 1
	})

	// Check preferred prefixes from longest to shortest. The list of prefixes
	// is reversed to assign weights in ascending order: they are multiplied by
	// -1 to ensure that entries with matching prefixes are put before any
	// non-matching lines (which assume the weight of 0).
	//
	// An empty prefix can be used to move all remaining entries to a position
	// between other prefixes.
	var prefixWeights []prefixWeight
	for i, p := range b.opts.PrefixOrder {
		prefixWeights = append(prefixWeights, prefixWeight{p, i - len(b.opts.PrefixOrder)})
	}
	slices.SortStableFunc(prefixWeights, func(a, b prefixWeight) int {
		return cmp.Compare(b.prefix, a.prefix)
	})

	prefixOrder := comparingProperty(func(lg lineGroup) int {
		for _, w := range prefixWeights {
			if lg.hasPrefix(w.prefix) {
				return w.weight
			}
		}
		return 0
	})

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
	transformOrder := comparingPropertyWith(func(lg lineGroup) numericTokens {
		l := lg.joinedLines()
		if s, ok := b.opts.removeIgnorePrefix(l); ok {
			l = s
		}
		if !b.opts.CaseSensitive {
			l = strings.ToLower(l)
		}
		return b.opts.maybeParseNumeric(l)
	}, numericTokens.compare)

	return func(a, b lineGroup) int {
		for _, cmp := range []func(a, b lineGroup) int{
			commentOnlyBlock,
			prefixOrder,
			transformOrder,
		} {
			if c := cmp(a, b); c != 0 {
				return c
			}
		}
		return a.less(b)
	}
}

func comparingProperty[T any, E cmp.Ordered](f func(T) E) func(a, b T) int {
	return comparingPropertyWith(f, func(a, b E) int {
		if a < b {
			return -1
		} else if a > b {
			return 1
		}
		return 0
	})
}

func comparingPropertyWith[T any, R any](f func(T) R, cmp func(R, R) int) func(a, b T) int {
	return func(a, b T) int {
		return cmp(f(a), f(b))
	}
}

type prefixWeight struct {
	prefix string
	weight int
}
