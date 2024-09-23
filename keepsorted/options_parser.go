package keepsorted

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
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
	val, rest, _ := strings.Cut(p.line, " ")
	p.line = rest
	return strings.Split(val, ","), nil
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
