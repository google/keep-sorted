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
	"maps"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	yaml "gopkg.in/yaml.v3"
)

type BlockOptions struct {
	opts blockOptions
}

func DefaultBlockOptions() BlockOptions {
	return BlockOptions{defaultOptions}
}

func ParseBlockOptions(options string) (BlockOptions, error) {
	opts, warns := parseBlockOptions( /*commentMarker=*/ "", options, blockOptions{})
	if err := errors.Join(warns...); err != nil {
		return BlockOptions{}, err
	}
	return BlockOptions{opts}, nil
}

func (opts BlockOptions) String() string {
	return opts.opts.String()
}

// blockOptions enable/disable extra features that control how a block of lines is sorted.
//
// Currently, only four types are supported:
//  1. bool:            key=yes, key=true, key=no, key=false
//  2. []string:        key=a,b,c,d
//  3. map[string]bool: key=a,b,c,d
//  4. int:             key=123
type blockOptions struct {
	// AllowYAMLLists determines whether list.set valued options are allowed to be specified by YAML.
	AllowYAMLLists bool `key:"allow_yaml_lists"`

	///////////////////////////
	//  Pre-sorting options  //
	///////////////////////////

	// SkipLines is the number of lines to ignore before sorting.
	SkipLines int `key:"skip_lines"`
	// Group determines whether we group lines together based on increasing indentation.
	Group bool
	// GroupPrefixes tells us about other types of lines that should be added to a group.
	GroupPrefixes map[string]bool `key:"group_prefixes"`
	// Block opts us into a more complicated algorithm to try and understand blocks of code.
	Block bool
	// StickyComments tells us to attach comments to the line immediately below them while sorting.
	StickyComments bool `key:"sticky_comments"`
	// StickyPrefixes tells us about other types of lines that should behave as sticky comments.
	StickyPrefixes map[string]bool `key:"sticky_prefixes"`

	///////////////////////
	//  Sorting options  //
	///////////////////////

	// CaseSensitive is whether we're case sensitive while sorting.
	CaseSensitive bool `key:"case"`
	// Numeric indicates that the contents should be sorted like numbers.
	Numeric bool
	// PrefixOrder allows the user to explicitly order lines based on their matching prefix.
	PrefixOrder []string `key:"prefix_order"`
	// IgnorePrefixes is a slice of prefixes that we do not consider when sorting lines.
	IgnorePrefixes []string `key:"ignore_prefixes"`
	// CommentOrder allows the user to sort lines based on their comment.
	CommentOrder bool `key:"comment_order"`

	////////////////////////////
	//  Post-sorting options  //
	////////////////////////////

	// NewlineSeparated indicates that the groups should be separated with newlines.
	NewlineSeparated bool `key:"newline_separated"`
	// RemoveDuplicates determines whether we drop lines that are an exact duplicate.
	RemoveDuplicates bool `key:"remove_duplicates"`

	// Syntax used to start a comment for keep-sorted annotation, e.g. "//".
	commentMarker string
}

var (
	defaultOptions = blockOptions{
		AllowYAMLLists:   true,
		Group:            true,
		StickyComments:   true,
		StickyPrefixes:   nil, // Will be populated with the comment marker of the start directive.
		CaseSensitive:    true,
		RemoveDuplicates: true,
	}

	fieldIndexByKey map[string]int
)

func init() {
	fieldIndexByKey = make(map[string]int)
	typ := reflect.TypeFor[blockOptions]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		key := key(field)
		if !keyRegex.MatchString(key + "=") {
			panic(fmt.Errorf("key %q for blockOptions.%s would not be matched by parser (regex: %v)", key, field.Name, keyRegex))
		}
		fieldIndexByKey[key] = i
	}
}

func key(f reflect.StructField) string {
	key := strings.ToLower(f.Name)
	if k, ok := f.Tag.Lookup("key"); ok {
		key = k
	}
	return key
}

func parseBlockOptions(commentMarker, options string, defaults blockOptions) (_ blockOptions, warnings []error) {
	ret := defaults
	opts := reflect.ValueOf(&ret).Elem()
	var warns []error
	parser := newParser(options)
	for {
		parser.allowYAMLLists = ret.AllowYAMLLists
		key, ok := parser.popKey()
		if !ok {
			break
		}
		fieldIdx, ok := fieldIndexByKey[key]
		if !ok {
			warns = append(warns, fmt.Errorf("unrecognized option %q", key))
			continue
		}

		field := opts.Field(fieldIdx)
		val, err := parser.popValue(field.Type())
		if err != nil {
			warns = append(warns, fmt.Errorf("while parsing option %q: %w", key, err))
			continue
		}
		field.Set(val)
	}

	if cm := guessCommentMarker(commentMarker); cm != "" {
		ret.setCommentMarker(cm)
	}
	if len(ret.IgnorePrefixes) > 1 {
		// Look at longer prefixes first, in case one of these prefixes is a prefix of another.
		slices.SortFunc(ret.IgnorePrefixes, func(a string, b string) int { return cmp.Compare(len(b), len(a)) })
	}

	if warn := validate(&ret); len(warn) > 0 {
		warns = append(warns, warn...)
	}

	return ret, warns
}

func formatValue(val reflect.Value) (string, error) {
	switch val.Type() {
	case reflect.TypeFor[bool]():
		return boolString[val.Bool()], nil
	case reflect.TypeFor[[]string]():
		return formatList(val.Interface().([]string))
	case reflect.TypeFor[map[string]bool]():
		return formatList(slices.Sorted(maps.Keys(val.Interface().(map[string]bool))))
	case reflect.TypeFor[int]():
		return strconv.Itoa(int(val.Int())), nil
	}

	panic(fmt.Errorf("unsupported blockOptions type: %v", val.Type()))
}

func formatList(vals []string) (string, error) {
	var specialChars bool
	if len(vals) > 0 && strings.HasPrefix(vals[0], "[") {
		specialChars = true
	} else {
		for _, val := range vals {
			if strings.ContainsAny(val, ", ") {
				specialChars = true
				break
			}
		}
	}

	if !specialChars {
		return strings.Join(vals, ","), nil
	}

	node := new(yaml.Node)
	if err := node.Encode(vals); err != nil {
		return "", fmt.Errorf("while converting list to YAML: %w", err)
	}
	node.Style |= yaml.FlowStyle
	out, err := yaml.Marshal(node)
	if err != nil {
		return "", fmt.Errorf("while formatting YAML: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func guessCommentMarker(startLine string) string {
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

func (opts *blockOptions) setCommentMarker(marker string) {
	opts.commentMarker = marker
	if opts.StickyComments {
		if opts.StickyPrefixes == nil {
			opts.StickyPrefixes = make(map[string]bool)
		}
		opts.StickyPrefixes[marker] = true
	}
}

func validate(opts *blockOptions) (warnings []error) {
	var warns []error
	if opts.SkipLines < 0 {
		warns = append(warns, fmt.Errorf("skip_lines has invalid value: %v", opts.SkipLines))
		opts.SkipLines = 0
	}

	if opts.GroupPrefixes != nil && !opts.Group {
		warns = append(warns, fmt.Errorf("group_prefixes may not be used with group=no"))
		opts.GroupPrefixes = nil
	}

	return warns
}

func (opts blockOptions) String() string {
	var s []string
	val := reflect.ValueOf(opts)
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(fieldIndexByKey)) {
		field := val.Type().Field(fieldIndexByKey[key])
		fieldVal := val.FieldByIndex(field.Index)
		if fieldVal.IsZero() {
			continue
		}
		val, err := formatValue(fieldVal)
		if err != nil {
			errs = append(errs, err)
		} else {
			s = append(s, fmt.Sprintf("%s=%s", key, val))
		}
	}

	if err := errors.Join(errs...); err != nil {
		panic(err)
	}

	return strings.Join(s, " ")
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
