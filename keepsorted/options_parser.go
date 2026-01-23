package keepsorted

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	yaml "gopkg.in/yaml.v3"
)

var (
	boolValues = map[string]bool{
		"yes":   true,
		"true":  true,
		"no":    false,
		"false": false,
	}
	boolString = map[bool]string{
		true:  "yes",
		false: "no",
	}
	keyRegex = regexp.MustCompile("(^| )(?P<key>[a-z_]+)=")

	errNotYAMLList = fmt.Errorf("content does not appear to be a YAML list")
)

type parser struct {
	line string

	allowYAMLLists bool
}

func newParser(options string) *parser {
	return &parser{line: options}
}

func (p *parser) popKey() (string, bool) {
	m := keyRegex.FindStringSubmatchIndex(p.line)
	if m == nil {
		return "", false
	}
	key := string(keyRegex.ExpandString(nil, "${key}", p.line, m))
	p.line = p.line[m[1]:]
	return key, true
}

func (p *parser) popValue(typ reflect.Type) (reflect.Value, error) {
	switch typ {
	case reflect.TypeFor[bool]():
		val, err := p.popBool()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[IntOrBool]():
		val, err := p.popIntOrBool()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[int]():
		val, err := p.popInt()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[SortOrder]():
		val, err := p.popSortOrder()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[[]string]():
		val, err := p.popList()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[map[string]bool]():
		val, err := p.popSet()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[[]ByRegexOption]():
		val, err := p.popByRegexOption()
		if err != nil {
			return reflect.Zero(typ), err
		}
		return reflect.ValueOf(val), nil
	case reflect.TypeFor[[]*regexp.Regexp]():
		val, err := p.popRegexListOption()
		if err != nil {
			return reflect.Zero(typ), err
		}
		return reflect.ValueOf(val), nil
	}

	panic(fmt.Errorf("unhandled case in switch: %v", typ))
}

func (p *parser) popBool() (bool, error) {
	val, rest, _ := strings.Cut(p.line, " ")
	p.line = rest
	b, ok := boolValues[val]
	if !ok {
		return false, fmt.Errorf("unrecognized bool value %q", val)
	}
	return b, nil
}

func (p *parser) popInt() (int, error) {
	val, rest, _ := strings.Cut(p.line, " ")
	p.line = rest
	i, err := strconv.Atoi(val)
	if err != nil {
		return 0, err
	}
	return i, nil
}

func (p *parser) popIntOrBool() (IntOrBool, error) {
	val, rest, _ := strings.Cut(p.line, " ")
	p.line = rest
	i, err := strconv.Atoi(val)
	if err != nil {
		b, ok := boolValues[val]
		if ok {
			if b {
				return 1, nil
			}
			return 0, nil
		}
		return 0, err
	}
	return IntOrBool(i), nil
}

func (ar *ByRegexOption) UnmarshalYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!str":
		pat, err := regexp.Compile(node.Value)
		if err != nil {
			return err
		}
		ar.Pattern = pat
		ar.Template = nil
		return nil
	case "!!map":
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return err
		}
		if len(m) != 1 {
			return fmt.Errorf("by_regex map item must have exactly one key-value pair, but got %d", len(m))
		}
		for pattern, template := range m {
			pat, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("invalid regex pattern %q: %w", pattern, err)
			}
			ar.Pattern = pat
			ar.Template = &template
			return nil
		}
	}

	return fmt.Errorf("unexpected data type at %v", node.Tag)
}

func popListValue[T any](p *parser, parse func(string) (T, error)) ([]T, error) {
	if p.allowYAMLLists {
		val, rest, err := tryFindYAMLListAtStart(p.line)
		if err != nil && !errors.Is(err, errNotYAMLList) {
			return nil, err
		}
		if err == nil {
			p.line = strings.TrimSpace(rest)
			return parseYAMLList[T](val)
		}
	}

	val, rest, _ := strings.Cut(p.line, " ")
	p.line = strings.TrimSpace(rest)
	if val == "" {
		return []T{}, nil
	}

	var ret []T
	var errs []error
	for _, item := range strings.Split(val, ",") {
		v, err := parse(item)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ret = append(ret, v)
	}
	return ret, errors.Join(errs...)
}

func (p *parser) popList() ([]string, error) {
	return popListValue(p, func(s string) (string, error) { return s, nil })
}

func (p *parser) popByRegexOption() ([]ByRegexOption, error) {
	return popListValue(p, func(s string) (ByRegexOption, error) {
		pat, err := regexp.Compile(s)
		return ByRegexOption{Pattern: pat}, err
	})
}

func (p *parser) popRegexListOption() ([]*regexp.Regexp, error) {
	return popListValue(p, func(s string) (*regexp.Regexp, error) {
		pat, err := regexp.Compile(s)
		return pat, err
	})
}

func (p *parser) popSortOrder() (SortOrder, error) {
	val, rest, _ := strings.Cut(p.line, " ")
	p.line = rest
	switch val {
	case string(OrderAsc):
		return OrderAsc, nil
	case string(OrderDesc):
		return OrderDesc, nil
	default:
		return OrderAsc, fmt.Errorf("unrecognized order value %q, expected 'asc' or 'desc'", val)
	}
}

func tryFindYAMLListAtStart(s string) (list, rest string, err error) {
	if s == "" || s[0] != '[' {
		return "", "", errNotYAMLList
	}

	var quote rune
	var depth int // s[0] == '[' forces this to 1 after the first iteration.
	iter := newRuneIter(s)
loop:
	for {
		ch, ok := iter.pop()
		if !ok {
			break
		}
		switch ch {
		case '[':
			if quote == 0 {
				depth++
			}
		case ']':
			if quote == 0 {
				if depth > 1 {
					depth--
					continue
				}

				// depth == 1

				// Force the last ] to either be the end, or followed by a space
				// We don't want to allow
				// key1=[a, b, c ,d]key2=yes
				if next, ok := iter.peek(); !ok || next == ' ' {
					if next == ' ' {
						iter.pop()
					}
					depth--
				}
				break loop
			}
		case '"', '\'':
			if quote == ch {
				quote = 0
			} else if quote == 0 {
				quote = ch
			}
		case '\\':
			if quote == '"' {
				if next, ok := iter.peek(); ok && (next == '"' || next == '\\') {
					iter.pop()
				}
			}
		}
	}
	if depth != 0 {
		// YAML list doesn't appear to terminate.
		return "", "", fmt.Errorf("content appears to be an unterminated YAML list: %q", s[:iter.idx])
	}
	return s[:iter.idx], s[iter.idx:], nil
}

func parseYAMLList[T any](list string) ([]T, error) {
	var val []T
	if err := yaml.Unmarshal([]byte(list), &val); err != nil {
		return nil, err
	}

	return val, nil
}

func (p *parser) popSet() (map[string]bool, error) {
	list, err := p.popList()
	if err != nil {
		return nil, err
	}
	val := make(map[string]bool, len(list))
	for _, e := range list {
		val[e] = true
	}
	return val, nil
}

type runeIter struct {
	s   string
	idx int
}

func newRuneIter(s string) *runeIter {
	return &runeIter{s: s}
}

func (iter *runeIter) peek() (rune, bool) {
	if iter.s[iter.idx:] == "" {
		return utf8.RuneError, false
	}
	ch, _ := utf8.DecodeRuneInString(iter.s[iter.idx:])
	if ch == utf8.RuneError {
		return utf8.RuneError, false
	}
	return ch, true
}

func (iter *runeIter) pop() (rune, bool) {
	ch, ok := iter.peek()
	if ok {
		iter.idx += utf8.RuneLen(ch)
	}
	return ch, ok
}
