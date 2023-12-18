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
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"unicode"

	"go.uber.org/multierr"
	"golang.org/x/exp/slices"
)

var (
	boolValues = map[string]bool{
		"yes":   true,
		"true":  true,
		"no":    false,
		"false": false,
	}
)

// blockOptions enable/disable extra features that control how a block of lines is sorted.
//
// Currently, only three types are supported:
//  1. bool:            key=yes, key=true, key=no, key=false
//  2. []string:        key=a,b,c,d
//  3. map[string]bool: key=a,b,c,d
type blockOptions struct {
	// Lint determines whether we emit lint warnings for this block.
	Lint bool `default:"true"`

	// LINT.IfChange
	///////////////////////////
	//  Pre-sorting options  //
	///////////////////////////

	// Group determines whether we group lines together based on increasing indentation.
	Group bool `default:"true"`
	// Block opts us into a more complicated algorithm to try and understand blocks of code.
	Block bool `default:"false"`
	// StickyComments tells us to attach comments to the line immediately below them while sorting.
	StickyComments bool `key:"sticky_comments" default:"true"`
	// StickyPrefixes tells us about other types of lines that should behave as sticky comments.
	StickyPrefixes map[string]bool `key:"sticky_prefixes"`

	///////////////////////
	//  Sorting options  //
	///////////////////////

	// CaseSensitive is whether we're case sensitive while sorting.
	CaseSensitive bool `key:"case" default:"true"`
	// Numeric indicates that the contents should be sorted like numbers.
	Numeric bool `default:"false"`
	// PrefixOrder allows the user to explicitly order lines based on their matching prefix.
	PrefixOrder []string `key:"prefix_order"`
	// IgnorePrefixes is a slice of prefixes that we do not consider when sorting lines.
	IgnorePrefixes []string `key:"ignore_prefixes"`

	////////////////////////////
	//  Post-sorting options  //
	////////////////////////////

	// NewlineSeparated indicates that the groups should be separated with newlines.
	NewlineSeparated bool `key:"newline_separated" default:"false"`
	// RemoveDuplicates determines whether we drop lines that are an exact duplicate.
	RemoveDuplicates bool `key:"remove_duplicates" default:"true"`

	// LINT.ThenChange(//depot/google3/devtools/keep_sorted/README.md)

	// Syntax used to start a comment for keep-sorted annotation, e.g. "//".
	commentMarker string
}

func (f *Fixer) parseBlockOptions(startLine string) (blockOptions, error) {
	ret := blockOptions{}
	opts := reflect.ValueOf(&ret)
	var errs error
	for i := 0; i < opts.Elem().NumField(); i++ {
		field := opts.Elem().Type().Field(i)
		if !field.IsExported() {
			continue
		}

		val, err := parseBlockOption(field, startLine)
		if err != nil {
			errs = multierr.Append(errs, err)
		}

		opts.Elem().Field(i).Set(val)
	}

	if cm := f.guessCommentMarker(startLine); cm != "" {
		ret.commentMarker = cm
		if ret.StickyComments {
			if ret.StickyPrefixes == nil {
				ret.StickyPrefixes = make(map[string]bool)
			}
			ret.StickyPrefixes[cm] = true
		}
	}
	if len(ret.IgnorePrefixes) > 1 {
		// Look at longer prefixes first, in case one of these prefixes is a prefix of another.
		slices.SortFunc(ret.IgnorePrefixes, func(a string, b string) bool { return len(a) > len(b) })
	}

	return ret, errs
}

func parseBlockOption(f reflect.StructField, startLine string) (reflect.Value, error) {
	key := strings.ToLower(f.Name)
	if k, ok := f.Tag.Lookup("key"); ok {
		key = k
	}

	regex := regexp.MustCompile(fmt.Sprintf(`(^|\s)%s=(?P<value>[^ ]+?)($|\s)`, regexp.QuoteMeta(key)))
	if m := regex.FindStringSubmatchIndex(startLine); m != nil {
		val := string(regex.ExpandString(nil, "${value}", startLine, m))
		return parseValue(f, key, val)
	}
	return parseDefaultValue(f, key), nil
}

func parseDefaultValue(f reflect.StructField, key string) reflect.Value {
	val, err := parseValueWithDefault(f, key, f.Tag.Get("default"), func() reflect.Value { return reflect.Zero(f.Type) })
	if err != nil {
		panic(fmt.Errorf("blockOptions field %s has invalid default %q: %w", f.Name, f.Tag.Get("default"), err))
	}
	return val
}

func parseValue(f reflect.StructField, key, val string) (reflect.Value, error) {
	return parseValueWithDefault(f, key, val, func() reflect.Value { return parseDefaultValue(f, key) })
}

func parseValueWithDefault(f reflect.StructField, key, val string, defaultFn func() reflect.Value) (reflect.Value, error) {
	switch f.Type {
	case reflect.TypeOf(true):
		b, ok := boolValues[val]
		if !ok {
			return defaultFn(), fmt.Errorf("option %q has unknown value %q", key, val)
		}

		return reflect.ValueOf(b), nil
	case reflect.TypeOf([]string{}):
		if val == "" {
			return defaultFn(), nil
		}

		return reflect.ValueOf(strings.Split(val, ",")), nil
	case reflect.TypeOf(map[string]bool{}):
		if val == "" {
			return defaultFn(), nil
		}

		sp := strings.Split(val, ",")
		m := make(map[string]bool)
		for _, s := range sp {
			m[s] = true
		}
		return reflect.ValueOf(m), nil
	}

	panic(fmt.Errorf("unsupported blockOptions type: %v", f.Type))
}

func (f *Fixer) guessCommentMarker(startLine string) string {
	startLine = strings.TrimSpace(startLine)
	if strings.HasPrefix(startLine, "//") {
		return "//"
	} else if strings.HasPrefix(startLine, "#") {
		return "#"
	} else if strings.HasPrefix(startLine, "/*") {
		return "/*"
	} else if strings.HasPrefix(startLine, "--") {
		return "--"
	} else if strings.HasPrefix(startLine, ";") {
		return ";"
	} else if strings.HasPrefix(startLine, "<!--") {
		return "<!--"
	}
	return ""
}

// hasStickyPrefix determines if s has one of the StickyPrefixes.
func (opts blockOptions) hasStickyPrefix(s string) bool {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	for p := range opts.StickyPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// removeIgnorePrefix removes the first matching IgnorePrefixes from s, if s
// matches one of the IgnorePrefixes.
func (opts blockOptions) removeIgnorePrefix(s string) (string, bool) {
	t := strings.TrimLeftFunc(s, unicode.IsSpace)
	for _, p := range opts.IgnorePrefixes {
		if strings.HasPrefix(t, p) {
			return strings.Replace(s, p, "", 1), true
		}
	}
	return "", false
}

var (
	mixedNumberPattern = regexp.MustCompile(`([0-9]+)|([^0-9]+)`)
)

// maybeParseNumeric handles the Numeric option.
//
// If Numeric is true, the string will be parsed into subsequences of strings and numeric values.
// If Numeric is false, the result will just be a single token of the unchanged string.
func (opts blockOptions) maybeParseNumeric(s string) numericTokens {
	if !opts.Numeric {
		return numericTokens{[]string{s}, nil}
	}

	var t numericTokens
	m := mixedNumberPattern.FindAllStringSubmatch(s, -1)
	for _, sm := range m {
		if sm[1] != "" { // Numeric token
			if t.len() == 0 {
				// Make sure numericTokens "starts" with a string.
				// See the comment on numericTokens for more details.
				t.s = append(t.s, "")
			}
			i := new(big.Int)
			if err := i.UnmarshalText([]byte(sm[1])); err != nil {
				panic(fmt.Errorf("mixedNumberPattern yielded an unparseable int: %w", err))
			}
			t.i = append(t.i, i)
		} else /* sm[2] != "" */ { // String token
			t.s = append(t.s, sm[2])
		}
	}
	return t
}

// numericTokens is the result of parsing all numeric tokens out of a string.
//
// e.g. a string like "Foo_123" becomes
//
//	s: []string{"Foo_"},
//	i: []int64{123},
//
// To make comparisons possible, numericTokens _always_ "start" with a string,
// even if the string naturally starts with a number e.g. a string like
// "123_Foo" becomes
//
//	s: []string{"", "_Foo"},
//	i: []int64{123},
type numericTokens struct {
	s []string
	i []*big.Int
}

func (t numericTokens) len() int {
	return len(t.s) + len(t.i)
}

func (t numericTokens) compare(o numericTokens) int {
	l := t.len()
	if k := o.len(); k < l {
		l = k
	}
	for i := 0; i < l; i++ {
		if i%2 == 0 { // Start by comparing strings.
			if c := strings.Compare(t.s[i/2], o.s[i/2]); c != 0 {
				return c
			}
		} else { // Alternate by comparing with numbers.
			if c := t.i[i/2].Cmp(o.i[i/2]); c != 0 {
				return c
			}
		}
	}

	// If the numericTokens are all the same, whichever numericTokens that's
	// smaller is less than the other.
	return t.len() - o.len()
}
