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
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestBlockOptions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		commentMarker  string
		in             string
		defaultOptions blockOptions

		want    blockOptions
		wantErr string
	}{
		{
			name:           "DefaultOptions",
			in:             "",
			defaultOptions: defaultOptions,

			want: defaultOptions,
		},
		{
			name:          "CommentMarker",
			commentMarker: "//",
			in:            "",

			want: blockOptions{
				commentMarker: "//",
			},
		},
		{
			name:          "StickyComments",
			commentMarker: "//",
			in:            "sticky_comments=yes",

			want: blockOptions{
				StickyComments: true,
				StickyPrefixes: map[string]bool{"//": true},
				commentMarker:  "//",
			},
		},
		{
			name: "SimpleSwitch",
			in:   "group=yes",

			want: blockOptions{Group: true},
		},
		{
			name: "SkipLines",
			in:   "skip_lines=10",

			want: blockOptions{SkipLines: 10},
		},
		{
			name: "NewlineSeparated_Bool",
			in:   "newline_separated=yes",

			want: blockOptions{NewlineSeparated: 1},
		},
		{
			name: "NewlineSeparated_Int",
			in:   "newline_separated=10",

			want: blockOptions{NewlineSeparated: 10},
		},
		{
			name: "NewlineSeparated_Invalid",
			in:   "newline_separated=-1",

			wantErr: "newline_separated has invalid value: -1",
		},
		{
			name: "ErrorSkipLinesIsNegative",
			in:   "skip_lines=-1",

			wantErr: "skip_lines has invalid value: -1",
		},
		{
			name: "ItemList",
			in:   "prefix_order=a,b,c,d",

			want: blockOptions{
				PrefixOrder: []string{"a", "b", "c", "d"},
			},
		},
		{
			name:           "ItemList_YAML",
			in:             `prefix_order=[a, b, c d, 'e",\f']`,
			defaultOptions: blockOptions{AllowYAMLLists: true},

			want: blockOptions{
				AllowYAMLLists: true,
				PrefixOrder:    []string{"a", "b", "c d", `e",\f`},
			},
		},
		{
			name: "ItemSet",
			in:   "sticky_prefixes=a,b,c,d",

			want: blockOptions{
				StickyPrefixes: map[string]bool{
					"a": true,
					"b": true,
					"c": true,
					"d": true,
				},
			},
		},
		{
			name: "ItemSet_YAML",
			in:   `allow_yaml_lists=yes sticky_prefixes=[a, b, c d, 'e",\f']`,

			want: blockOptions{
				AllowYAMLLists: true,
				StickyPrefixes: map[string]bool{"a": true, "b": true, "c d": true, `e",\f`: true},
			},
		},
		{
			name: "ignore_prefixes",
			in:   "ignore_prefixes=a,b,c,d",

			want: blockOptions{
				IgnorePrefixes: []string{"a", "b", "c", "d"},
			},
		},
		{
			name: "ignore_prefixes_ChecksLongestPrefixesFirst",
			in:   "ignore_prefixes=DoSomething(,DoSomething({",

			want: blockOptions{
				IgnorePrefixes: []string{"DoSomething({", "DoSomething("},
			},
		},
		{
			name: "GroupPrefixesRequiresGrouping",
			in:   "group_prefixes=a,b,c group=no",

			wantErr: "group_prefixes may not be used with group=no",
		},
		{
			name:          "OptionInTrailingComment",
			commentMarker: "#",
			in:            "block=yes  # group=yes",

			want: blockOptions{
				Block:         true,
				Group:         true,
				commentMarker: "#",
			},
		},
		{
			name: "ErrorDoesNotStopParsing",
			in:   "group=nah case=no",
			defaultOptions: blockOptions{
				Group:         true,
				CaseSensitive: true,
			},

			want: blockOptions{
				Group:         true, // The default value should not change.
				CaseSensitive: false,
			},
			wantErr: `while parsing option "group": unrecognized bool value "nah"`,
		},
		{
			name:           "Regex",
			in:             `by_regex=['(?:abcd)', efg.*]`,
			defaultOptions: blockOptions{AllowYAMLLists: true},

			want: blockOptions{
				AllowYAMLLists: true,
				ByRegex: []ByRegexOption{
					{regexp.MustCompile("(?:abcd)"), nil}, {regexp.MustCompile("efg.*"), nil},
				},
			},
		},
		{
			name:           "RegexWithTemplate",
			in:             `by_regex=['.*', '\b(\d{2})/(\d{2})/(\d{4})\b': '${3}-${1}-${2}']`,
			defaultOptions: blockOptions{AllowYAMLLists: true},

			want: blockOptions{
				AllowYAMLLists: true,
				ByRegex: []ByRegexOption{
					{Pattern: regexp.MustCompile(`.*`)},
					{Pattern: regexp.MustCompile(`\b(\d{2})/(\d{2})/(\d{4})\b`),
						Template: &[]string{"${3}-${1}-${2}"}[0]},
				},
			},
		},
		{
			name: "OrderAsc",
			in:   "order=asc",
			want: blockOptions{Order: OrderAsc},
		},
		{
			name: "OrderDesc",
			in:   "order=desc",
			want: blockOptions{Order: OrderDesc},
		},
		{
			name:           "OrderInvalid",
			in:             "order=foo",
			defaultOptions: blockOptions{Order: OrderAsc},
			want:           blockOptions{Order: OrderAsc},
			wantErr:        `while parsing option "order": unrecognized order value "foo", expected 'asc' or 'desc'`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initZerolog(t)
			got, warns := parseBlockOptions(tc.commentMarker, tc.in, tc.defaultOptions)
			if err := errors.Join(warns...); err != nil {
				if tc.wantErr == "" {
					t.Errorf("parseBlockOptions(%q, %q) = %v", tc.commentMarker, tc.in, err)
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("parseBlockOptions(%q, %q) = %v, expected to contain %q", tc.commentMarker, tc.in, err, tc.wantErr)
				}
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(blockOptions{}), cmpRegexp); diff != "" {
				t.Errorf("parseBlockOptions(%q, %q) mismatch (-want +got):\n%s", tc.commentMarker, tc.in, diff)
			}

			if tc.wantErr == "" {
				t.Run("StringRoundtrip", func(t *testing.T) {
					s := got.String()
					got2, warns := parseBlockOptions(tc.commentMarker, s, tc.defaultOptions)
					if err := errors.Join(warns...); err != nil {
						t.Errorf("parseBlockOptions(%q, %q) = %v", tc.commentMarker, s, err)
					}
					if diff := cmp.Diff(got, got2, cmp.AllowUnexported(blockOptions{}), cmpRegexp); diff != "" {
						t.Errorf("parseBlockOptions(%q, %q) mismatch (-want +got):\n%s", tc.commentMarker, s, diff)
					}
				})
			}
		})
	}
}

func TestBlockOptions_ClonesDefaultOptions(t *testing.T) {
	defaults := blockOptions{
		StickyPrefixes: map[string]bool{},
	}
	_, warns := parseBlockOptions("", "sticky_prefixes=//", defaults)
	if err := errors.Join(warns...); err != nil {
		t.Errorf("parseBlockOptions() = _, %v", err)
	}
	if diff := cmp.Diff(blockOptions{}, defaults, cmp.AllowUnexported(blockOptions{}), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("defaults appear to have been modified (-want +got):\n%s", diff)
	}
}

func TestBlockOptions_ClonesDefaultOptions_Reflection(t *testing.T) {
	defaults := blockOptions{}
	defaultOpts := reflect.ValueOf(&defaults).Elem()
	var s []string
	for i := 0; i < defaultOpts.NumField(); i++ {
		val := defaultOpts.Field(i)
		switch val.Kind() {
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
			continue
		case reflect.Slice:
			val.Set(reflect.MakeSlice(val.Type(), 0, 0))
			s = append(s, fmt.Sprintf("%s=a,b,c", key(defaultOpts.Type().Field(i))))
		case reflect.Map:
			val.Set(reflect.MakeMap(val.Type()))
			s = append(s, fmt.Sprintf("%s=a,b,c", key(defaultOpts.Type().Field(i))))
		default:
			t.Errorf("Option %q has unhandled type: %v", key(defaultOpts.Type().Field(i)), val.Type())
		}

	}
	_, _ = parseBlockOptions("", strings.Join(s, " "), defaults)
	if diff := cmp.Diff(blockOptions{}, defaults, cmp.AllowUnexported(blockOptions{}), cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("defaults appear to have been modified (-want +got):\n%s", diff)
	}
}

func TestBlockOptions_regexTransform(t *testing.T) {
	for _, tc := range []struct {
		name string

		regexes []string
		in      string

		want [][]string
	}{
		{
			name:    "NoCapturingGroups",
			regexes: []string{".*"},
			in:      "abcde",
			want:    [][]string{{"abcde"}},
		},
		{
			name:    "CapturingGroups",
			regexes: []string{".(.).(.)."},
			in:      "abcde",
			want:    [][]string{{"b", "d"}},
		},
		{
			name:    "NonCapturingGroups",
			regexes: []string{".(.).(?:.)?."},
			in:      "abcde",
			want:    [][]string{{"b"}},
		},
		{
			name:    "MultipleRegexps",
			regexes: []string{".*", ".{3}(.)"},
			in:      "abcde",
			want:    [][]string{{"abcde"}, {"d"}},
		},
		{
			name:    "RegexDoesNotMatch",
			regexes: []string{`\d+`, `\w+`},
			in:      "abcde",
			want:    [][]string{nil, {"abcde"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opts blockOptions
			for _, regex := range tc.regexes {
				opts.ByRegex = append(opts.ByRegex, ByRegexOption{regexp.MustCompile(regex), nil})
			}

			gotTokens := opts.matchRegexes(tc.in)
			got := make([][]string, len(gotTokens))
			for i, t := range gotTokens {
				got[i] = []string(t)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("%q.matchRegexes(%q) diff (-want +got)\n%s", opts, tc.in, diff)
			}
		})
	}
}
