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
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
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
// Currently, only four types are supported:
//  1. bool:            key=yes, key=true, key=no, key=false
//  2. []string:        key=a,b,c,d
//  3. map[string]bool: key=a,b,c,d
//  4. int:             key=123
type blockOptions struct {
	// Lint determines whether we emit lint warnings for this block.
	Lint bool `default:"true"`

	// LINT.IfChange
	///////////////////////////
	//  Pre-sorting options  //
	///////////////////////////

	// SkipLines is the number of lines to ignore before sorting.
	SkipLines int `key:"skip_lines"`
	// Group determines whether we group lines together based on increasing indentation.
	Group bool `default:"true"`
	// GroupPrefixes tells us about other types of lines that should be added to a group.
	GroupPrefixes map[string]bool `key:"group_prefixes"`
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
			errs = errors.Join(errs, err)
		}

		opts.Elem().Field(i).Set(val)
	}

	if ret.SkipLines < 0 {
		errs = errors.Join(errs, fmt.Errorf("skip_lines has invalid value: %v", ret.SkipLines))
		ret.SkipLines = 0
	}

	if ret.GroupPrefixes != nil && !ret.Group {
		errs = errors.Join(errs, fmt.Errorf("group_prefixes may not be used with group=no"))
		ret.GroupPrefixes = nil
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
		slices.SortFunc(ret.IgnorePrefixes, func(a string, b string) int { return cmp.Compare(len(b), len(a)) })
	}

	return ret, errs
}

func parseBlockOption(f reflect.StructField, startLine string) (reflect.Value, error) {
	key := strings.ToLower(f.Name)
	if k, ok := f.Tag.Lookup("key"); ok {
		key = k
	}

	needle := key + "="
	i := strings.Index(startLine, needle)
	if i < 0 {
		return parseDefaultValue(f, key), nil
	}

	valRunes := []rune(startLine[i+len(needle):])
	var val strings.Builder
	var quote bool
loop:
	for i := 0; i < len(valRunes); i++ {
		r := valRunes[i]
		switch {
		case r == '"':
			quote = !quote
		case r == '\\':
			if i+1 < len(valRunes) {
				s := valRunes[i+1]
				i++
				switch s {
				case '"', '\\':
					// Skip the escaping \
				default:
					val.WriteRune(r)
				}
				val.WriteRune(s)
			} else {
				val.WriteRune(r)
			}
		case !quote && unicode.IsSpace(r):
			break loop
		default:
			val.WriteRune(r)
		}
	}

	parsed, err := parseValue(f, key, val.String())
	if quote {
		err = errors.Join(fmt.Errorf("value for %q has no terminating quote", key), err)
	}
	return parsed, err
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
	case reflect.TypeOf(bool(false)):
		b, ok := boolValues[val]
		if !ok {
			return defaultFn(), fmt.Errorf("option %q has unknown value %q", key, val)
		}

		return reflect.ValueOf(b), nil
	case reflect.TypeOf([]string{}):
		if val == "" {
			return defaultFn(), nil
		}

		return reflect.ValueOf(splitStringVal(val)), nil
	case reflect.TypeOf(map[string]bool{}):
		if val == "" {
			return defaultFn(), nil
		}

		sp := splitStringVal(val)
		m := make(map[string]bool)
		for _, s := range sp {
			m[s] = true
		}
		return reflect.ValueOf(m), nil
	case reflect.TypeOf(int(0)):
		if val == "" {
			return defaultFn(), nil
		}

		i, err := strconv.Atoi(val)
		if err != nil {
			return defaultFn(), fmt.Errorf("option %q has invalid value %q: %w", key, val, err)
		}
		return reflect.ValueOf(i), nil
	}

	panic(fmt.Errorf("unsupported blockOptions type: %v", f.Type))
}

func splitStringVal(val string) []string {
	runes := []rune(val)
	var vals []string
	var cur strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case ',':
			vals = append(vals, cur.String())
			cur.Reset()
		case '\\':
			if i+1 < len(runes) && runes[i+1] == ',' {
				cur.WriteRune(',')
				i++
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	return append(vals, cur.String())
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

// hasPrefix determines if s has one of the prefixes.
func hasPrefix(s string, prefixes map[string]bool) bool {
	if len(prefixes) == 0 {
		return false
	}
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	for p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// hasStickyPrefix determines if s has one of the StickyPrefixes.
func (opts blockOptions) hasStickyPrefix(s string) bool {
	return hasPrefix(s, opts.StickyPrefixes)
}

// hasGroupPrefix determines if s has one of the GroupPrefixes.
func (opts blockOptions) hasGroupPrefix(s string) bool {
	return hasPrefix(s, opts.GroupPrefixes)
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
			if _, ok := i.SetString(sm[1], 10); !ok {
				panic(fmt.Errorf("mixedNumberPattern yielded an unparseable int: %q", sm[1]))
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
