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
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
)

type block struct {
	metadata blockMetadata

	start, end int
	// lines are the content of this block from the original file.
	//
	// Do not modify this slice:
	// This slice shares the same backing array as every other keep-sorted block
	// in this file.  That same backing array is also used by Fixer.Fix to
	// generate the fixed file content. Modifying the backing array might have
	// unintended effects on other (nested) blocks. Modifying the backing array
	// will have unintended effects on Fixer.Fix.
	lines []string

	nestedBlocks []block
}

type blockMetadata struct {
	startDirective, endDirective string
	opts                         blockOptions
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
func (f *Fixer) newBlocks(filename string, lines []string, offset int, include func(start, end int) bool) (_ []block, _ []incompleteBlock, warnings []*Finding) {
	var blocks []block
	var incompleteBlocks []incompleteBlock

	type startLine struct {
		index int
		line  string
	}
	// starts is a stack of startLines.
	var starts []startLine
	// nestedBlocks by nesting level. nestedBlocks[0] is the slice of blocks that
	// are nested under the current top-level block.
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

			commentMarker, options, _ := strings.Cut(start.line, f.startDirective)
			opts, optionWarnings := parseBlockOptions(commentMarker, options, f.defaultOptions)
			for _, warn := range optionWarnings {
				warnings = append(warnings, finding(filename, start.index+offset, start.index+offset, warn.Error()))
			}

			start.index += opts.SkipLines
			if start.index > endIndex {
				continue
			}

			// Top-level keep-sorted directives have depth 0. Nested keep-sorted
			// directives will have depth >= 1 based on how deep it is.
			depth := len(starts)
			block := block{
				metadata: blockMetadata{
					startDirective: f.startDirective,
					endDirective:   f.endDirective,
					opts:           opts,
				},
				start: start.index + offset,
				end:   endIndex + offset,
				lines: lines[start.index+1 : endIndex],
			}
			// For example, consider depth=0:
			// If we just finished a top-level block and there are first-level nested
			// blocks present, we need to remove those from nestedBlocks and include
			// them on this block.
			// It isn't possible for len(nestedBlocks) to be > depth+1:
			// At depth n, n != 0, we increase the length of nestedBlocks to be n.
			// At depth m=n-1, the length of nestedBlocks will initially be n=m+1 (the assertion from above)
			// and then we trim that down to be length m when we add the nested blocks
			// to the current block.
			if len(nestedBlocks) == depth+1 {
				block.nestedBlocks = nestedBlocks[depth]
				nestedBlocks = nestedBlocks[0:depth]
			}
			if depth == 0 {
				// Top-level blocks get returned.
				// Nested blocks are returned via their top-level block.
				blocks = append(blocks, block)
			} else {
				// Otherwise, the current block appears to be nested. Add it to nestedBlocks.
				for len(nestedBlocks) < depth {
					nestedBlocks = append(nestedBlocks, nil)
				}
				nestedBlocks[depth-1] = append(nestedBlocks[depth-1], block)
			}
			// Invariant: len(nestedBlocks) == depth
		}
	}
	if len(starts) > 0 {
		for _, st := range starts {
			incompleteBlocks = append(incompleteBlocks, incompleteBlock{st.index + offset, startDirective})
		}
		// There were some unfinished start directives. They might've caused some
		// blocks to be incorrectly considered nested.
		for _, nested := range nestedBlocks {
			blocks = append(blocks, nested...)
		}
	}

	return blocks, incompleteBlocks, warnings
}

// sorted returns a slice which represents the correct sorting of b.lines.
// If b.lines is already correctly sorted, we will return b.lines, true.
func (b block) sorted() (sorted []string, alreadySorted bool) {
	alreadySorted = true

	// Sort the nested blocks first so that their changes are visible to the
	// outer block.
	type nestedResult struct {
		lines         []string
		alreadySorted bool
	}
	var nestedResults []nestedResult
	for _, n := range b.nestedBlocks {
		lines, already := n.sorted()
		if !already {
			alreadySorted = false
		}
		nestedResults = append(nestedResults, nestedResult{lines, already})
	}

	lines := b.lines
	if !alreadySorted {
		var lineChunks [][]string
		// The total number of lines in lineChunks.
		var numLines int
		// Our current position within lines.
		var cursor int
		for i, nested := range b.nestedBlocks {
			res := nestedResults[i]
			if res.alreadySorted {
				// This nested block was already sorted. Its content in lines is already
				// correct. We will add this block to lineChunks either as an unchanged
				// prefix to a changed nested block, or as the remainder of lines if there
				// are no more changed nested blocks.
				continue
			}

			offset := nested.start - b.start
			// Unchanged prefix of lines.
			lineChunks = append(lineChunks, lines[cursor:offset])
			numLines += offset - cursor
			// The piece of the nested block that changed.
			lineChunks = append(lineChunks, res.lines)
			numLines += len(res.lines)

			// Advance cursor to the end of the nested block within lines.
			cursor = offset + len(nested.lines)
		}

		if rem := lines[cursor:]; len(rem) > 0 {
			// Are there any lines remaining in lines after handing all the nested
			// blocks?
			lineChunks = append(lineChunks, rem)
			numLines += len(rem)
		}

		// See above for the scary comment telling us not to modify b.lines directly.
		lines = make([]string, 0, numLines)
		for _, chunk := range lineChunks {
			lines = append(lines, chunk...)
		}
	}

	groups := groupLines(lines, b.metadata)
	log.Printf("Previous %d groups were for block at index %d are (options %v)", len(groups), b.start, b.metadata.opts)
	trimTrailingComma := handleTrailingComma(groups)

	numNewlines := int(b.metadata.opts.NewlineSeparated)
	wasNewlineSeparated := true
	if b.metadata.opts.NewlineSeparated > 0 {
		wasNewlineSeparated = isNewlineSeparated(groups, numNewlines)
		var withoutNewlines []*lineGroup
		for _, lg := range groups {
			if !isAllEmpty(lg) {
				withoutNewlines = append(withoutNewlines, lg)
			}
		}
		groups = withoutNewlines
	}

	removedDuplicate := false
	if b.metadata.opts.RemoveDuplicates {
		seen := map[string]bool{}
		var deduped []*lineGroup
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

	if alreadySorted && wasNewlineSeparated && !removedDuplicate && slices.IsSortedFunc(groups, compareLineGroups) {
		trimTrailingComma(groups)
		return lines, true
	}

	slices.SortStableFunc(groups, compareLineGroups)

	trimTrailingComma(groups)

	if b.metadata.opts.NewlineSeparated > 0 {
		var separated []*lineGroup
		newline := &lineGroup{lineGroupContent: lineGroupContent{lines: make([]string, numNewlines)}}
		for _, lg := range groups {
			if separated != nil {
				separated = append(separated, newline)
			}
			separated = append(separated, lg)
		}
		groups = separated
	}

	l := make([]string, 0, len(lines))
	for _, g := range groups {
		l = append(l, g.allLines()...)
	}
	return l, false
}

// isNewlineSeparated determines if the given lineGroups are already NewlineSeparated,
// and are separated by groups containing exactly numNewlines empty lines.
//
// e.g.
// non-empty group
// newline group (repeated numNewlines times)
// non-empty group
// newline group (repeated numNewlines times)
// .
// .
// .
// non-empty group
func isNewlineSeparated(gs []*lineGroup, numNewlines int) bool {
	if len(gs) == 0 {
		return true
	}
	if isAllEmpty(gs[len(gs)-1]) {
		return false
	}

	i := 0
	for i < len(gs) {
		// Expect a data group (a group with at least one non-empty line or a comment)
		if isAllEmpty(gs[i]) {
			return false // Expected data group, found an empty group without comments
		}
		i++

		// If this is the last group, we are done.
		if i == len(gs) {
			break
		}

		// Expect a separator of numNewlines empty lines.
		emptyLinesCount := 0
		// Sum up consecutive empty line groups
		for i < len(gs) && isAllEmpty(gs[i]) {
			emptyLinesCount += len(gs[i].lines)
			i++
		}

		if emptyLinesCount != numNewlines {
			return false // Incorrect number of newlines in the separator
		}
	}

	return true
}

func isAllEmpty(lg *lineGroup) bool {
	if len(lg.comment) > 0 {
		return false
	}
	for _, line := range lg.lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}

// handleTrailingComma handles the special case that all lines of a sorted segment are terminated
// by a comma except for the final element; in this case, we add a ',' to the
// last linegroup and strip it again after sorting.
func handleTrailingComma(lgs []*lineGroup) (trimTrailingComma func([]*lineGroup)) {
	var dataGroups []*lineGroup
	for _, lg := range lgs {
		if len(lg.lines) > 0 {
			dataGroups = append(dataGroups, lg)
		}
	}

	if n := len(dataGroups); n > 1 && allHaveSuffix(dataGroups[0:n-1], ",") && !dataGroups[n-1].hasSuffix(",") {
		dataGroups[n-1].append(",")

		return func(lgs []*lineGroup) {
			for i := len(lgs) - 1; i >= 0; i-- {
				if len(lgs[i].lines) > 0 {
					lgs[i].trimSuffix(",")
					return
				}
			}
		}
	}

	return func([]*lineGroup) {}
}

func allHaveSuffix(lgs []*lineGroup, s string) bool {
	for _, lg := range lgs {
		if !lg.hasSuffix(s) {
			return false
		}
	}
	return true
}
