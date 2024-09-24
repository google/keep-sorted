package keepsorted

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPopValue(t *testing.T) {
	for _, tc := range []struct {
		name string

		input string

		want    any
		wantErr bool
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := "trailing content..."
			in := tc.input + " " + suffix
			typ := reflect.TypeOf(tc.want)

			parser := &parser{line: in}

			val, err := parser.popValue(typ)
			if (err != nil) != tc.wantErr {
				t.Errorf("parser{%q}.popValue(%v) = _, %v; wantErr %t", in, typ, err, tc.wantErr)
			}
			if err == nil {
				if diff := cmp.Diff(val.Interface(), tc.want); diff != "" {
					t.Errorf("parser{%q}.popValue(%v) = %v, _; want %v", in, typ, val.Interface(), tc.want)
				}
			} else {
				want := reflect.Zero(typ).Interface()
				if diff := cmp.Diff(val.Interface(), want); diff != "" {
					t.Errorf("parser{%q}.popValue(%v) = %v, _; want zero value for %v (%v)", in, typ, val.Interface(), typ, want)
				}
			}

			if parser.line != suffix {
				t.Errorf("parser{%q}.popValue(%v) did not consume the right amount of input. %q is remaining; expected %q", in, typ, parser.line, suffix)
			}
		})
	}
}
