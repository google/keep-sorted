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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
)

var (
	defaultOptions = blockOptions{
		Lint:             true,
		Group:            true,
		IgnorePrefixes:   nil,
		Numeric:          false,
		StickyComments:   true,
		StickyPrefixes:   map[string]bool{"//": true},
		RemoveDuplicates: true,
		PrefixOrder:      nil,
		Block:            false,
		NewlineSeparated: false,
		CaseSensitive:    true,
		commentMarker:    "//",
	}
)

func defaultOptionsWith(f func(*blockOptions)) blockOptions {
	opts := defaultOptions
	f(&opts)
	return opts
}

func TestFix(t *testing.T) {
	for _, tc := range []struct {
		name string

		in string

		want             string
		wantAlreadyFixed bool
	}{
		{
			name: "empty",

			in: `
// keep-sorted-test start
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
// keep-sorted-test end`,
			wantAlreadyFixed: true,
		},
		{
			name: "already sorted",

			in: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,
			wantAlreadyFixed: true,
		},
		{
			name: "unordered block",

			in: `
// keep-sorted-test start
2
1
3
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,
		},
		{
			name: "unmatched start",

			in: `
// keep-sorted-test start
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,
		},
		{
			name: "unmatched end",

			in: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end
`,
		},
		{
			name: "multiple fixes",

			in: `
// keep-sorted-test end
// keep-sorted-test start
// keep-sorted-test start
2
1
3
// keep-sorted-test end
// keep-sorted-test start
foo
bar
baz
// keep-sorted-test end`,

			want: `

// keep-sorted-test start
1
2
3
// keep-sorted-test end
// keep-sorted-test start
bar
baz
foo
// keep-sorted-test end`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, gotAlreadyFixed := New("keep-sorted-test").Fix(tc.in, nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Fix diff (-want +got):\n%s", diff)
			}
			if gotAlreadyFixed != tc.wantAlreadyFixed {
				t.Errorf("alreadyFixed diff: got %t want %t", gotAlreadyFixed, tc.wantAlreadyFixed)
			}
		})
	}
}

func TestFindings(t *testing.T) {
	filename := "test"
	for _, tc := range []struct {
		name string

		in                 string
		modifiedLines      []int
		considerLintOption bool

		want []*Finding
	}{
		{
			name: "already sorted",

			in: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,

			want: nil,
		},
		{
			name: "not sorted",

			in: `
// keep-sorted-test start
2
1
3
// keep-sorted-test end`,

			want: []*Finding{finding(filename, 3, 5, errorUnordered, "1\n2\n3\n")},
		},
		{
			name: "mismatched start",

			in: `
// keep-sorted-test start`,

			want: []*Finding{finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test end' line", "")},
		},
		{
			name: "mismatched end",

			in: `
// keep-sorted-test end`,

			want: []*Finding{finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test start' line", "")},
		},
		{
			name: "multiple findings",

			in: `
// keep-sorted-test end
// keep-sorted-test start
// keep-sorted-test start
2
1
3
// keep-sorted-test end
// keep-sorted-test start
foo
bar
baz
// keep-sorted-test end
`,

			want: []*Finding{
				finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test start' line", ""),
				finding(filename, 3, 3, "This instruction doesn't have matching 'keep-sorted-test end' line", ""),
				finding(filename, 5, 7, errorUnordered, "1\n2\n3\n"),
				finding(filename, 10, 12, errorUnordered, "bar\nbaz\nfoo\n"),
			},
		},
		{
			name: "modified lines",

			in: `
// keep-sorted-test start
2
1
3
// keep-sorted-test end
// keep-sorted-test start
foo
bar
baz
// keep-sorted-test end`,
			modifiedLines: []int{3},

			want: []*Finding{finding(filename, 3, 5, errorUnordered, "1\n2\n3\n")},
		},
		{
			name: "lint=no",

			in: `
// keep-sorted-test start lint=no
2
1
3
// keep-sorted-test end`,
			considerLintOption: true,

			want: nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mod []LineRange
			if tc.modifiedLines != nil {
				for _, l := range tc.modifiedLines {
					mod = append(mod, LineRange{l, l})
				}
			}
			got := New("keep-sorted-test").findings(filename, strings.Split(tc.in, "\n"), mod, tc.considerLintOption)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("Findings diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCreatingBlocks(t *testing.T) {
	for _, tc := range []struct {
		name string

		in      string
		include func(start, end int) bool

		wantBlocks           []block
		wantIncompleteBlocks []incompleteBlock
	}{
		{
			name: "multiple blocks",

			in: `
foo
bar
// keep-sorted-test start
c
b
a
// keep-sorted-test end
baz
// keep-sorted-test start
1
2
3
// keep-sorted-test end
dog
cat`,

			wantBlocks: []block{
				{
					opts:  defaultOptions,
					start: 3,
					end:   7,
					lines: []string{
						"c",
						"b",
						"a",
					},
				},
				{
					opts:  defaultOptions,
					start: 9,
					end:   13,
					lines: []string{
						"1",
						"2",
						"3",
					},
				},
			},
		},
		{
			name: "incomplete blocks",

			in: `
// keep-sorted-test start
foo
bar
// keep-sorted-test start
baz
// keep-sorted-test end
dog
// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					opts:  defaultOptions,
					start: 4,
					end:   6,
					lines: []string{
						"baz",
					},
				},
			},
			wantIncompleteBlocks: []incompleteBlock{
				{1, startDirective},
				{8, endDirective},
			},
		},
		{
			name: "filtered blocks",

			in: `
foo
bar
// keep-sorted-test start
c
b
a
// keep-sorted-test end
baz
// keep-sorted-test start
1
2
3
// keep-sorted-test end
dog
cat`,
			include: func(start, end int) bool {
				return start < 4 && 4 < end
			},

			wantBlocks: []block{
				{
					opts:  defaultOptions,
					start: 3,
					end:   7,
					lines: []string{
						"c",
						"b",
						"a",
					},
				},
			},
		},
		{
			name: "trailing newlines",

			in: `
// keep-sorted-test start

1
2
3



// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					opts:  defaultOptions,
					start: 1,
					end:   6,
					lines: []string{
						"",
						"1",
						"2",
						"3",
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.include == nil {
				tc.include = func(start, end int) bool { return true }
			}

			gotBlocks, gotIncompleteBlocks := New("keep-sorted-test").newBlocks(strings.Split(tc.in, "\n"), 0, tc.include)
			if diff := cmp.Diff(tc.wantBlocks, gotBlocks, cmp.AllowUnexported(block{}), cmp.AllowUnexported(blockOptions{})); diff != "" {
				t.Errorf("blocks diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantIncompleteBlocks, gotIncompleteBlocks, cmp.AllowUnexported(incompleteBlock{})); diff != "" {
				t.Errorf("incompleteBlocks diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLineSorting(t *testing.T) {
	for _, tc := range []struct {
		name string

		opts blockOptions
		in   []string

		want              []string
		wantAlreadySorted bool
	}{
		{
			name: "nothing to sort",

			opts: defaultOptions,
			in:   []string{},

			want:              []string{},
			wantAlreadySorted: true,
		},
		{
			name: "already sorted",

			opts: defaultOptions,
			in: []string{
				"Bar",
				"Baz",
				"Foo",
				"Qux",
			},

			want: []string{
				"Bar",
				"Baz",
				"Foo",
				"Qux",
			},
			wantAlreadySorted: true,
		},
		{
			name: "already sorted -- except for duplicate",

			opts: defaultOptions,
			in: []string{
				"Bar",
				"Bar",
				"Foo",
			},

			want: []string{
				"Bar",
				"Foo",
			},
			wantAlreadySorted: false,
		},
		{
			name: "already sorted -- newline separated",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.NewlineSeparated = true
			}),
			in: []string{
				"Bar",
				"",
				"Baz",
				"",
				"Foo",
			},

			want: []string{
				"Bar",
				"",
				"Baz",
				"",
				"Foo",
			},
			wantAlreadySorted: true,
		},
		{
			name: "already sorted -- except for newline separated",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.NewlineSeparated = true
			}),
			in: []string{
				"Bar",
				"Baz",
				"Foo",
			},

			want: []string{
				"Bar",
				"",
				"Baz",
				"",
				"Foo",
			},
			wantAlreadySorted: false,
		},
		{
			name: "simple sorting",

			opts: defaultOptions,
			in: []string{
				"Baz",
				"Foo",
				"Bar",
				"Qux",
			},

			want: []string{
				"Bar",
				"Baz",
				"Foo",
				"Qux",
			},
		},
		{
			name: "comment only block",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.StickyComments = true
				opts.StickyPrefixes = map[string]bool{"//": true}
			}),
			in: []string{
				"2",
				"1",
				"// trailing comment",
			},

			want: []string{
				"1",
				"2",
				"// trailing comment",
			},
		},
		{
			name: "prefix",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.PrefixOrder = []string{"INIT_", "", "FINAL_"}
			}),
			in: []string{
				// keep-sorted start prefix_order=
				"DO_SOMETHING_WITH_BAR",
				"DO_SOMETHING_WITH_FOO",
				"FINAL_BAR",
				"FINAL_FOO",
				"INIT_BAR",
				"INIT_FOO",
				// keep-sorted end
			},

			want: []string{
				"INIT_BAR",
				"INIT_FOO",
				"DO_SOMETHING_WITH_BAR",
				"DO_SOMETHING_WITH_FOO",
				"FINAL_BAR",
				"FINAL_FOO",
			},
		},
		{
			name: "remove duplicates -- by default",

			opts: defaultOptions,
			in: []string{
				"foo",
				"foo",
				"bar",
			},

			want: []string{
				"bar",
				"foo",
			},
		},
		{
			name: "remove duplicates -- considers comments",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.RemoveDuplicates = true
				opts.StickyComments = true
				opts.StickyPrefixes = map[string]bool{"//": true}
			}),
			in: []string{
				"// comment 1",
				"foo",
				"// comment 2",
				"foo",
				"// comment 1",
				"foo",
				"bar",
			},

			want: []string{
				"bar",
				"// comment 1",
				"foo",
				"// comment 2",
				"foo",
			},
		},
		{
			name: "remove duplicates -- ignores trailing commas",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.RemoveDuplicates = true
			}),
			in: []string{
				"foo,",
				"bar,",
				"bar",
			},

			want: []string{
				"bar,",
				"foo",
			},
		},
		{
			name: "remove duplicates -- ignores trailing commas -- removes comma if last element",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.RemoveDuplicates = true
			}),
			in: []string{
				"foo,",
				"foo,",
				"bar",
			},

			want: []string{
				"bar,",
				"foo",
			},
		},
		{
			name: "remove duplicates -- ignores trailing commas -- removes comma if only element",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.RemoveDuplicates = true
			}),
			in: []string{
				"foo,",
				"foo",
			},

			want: []string{
				"foo",
			},
		},
		{
			name: "remove duplicates -- keep",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.RemoveDuplicates = false
			}),
			in: []string{
				"foo",
				"foo",
				"bar",
			},

			want: []string{
				"bar",
				"foo",
				"foo",
			},
		},
		{
			name: "trailing commas",

			opts: defaultOptions,
			in: []string{
				"foo,",
				"baz,",
				"bar",
			},

			want: []string{
				"bar,",
				"baz,",
				"foo",
			},
		},
		{
			name: "ignore prefixes",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.IgnorePrefixes = []string{"fs.setBoolFlag", "fs.setIntFlag"}
			}),
			in: []string{
				// keep-sorted start ignore_prefixes=
				`fs.setBoolFlag("paws_with_cute_toebeans", true)`,
				`fs.setBoolFlag("whiskered_adorable_dog", true)`,
				`fs.setIntFlag("pretty_whiskered_kitten", 6)`,
				// keep-sorted end
			},

			want: []string{
				`fs.setBoolFlag("paws_with_cute_toebeans", true)`,
				`fs.setIntFlag("pretty_whiskered_kitten", 6)`,
				`fs.setBoolFlag("whiskered_adorable_dog", true)`,
			},
		},
		{
			name: "case insensitive",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.CaseSensitive = false
			}),
			in: []string{
				// keep-sorted start case=yes
				"Bravo",
				"Echo",
				"delta",
				// keep-sorted end
			},

			want: []string{
				"Bravo",
				"delta",
				"Echo",
			},
		},
		{
			name: "numeric",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Numeric = true
			}),
			in: []string{
				// keep-sorted start numeric=no
				"PROGRESS_100_PERCENT",
				"PROGRESS_10_PERCENT",
				"PROGRESS_1_PERCENT",
				"PROGRESS_50_PERCENT",
				"PROGRESS_5_PERCENT",
				// keep-sorted end
			},

			want: []string{
				"PROGRESS_1_PERCENT",
				"PROGRESS_5_PERCENT",
				"PROGRESS_10_PERCENT",
				"PROGRESS_50_PERCENT",
				"PROGRESS_100_PERCENT",
			},
		},
		{
			name: "multiple transforms",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.IgnorePrefixes = []string{"R2D2", "C3PO", "R4"}
				opts.Numeric = true
			}),
			in: []string{
				// keep-sorted start ignore_prefixes= numeric=no
				"C3PO_ARM_L",
				"C3PO_ARM_R",
				"C3PO_HEAD",
				"R2D2_BOLTS_10_MM",
				"R2D2_BOLTS_5_MM",
				"R2D2_PROJECTOR",
				"R4_MOTIVATOR",
				// keep-sorted end
			},

			want: []string{
				"C3PO_ARM_L",
				"C3PO_ARM_R",
				"R2D2_BOLTS_5_MM",
				"R2D2_BOLTS_10_MM",
				"C3PO_HEAD",
				"R4_MOTIVATOR",
				"R2D2_PROJECTOR",
			},
		},
		{
			name: "newline separated",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.NewlineSeparated = true
			}),
			in: []string{
				"B",
				"",
				"C",
				"A",
			},

			want: []string{
				"A",
				"",
				"B",
				"",
				"C",
			},
		},
		{
			name: "newline separated -- empty",

			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.NewlineSeparated = true
			}),
			in: []string{},

			want:              []string{},
			wantAlreadySorted: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, gotAlreadySorted := block{lines: tc.in, opts: tc.opts}.sorted()
			if gotAlreadySorted != tc.wantAlreadySorted {
				t.Errorf("alreadySorted mismatch: got %t want %t", gotAlreadySorted, tc.wantAlreadySorted)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("sorted() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLineGrouping(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts blockOptions

		want []lineGroup
	}{
		{
			name: "simple",
			opts: defaultOptions,

			want: []lineGroup{
				{nil, []string{"foo"}},
				{nil, []string{"bar"}},
			},
		},
		{
			name: "sticky comments",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.StickyComments = true
				opts.StickyPrefixes = map[string]bool{"//": true}
			}),

			want: []lineGroup{
				{
					[]string{
						"// comment 1",
						"// comment 2",
					},
					[]string{
						"foo",
					},
				},
				{
					[]string{
						"// comment 3",
					}, []string{
						"bar",
					},
				},
			},
		},
		{
			name: "comment only group",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.StickyComments = true
				opts.StickyPrefixes = map[string]bool{"//": true}
			}),

			want: []lineGroup{
				{
					[]string{
						"// comment 1",
					},
					[]string{
						"foo",
					},
				},
				{
					[]string{
						"// trailing comment",
					},
					nil,
				},
			},
		},
		{
			name: "group",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Group = true
			}),

			want: []lineGroup{
				{nil, []string{
					"  foo",
					"    bar",
				}},
				{nil, []string{
					"  baz",
				}},
			},
		},
		{
			name: "Group_UnindentedNewlines",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Group = true
			}),

			want: []lineGroup{
				{nil, []string{
					"  foo",
					"", // Since the next non-empty line has the correct indent.
					"    bar",
				}},
				{nil, []string{
					"", // Next non-empty line has the wrong indent.
				}},
				{nil, []string{
					"  baz",
				}},
				{nil, []string{
					"", // There is no next non-empty line.
				}},
			},
		},
		{
			name: "block -- brackets",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					"foo(",
					"abcd",
					"efgh",
					")",
				}},
				{nil, []string{
					"bar()",
				}},
				{nil, []string{
					"baz",
				}},
			},
		},
		{
			name: "block -- quotes",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					`foo"`,
					"abcd",
					"efgh",
					`"`,
				}},
				{nil, []string{
					`bar""`,
				}},
				{nil, []string{
					"baz",
				}},
			},
		},
		{
			name: "block -- escaped quote",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					`foo"`,
					`\"abcd`,
					`efgh\"`,
					`"`,
				}},
				{nil, []string{
					`bar""`,
				}},
				{nil, []string{
					"baz",
				}},
			},
		},
		{
			name: "block -- ignores quotes within quotes",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					`foo"`,
					`ab'cd`,
					`efgh`,
					`"`,
				}},
				{nil, []string{
					"bar'`'",
				}},
				{nil, []string{
					"baz",
				}},
			},
		},
		{
			name: "block -- ignores braces within quotes",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					`foo"`,
					`ab(cd`,
					`ef[gh`,
					`"`,
				}},
				{nil, []string{
					`foo"`,
					`ab)cd`,
					`ef]gh`,
					`"`,
				}},
			},
		},
		{
			name: "block -- ignores special characters within full-line comments",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
				opts.StickyPrefixes["//"] = true
			}),

			want: []lineGroup{
				{nil, []string{
					"foo(",
					"// ignores quotes in a comment '",
					"// ignores parenthesis in a comment )",
					"abcd",
					")",
				}},
				{nil, []string{
					"'string literal",
					"// does not ignore quotes here '",
				}},
				{nil, []string{
					"abcd'",
				}},
			},
		},
		{
			name: "block -- ignores special characters within trailing comments",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
				opts.StickyPrefixes["//"] = true
			}),

			want: []lineGroup{
				{nil, []string{
					"foo(// ignores quotes in a comment '",
					"abcd // ignores parenthesis in a comment )",
					")",
				}},
				{nil, []string{
					"'string literal",
					"with line break // does not ignore quotes here '",
				}},
				{nil, []string{
					`"another string literal`,
					`with line break // does not ignore quote " here`,
				}},
				{nil, []string{
					`"abcd"`,
				}},
			},
		},
		{
			name: "block -- triple quotes",
			opts: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
			}),

			want: []lineGroup{
				{nil, []string{
					`"""documentation`,
					"ab'cd",
					"efgh",
					"abcd",
					`"""`}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in []string
			for _, lg := range tc.want {
				in = append(in, lg.comment...)
				in = append(in, lg.lines...)
			}

			got := groupLines(in, tc.opts)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(lineGroup{})); diff != "" {
				t.Errorf("groupLines mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBlockOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string

		want    blockOptions
		wantErr string
	}{
		{
			name: "default options",
			in:   "// keep-sorted-test",

			want: defaultOptions,
		},
		{
			name: "simple switch",
			in:   "// keep-sorted-test lint=no",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.Lint = false
			}),
		},
		{
			name: "item list",
			in:   "// keep-sorted-test prefix_order=a,b,c,d",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.PrefixOrder = []string{"a", "b", "c", "d"}
			}),
		},
		{
			name: "item set",
			in:   "keep-sorted-test sticky_prefixes=a,b,c,d",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.StickyPrefixes = map[string]bool{"a": true, "b": true, "c": true, "d": true}
				opts.commentMarker = ""
			}),
		},
		{
			name: "ignore_prefixes",
			in:   "// keep-sorted-test ignore_prefixes=a,b,c,d",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.IgnorePrefixes = []string{"a", "b", "c", "d"}
			}),
		},
		{
			name: "ignore_prefixes checks longest prefixes first",
			in:   "// keep-sorted-test ignore_prefixes=DoSomething(,DoSomething({",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.IgnorePrefixes = []string{"DoSomething({", "DoSomething("}
			}),
		},
		{
			name: "option in a trailing comment",
			in:   "// keep-sorted-test block=yes  # lint=no",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.Block = true
				opts.Lint = false
			}),
		},
		{
			name: "error doesn't stop parsing",
			in:   "// keep-sorted-test lint=yep case=no",

			want: defaultOptionsWith(func(opts *blockOptions) {
				opts.Lint = true // The default value should not change.
				opts.CaseSensitive = false
			}),
			wantErr: `option "lint" has unknown value "yep"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New("keep-sorted-test").parseBlockOptions(tc.in)
			if err != nil {
				if tc.wantErr == "" {
					t.Errorf("parseBlockOptions(%q) = %v", tc.in, err)
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("parseBlockOptions(%q) = %v, expected to contain %q", tc.in, err, tc.wantErr)
				}
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(blockOptions{})); diff != "" {
				t.Errorf("parseBlockOptions(%q) mismatch (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}
