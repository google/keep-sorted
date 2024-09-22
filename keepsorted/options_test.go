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
	"reflect"
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
			in:   "lint=yes",

			want: blockOptions{Lint: true},
		},
		{
			name: "SkipLines",
			in:   "skip_lines=10",

			want: blockOptions{SkipLines: 10},
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
			name: "ItemList_WithSpaces",
			in:   `prefix_order=[a, b, c d, 'e",\f', 'g]', "h\"]"]`,

			want: blockOptions{
				PrefixOrder: []string{"a", "b", "c d", `e",\f`, "g]", `h"]`},
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
			name: "ItemSet_WithSpaces",
			in:   `sticky_prefixes=[a, b, c d, 'e",\f', 'g]', "h\"]"]`,

			want: blockOptions{
				StickyPrefixes: map[string]bool{"a": true, "b": true, "c d": true, `e",\f`: true, "g]": true, `h"]`: true},
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
			in:            "block=yes  # lint=yes",

			want: blockOptions{
				Block:         true,
				Lint:          true,
				commentMarker: "#",
			},
		},
		{
			name: "ErrorDoesNotStopParsing",
			in:   "lint=nah case=no",
			defaultOptions: blockOptions{
				Lint:          true,
				CaseSensitive: true,
			},

			want: blockOptions{
				Lint:          true, // The default value should not change.
				CaseSensitive: false,
			},
			wantErr: `while parsing option "lint": unrecognized bool value "nah"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initZerolog(t)
			got, err := parseBlockOptions(tc.commentMarker, tc.in, tc.defaultOptions)
			if err != nil {
				if tc.wantErr == "" {
					t.Errorf("parseBlockOptions(%q, %q) = %v", tc.commentMarker, tc.in, err)
				} else if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("parseBlockOptions(%q, %q) = %v, expected to contain %q", tc.commentMarker, tc.in, err, tc.wantErr)
				}
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(blockOptions{})); diff != "" {
				t.Errorf("parseBlockOptions(%q, %q) mismatch (-want +got):\n%s", tc.commentMarker, tc.in, diff)
			}

			_ = got.String() // Make sure this doesn't panic.
		})
	}
}

func TestBlockOptions_ClonesDefaultOptions(t *testing.T) {
	defaults := blockOptions{
		StickyPrefixes: map[string]bool{},
	}
	_, err := parseBlockOptions("", "sticky_prefixes=//", defaults)
	if err != nil {
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
