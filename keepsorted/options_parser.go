package keepsorted

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
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
	keyRegex = regexp.MustCompile("(?P<key>[a-z_]+)=")
)

type parser struct {
	line string
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
	case reflect.TypeFor[int]():
		val, err := p.popInt()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[[]string]():
		val, err := p.popList()
		return reflect.ValueOf(val), err
	case reflect.TypeFor[map[string]bool]():
		val, err := p.popSet()
		return reflect.ValueOf(val), err
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

func (p *parser) popList() ([]string, error) {
	val, rest, ok := tryFindYAMLListAtStart(p.line)
	if ok {
		p.line = rest
		return parseYAMLList(val)
	}
	val, rest, _ = strings.Cut(p.line, " ")
	p.line = rest
	return strings.Split(val, ","), nil
}

func tryFindYAMLListAtStart(s string) (list, rest string, ok bool) {
	if s == "" || s[0] != '[' {
		return "", "", false
	}

	var quote rune
	var depth int // s[0] == '[' forces this to 1 after the first iteration.
	iter := newRuneIter(s)
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
					depth--
				}
				break
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
		return "", "", false
	}
	return s[:iter.idx], s[iter.idx:], true
}

func parseYAMLList(list string) ([]string, error) {
	var val []string
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
