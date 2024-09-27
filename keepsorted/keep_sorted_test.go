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
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// initZerolog initializes zerolog to log as part of the test.
// It returns a function that restores zerolog to its state before this function was called.
func initZerolog(t testing.TB) {
	oldLogger := log.Logger
	log.Logger = log.Output(zerolog.NewTestWriter(t))
	t.Cleanup(func() { log.Logger = oldLogger })
}

func defaultMetadataWith(opts blockOptions) blockMetadata {
	return blockMetadata{
		startDirective: "keep-sorted-test start",
		endDirective:   "keep-sorted-test end",
		opts:           opts,
	}
}

func defaultMetadataWithCommentMarker(marker string) blockMetadata {
	var opts blockOptions
	opts.setCommentMarker(marker)
	return defaultMetadataWith(opts)
}

func TestFix(t *testing.T) {
	for _, tc := range []struct {
		name string

		in string

		want             string
		wantAlreadyFixed bool
	}{
		{
			name: "Empty",

			in: `
// keep-sorted-test start
// keep-sorted-test end`,

			want: `
// keep-sorted-test start
// keep-sorted-test end`,
			wantAlreadyFixed: true,
		},
		{
			name: "AlreadySorted",

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
			name: "UnorderedBlock",

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
			name: "UnmatchedStart",

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
			name: "UnmatchedEnd",

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
			name: "MultipleFixes",

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
			initZerolog(t)
			got, gotAlreadyFixed, gotWarnings := New("keep-sorted-test", BlockOptions{}).Fix("unused-filename", tc.in, nil)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Fix diff (-want +got):\n%s", diff)
			}
			if gotAlreadyFixed != tc.wantAlreadyFixed {
				t.Errorf("alreadyFixed diff: got %t want %t", gotAlreadyFixed, tc.wantAlreadyFixed)
			}
			if len(gotWarnings) != 0 {
				t.Errorf("Fix returned warnings, expected none:\n%v", gotWarnings)
			}
		})
	}
}

func TestFindings(t *testing.T) {
	filename := "test"
	for _, tc := range []struct {
		name string

		in            string
		modifiedLines []int

		want []*Finding
	}{
		{
			name: "AlreadySorted",

			in: `
// keep-sorted-test start
1
2
3
// keep-sorted-test end`,

			want: nil,
		},
		{
			name: "NotSorted",

			in: `
// keep-sorted-test start
2
1
3
// keep-sorted-test end`,

			want: []*Finding{finding(filename, 3, 5, errorUnordered, replacement(3, 5, "1\n2\n3\n"))},
		},
		{
			name: "SkipLines",

			in: `
// keep-sorted-test start skip_lines=2
5
4
3
2
1
// keep-sorted-test end`,

			want: []*Finding{finding(filename, 5, 7, errorUnordered, replacement(5, 7, "1\n2\n3\n"))},
		},
		{
			name: "MismatchedStart",

			in: `
// keep-sorted-test start`,

			want: []*Finding{finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test end' line", replacement(2, 2, ""))},
		},
		{
			name: "MismatchedEnd",

			in: `
// keep-sorted-test end`,

			want: []*Finding{finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test start' line", replacement(2, 2, ""))},
		},
		{
			name: "MultipleFindings",

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
				finding(filename, 2, 2, "This instruction doesn't have matching 'keep-sorted-test start' line", replacement(2, 2, "")),
				finding(filename, 3, 3, "This instruction doesn't have matching 'keep-sorted-test end' line", replacement(3, 3, "")),
				finding(filename, 5, 7, errorUnordered, replacement(5, 7, "1\n2\n3\n")),
				finding(filename, 10, 12, errorUnordered, replacement(10, 12, "bar\nbaz\nfoo\n")),
			},
		},
		{
			name: "ModifiedLines",

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

			want: []*Finding{finding(filename, 3, 5, errorUnordered, replacement(3, 5, "1\n2\n3\n"))},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initZerolog(t)
			var mod []LineRange
			if tc.modifiedLines != nil {
				for _, l := range tc.modifiedLines {
					mod = append(mod, LineRange{l, l})
				}
			}
			got := New("keep-sorted-test", BlockOptions{}).findings(filename, strings.Split(tc.in, "\n"), mod)
			if diff := cmp.Diff(tc.want, got); diff != "" {
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
		wantWarnings         []string
	}{
		{
			name: "MultipleBlocks",

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
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    3,
					end:      7,
					lines: []string{
						"c",
						"b",
						"a",
					},
				},
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    9,
					end:      13,
					lines: []string{
						"1",
						"2",
						"3",
					},
				},
			},
		},
		{
			name: "IncompleteBlocks",

			in: `
// keep-sorted-test end
// keep-sorted-test start
foo
bar
// keep-sorted-test start
baz
// keep-sorted-test end
dog
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    5,
					end:      7,
					lines: []string{
						"baz",
					},
				},
			},
			wantIncompleteBlocks: []incompleteBlock{
				{1, endDirective},
				{2, startDirective},
			},
		},
		{
			name: "FilteredBlocks",

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
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    3,
					end:      7,
					lines: []string{
						"c",
						"b",
						"a",
					},
				},
			},
		},
		{
			name: "TrailingNewlines",

			in: `
// keep-sorted-test start

1
2
3



// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    1,
					end:      6,
					lines: []string{
						"",
						"1",
						"2",
						"3",
					},
				},
			},
		},
		{
			name: "NestedBlocks",

			in: `
// keep-sorted-test start
a
b
c
// keep-sorted-test start
d
e
f
// keep-sorted-test end
g
h
i
// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    1,
					end:      13,
					lines: []string{
						"a",
						"b",
						"c",
						"// keep-sorted-test start",
						"d",
						"e",
						"f",
						"// keep-sorted-test end",
						"g",
						"h",
						"i",
					},
					nestedBlocks: []block{
						{
							metadata: defaultMetadataWithCommentMarker("//"),
							start:    5,
							end:      9,
							lines: []string{
								"d",
								"e",
								"f",
							},
						},
					},
				},
			},
		},
		{
			name: "NestedBlocks_DeeplyNested",

			in: `
// keep-sorted-test start
0.1
0.2
0.3
// keep-sorted-test start
1.1
1.2
1.3
// keep-sorted-test start
2.1
2.2
2.3
// keep-sorted-test start
3.1
3.2
3.3
// keep-sorted-test end // 0:1:2:3
2.4
2.5
2.6
// keep-sorted-test end // 0:1:2
// keep-sorted-test start
4.1
4.2
4.3
// keep-sorted-test end // 0:1:4
1.4
1.5
1.6
// keep-sorted-test end // 0:1
0.4
0.5
0.6
// keep-sorted-test end // 0
// keep-sorted-test start
5.1
5.2
5.3
// keep-sorted-test end // 5
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    1,
					end:      34,
					lines: []string{
						"0.1",
						"0.2",
						"0.3",
						"// keep-sorted-test start",
						"1.1",
						"1.2",
						"1.3",
						"// keep-sorted-test start",
						"2.1",
						"2.2",
						"2.3",
						"// keep-sorted-test start",
						"3.1",
						"3.2",
						"3.3",
						"// keep-sorted-test end // 0:1:2:3",
						"2.4",
						"2.5",
						"2.6",
						"// keep-sorted-test end // 0:1:2",
						"// keep-sorted-test start",
						"4.1",
						"4.2",
						"4.3",
						"// keep-sorted-test end // 0:1:4",
						"1.4",
						"1.5",
						"1.6",
						"// keep-sorted-test end // 0:1",
						"0.4",
						"0.5",
						"0.6",
					},
					nestedBlocks: []block{
						{
							metadata: defaultMetadataWithCommentMarker("//"),
							start:    5,
							end:      30,
							lines: []string{
								"1.1",
								"1.2",
								"1.3",
								"// keep-sorted-test start",
								"2.1",
								"2.2",
								"2.3",
								"// keep-sorted-test start",
								"3.1",
								"3.2",
								"3.3",
								"// keep-sorted-test end // 0:1:2:3",
								"2.4",
								"2.5",
								"2.6",
								"// keep-sorted-test end // 0:1:2",
								"// keep-sorted-test start",
								"4.1",
								"4.2",
								"4.3",
								"// keep-sorted-test end // 0:1:4",
								"1.4",
								"1.5",
								"1.6",
							},
							nestedBlocks: []block{
								{
									metadata: defaultMetadataWithCommentMarker("//"),
									start:    9,
									end:      21,
									lines: []string{
										"2.1",
										"2.2",
										"2.3",
										"// keep-sorted-test start",
										"3.1",
										"3.2",
										"3.3",
										"// keep-sorted-test end // 0:1:2:3",
										"2.4",
										"2.5",
										"2.6",
									},
									nestedBlocks: []block{
										{
											metadata: defaultMetadataWithCommentMarker("//"),
											start:    13,
											end:      17,
											lines: []string{
												"3.1",
												"3.2",
												"3.3",
											},
										},
									},
								},
								{
									metadata: defaultMetadataWithCommentMarker("//"),
									start:    22,
									end:      26,
									lines: []string{
										"4.1",
										"4.2",
										"4.3",
									},
								},
							},
						},
					},
				},
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    35,
					end:      39,
					lines: []string{
						"5.1",
						"5.2",
						"5.3",
					},
				},
			},
		},
		{
			name: "NestedBlocks_MissingEnds",

			in: `
// keep-sorted-test start
0
// keep-sorted-test start
1
// keep-sorted-test start
2
// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWithCommentMarker("//"),
					start:    5,
					end:      7,
					lines:    []string{"2"},
				},
			},
			wantIncompleteBlocks: []incompleteBlock{
				{1, startDirective},
				{3, startDirective},
			},
		},
		{
			name: "BadOption",
			in: `
// keep-sorted-test start foo=bar block=yes
0
1
2
// keep-sorted-test end
`,

			wantBlocks: []block{
				{
					metadata: defaultMetadataWith(func() blockOptions {
						var opts blockOptions
						opts.Block = true
						opts.setCommentMarker("//")
						return opts
					}()),
					start: 1,
					end:   5,
					lines: []string{"0", "1", "2"},
				},
			},
			wantWarnings: []string{`unrecognized option "foo"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initZerolog(t)
			if tc.include == nil {
				tc.include = func(start, end int) bool { return true }
			}

			gotBlocks, gotIncompleteBlocks, gotWarnings := New("keep-sorted-test", BlockOptions{}).newBlocks("unused-filename", strings.Split(tc.in, "\n"), 0, tc.include)
			if diff := cmp.Diff(tc.wantBlocks, gotBlocks, cmp.AllowUnexported(block{}, blockMetadata{}, blockOptions{})); diff != "" {
				t.Errorf("blocks diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantIncompleteBlocks, gotIncompleteBlocks, cmp.AllowUnexported(incompleteBlock{})); diff != "" {
				t.Errorf("incompleteBlocks diff (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(tc.wantWarnings, messages(gotWarnings), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("warnings diff (-want +got):\n%s", diff)
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
			name: "NothingToSort",

			in: []string{},

			want:              []string{},
			wantAlreadySorted: true,
		},
		{
			name: "AlreadySorted",

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
			name: "AlreadySorted_ExceptForDuplicate",

			opts: blockOptions{
				RemoveDuplicates: true,
			},
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
			name: "AlreadySorted_NewlineSeparated",

			opts: blockOptions{
				NewlineSeparated: true,
			},
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
			name: "AlreadySorted_ExceptForNewlineSorted",

			opts: blockOptions{
				NewlineSeparated: true,
			},
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
			name: "SimpleSorting",

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
			name: "CommentOnlyBlock",

			opts: func() blockOptions {
				opts := blockOptions{
					StickyComments: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),
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
			name: "Prefix",

			opts: blockOptions{
				PrefixOrder: []string{"INIT_", "", "FINAL_"},
			},
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
			name: "RemoveDuplicates_ByDefault",

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
			name: "RemoveDuplicates_ConsidersComments",

			opts: func() blockOptions {
				opts := blockOptions{
					RemoveDuplicates: true,
					StickyComments:   true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),
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
			name: "RemoveDuplicates_IgnoresTraliningCommas",

			opts: blockOptions{
				RemoveDuplicates: true,
			},
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
			name: "RemoveDuplicates_IgnoresTrailingCommas_RemovesCommaIfLastElement",

			opts: blockOptions{
				RemoveDuplicates: true,
			},
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
			name: "RemoveDuplicates_IgnoresTrailingCommas_RemovesCommaIfOnlyElement",

			opts: blockOptions{
				RemoveDuplicates: true,
			},
			in: []string{
				"foo,",
				"foo",
			},

			want: []string{
				"foo",
			},
		},
		{
			name: "RemoveDuplicates_Keep",

			opts: blockOptions{
				RemoveDuplicates: false,
			},
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
			name: "TrailingCommas",

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
			name: "IgnorePrefixes",

			opts: blockOptions{
				IgnorePrefixes: []string{"fs.setBoolFlag", "fs.setIntFlag"},
			},
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
			name: "CaseInsensitive",

			opts: blockOptions{
				CaseSensitive: false,
			},
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
			name: "Numeric",

			opts: blockOptions{
				Numeric: true,
			},
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
			name: "MultipleTransforms",

			opts: blockOptions{
				IgnorePrefixes: []string{"R2D2", "C3PO", "R4"},
				Numeric:        true,
			},
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
			name: "NewlineSeparated",

			opts: blockOptions{
				NewlineSeparated: true,
			},
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
			name: "NewlineSeparated_Empty",

			opts: blockOptions{
				NewlineSeparated: true,
			},
			in: []string{},

			want:              []string{},
			wantAlreadySorted: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initZerolog(t)
			got, gotAlreadySorted := block{lines: tc.in, metadata: defaultMetadataWith(tc.opts)}.sorted()
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

		// We set the input to be the concatenation of all the lineGroups.
		want []lineGroup
	}{
		{
			name: "Simple",

			want: []lineGroup{
				{nil, []string{"foo"}},
				{nil, []string{"bar"}},
			},
		},
		{
			name: "StickyComments",
			opts: func() blockOptions {
				opts := blockOptions{
					StickyComments: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),

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
			name: "CommentOnlyGroup",
			opts: func() blockOptions {
				opts := blockOptions{
					StickyComments: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),

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
			name: "Group",
			opts: blockOptions{
				Group: true,
			},

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
			name: "Group_Prefixes",
			opts: blockOptions{
				Group:         true,
				GroupPrefixes: map[string]bool{"and": true, "with": true},
			},

			want: []lineGroup{
				{nil, []string{
					"peanut butter",
					"and jelly",
				}},
				{nil, []string{
					"spaghetti",
					"with meatballs",
				}},
				{nil, []string{
					"hamburger",
					"  with lettuce",
					" and tomatoes",
					"and cheese",
				}},
				{nil, []string{
					"dogs and cats",
				}},
			},
		},
		{
			name: "Group_UnindentedNewlines",
			opts: blockOptions{
				Group: true,
			},

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
			name: "Group_NestedKeepSortedBlocksWithoutAnyIndentation",
			opts: func() blockOptions {
				opts := blockOptions{
					Group:          true,
					StickyComments: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),

			want: []lineGroup{
				{[]string{
					"// def",
				}, []string{
					"// keep-sorted-test start",
					"3",
					"1",
					"2",
					"// keep-sorted-test end",
				}},
				{[]string{
					"// abc",
				}, []string{
					"// keep-sorted-test start",
					"b",
					"c",
					"a",
					"// keep-sorted-test end",
				}},
			},
		},
		{
			name: "Block_Brackets",
			opts: blockOptions{
				Block: true,
			},

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
			name: "Block_Quotes",
			opts: blockOptions{
				Block: true,
			},

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
			name: "Block_EscapedQuote",
			opts: blockOptions{
				Block: true,
			},

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
			name: "Block_IgnoresQuotesWithinQuotes",
			opts: blockOptions{
				Block: true,
			},

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
			name: "Block_IgnoresBracesWithinQuotes",
			opts: blockOptions{
				Block: true,
			},

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
			name: "Block_IgnoresSpecialCharactersWithinFullLineComments",
			opts: func() blockOptions {
				opts := blockOptions{
					Block: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),

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
			name: "Block_IgnoresSpecialCharactersWithinTrailingComments",
			opts: func() blockOptions {
				opts := blockOptions{
					Block: true,
				}
				opts.setCommentMarker("//")
				return opts
			}(),

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
			name: "Block_TripleQuotes",
			opts: blockOptions{
				Block: true,
			},

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
			initZerolog(t)
			var in []string
			for _, lg := range tc.want {
				in = append(in, lg.comment...)
				in = append(in, lg.lines...)
			}

			got := groupLines(in, defaultMetadataWith(tc.opts))
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(lineGroup{})); diff != "" {
				t.Errorf("groupLines mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func messages(fs []*Finding) []string {
	ret := make([]string, len(fs))
	for i, f := range fs {
		ret[i] = f.Message
	}
	return ret
}
