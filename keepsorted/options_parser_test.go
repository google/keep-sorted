package keepsorted

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var cmpRegexp = cmp.Comparer(func(a, b *regexp.Regexp) bool {
	return a.String() == b.String()
})

func TestPopValue(t *testing.T) {
	for _, tc := range []struct {
		name string

		input         string
		allowYAMLList bool

		want                      any
		wantErr                   bool
		additionalTrailingContent string
	}{
		{
			name: "Bool",

			input: "yes",
			want:  true,
		},
		{
			name: "Bool_Invalid",

			input:   "nah",
			want:    false,
			wantErr: true,
		},
		{
			name: "Int",

			input: "123",
			want:  int(123),
		},
		{
			name: "Int_Invalid",

			input:   "foo",
			want:    int(0),
			wantErr: true,
		},
		{
			name: "List_Empty",

			input: "",
			want:  []string{},
		},
		{
			name: "List_SingleElement",

			input: "foo",
			want:  []string{"foo"},
		},
		{
			name: "List_MultipleElements_WithRepeats",

			input: "foo,bar,foo",
			want:  []string{"foo", "bar", "foo"},
		},
		{
			name: "List_YAML",

			input:         "[foo, bar, foo]",
			allowYAMLList: true,
			want:          []string{"foo", "bar", "foo"},
		},
		{
			name: "List_YAML_YAMLNotAllowed",

			input:                     "[foo, bar, foo]",
			allowYAMLList:             false,
			want:                      []string{"[foo", ""},
			additionalTrailingContent: "bar, foo]",
		},
		{
			name: "List_YAML_LooksLikeYAMLButIsnt",

			input:                     "[,[`",
			allowYAMLList:             true,
			want:                      []string{},
			wantErr:                   true,
			additionalTrailingContent: "[,[`",
		},
		{
			name: "List_YAML_YamlNotAllowed_LooksLikeYAMLButIsnt",

			input:         "[,[`",
			allowYAMLList: false,
			want:          []string{"[", "[`"},
		},
		{
			name: "List_YAML_NotTerminated",

			input:                     "[foo, bar, foo",
			allowYAMLList:             false,
			want:                      []string{"[foo", ""},
			additionalTrailingContent: "bar, foo",
		},
		{
			name: "List_YAML_NestedList_ParsesButYieldsError",

			input:         "[foo, [bar]]",
			allowYAMLList: true,
			want:          []string{},
			wantErr:       true,
		},
		{
			name: "List_YAML_NestedList_NotTerminated",

			input:                     "[foo, [bar]",
			allowYAMLList:             true,
			want:                      []string{},
			wantErr:                   true,
			additionalTrailingContent: "[foo, [bar]",
		},
		{
			name: "List_YAML_EscapingRules_SinglyQuotedOpenBracketDoesNotIncreaseDepth",

			input:         `['[']`,
			allowYAMLList: true,
			want:          []string{`[`},
		},
		{
			name: "List_YAML_EscapingRules_SinglyQuotedOpenBracketDoesNotIncreaseDepth_AdditionalDoubleQuoteDoesNotConfuseQuotingRules",

			input:         `['["']`,
			allowYAMLList: true,
			want:          []string{`["`},
		},
		{
			name: "List_YAML_EscapingRules_DoublyQuotedOpenBracketDoesNotIncreaseDepth",

			input:         `["["]`,
			allowYAMLList: true,
			want:          []string{`[`},
		},
		{
			name: "List_YAML_EscapingRules_DoublyQuotedOpenBracketDoesNotIncreaseDepth_AdditionalSingleQuoteDoesNotConfuseQuotingRules",

			input:         `["['"]`,
			allowYAMLList: true,
			want:          []string{`['`},
		},
		{
			name: "List_YAML_EscapingRules_SinglyQuotedClosingBracketDoesNotTerminate",

			input:         `[']']`,
			allowYAMLList: true,
			want:          []string{`]`},
		},
		{
			name: "List_YAML_EscapingRules_DoublyQuotedClosingBracketDoesNotTerminate",

			input:         `["]"]`,
			allowYAMLList: true,
			want:          []string{`]`},
		},
		{
			name: "List_YAML_EscapingRules_SinglyQuotedClosingBracketDoesNotTerminate_AdditionalEscapedSingleQuote",

			input:         `[']''']`,
			allowYAMLList: true,
			want:          []string{`]'`},
		},
		{
			name: "List_YAML_EscapingRules_SinglyQuotedClosingBracketDoesNotTerminate_BackslashDoesNotEscapeSingleQuote",

			input:         `[']\']`,
			allowYAMLList: true,
			want:          []string{`]\`},
		},
		{
			name: "List_YAML_EscapingRules_DoublyQuotedClosingBracketDoesNotTerminate_AdditionalEscapedDoubleQuote",

			input:         `["]\""]`,
			allowYAMLList: true,
			want:          []string{`]"`},
		},
		{
			name: "List_YAML_EscapingRules_DoublyQuotedClosingBracketDoesNotTerminate_AdditionalEscapedDoubleQuoteAndBackslashes",

			input:         `["]\"\\"]`,
			allowYAMLList: true,
			want:          []string{`]"\`},
		},
		{
			name: "Set_Empty",

			input: "",
			want:  map[string]bool{},
		},
		{
			name: "Set_SingleElement",

			input: "foo",
			want:  map[string]bool{"foo": true},
		},
		{
			name: "Set_MultipleElements_WithRepeats",

			input: "foo,bar,foo",
			want:  map[string]bool{"foo": true, "bar": true},
		},
		{
			name: "Regex",

			input: ".*",
			want:  []*regexp.Regexp{regexp.MustCompile(".*")},
		},
		{
			name: "MultipleRegex",

			input:         `[.*, abcd, '(?:efgh)ijkl']`,
			allowYAMLList: true,
			want:          []*regexp.Regexp{regexp.MustCompile(".*"), regexp.MustCompile("abcd"), regexp.MustCompile("(?:efgh)ijkl")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := "trailing content..."
			in := tc.input + " " + suffix
			typ := reflect.TypeOf(tc.want)

			parser := &parser{line: in, allowYAMLLists: tc.allowYAMLList}

			val, err := parser.popValue(typ)
			if (err != nil) != tc.wantErr {
				t.Errorf("parser{%q}.popValue(%v) = _, %v; wantErr %t", in, typ, err, tc.wantErr)
			}
			if err == nil {
				if diff := cmp.Diff(val.Interface(), tc.want, cmpRegexp); diff != "" {
					t.Errorf("parser{%q}.popValue(%v) = %v, _; want %v", in, typ, val.Interface(), tc.want)
				}
			} else {
				want := reflect.Zero(typ).Interface()
				if diff := cmp.Diff(val.Interface(), want, cmpRegexp); diff != "" {
					t.Errorf("parser{%q}.popValue(%v) = %v, _; want zero value for %v (%v)", in, typ, val.Interface(), typ, want)
				}
			}

			if tc.additionalTrailingContent != "" {
				tc.additionalTrailingContent += " "
			}
			if want := tc.additionalTrailingContent + suffix; parser.line != want {
				t.Errorf("parser{%q}.popValue(%v) did not consume the right amount of input. %q is remaining; expected %q", in, typ, parser.line, want)
			}
		})
	}
}
