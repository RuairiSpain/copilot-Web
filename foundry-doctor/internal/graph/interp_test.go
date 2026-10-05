package graph

import (
	"reflect"
	"testing"
)

func TestScanInterpolations(t *testing.T) {
	tests := []struct {
		name, in string
		want     []Interpolation
	}{
		{"none", "plain $VAR text", nil},
		{"braced", "a ${FOO} b", []Interpolation{{Name: "FOO", Form: FormBraced, Offset: 2}}},
		{"default", "${FOO:-bar}", []Interpolation{{Name: "FOO", Form: FormDefault, HasDefault: true}}},
		{"empty default", "${FOO:-}", []Interpolation{{Name: "FOO", Form: FormDefault, HasDefault: true}}},
		{"expression", "${{ project.endpoint }}", []Interpolation{{Form: FormExpression, Expression: true}}},
		{"expression var", "${{VAR}}", []Interpolation{{Form: FormExpression, Expression: true}}},
		{"escaped", "$${FOO}", nil},
		{"escaped expression", "$${{x}}", nil},
		{"escape then real", "$$ ${A}", []Interpolation{{Name: "A", Form: FormBraced, Offset: 3}}},
		{"two", "${A}${B:-x}", []Interpolation{{Name: "A", Form: FormBraced}, {Name: "B", Form: FormDefault, HasDefault: true, Offset: 4}}},
		{"nested default", "${A:-${B}}", []Interpolation{{Form: FormMalformed}}},
		{"unterminated", "${A", []Interpolation{{Form: FormMalformed}}},
		{"unterminated default", "${A:-x", []Interpolation{{Form: FormMalformed}}},
		{"unterminated expression", "${{ x", []Interpolation{{Form: FormMalformed}}},
		{"bad name", "${1A}", []Interpolation{{Form: FormMalformed}}},
		{"unsupported operator", "${A:=x}", []Interpolation{{Form: FormMalformed}}},
		{"empty", "${}", []Interpolation{{Form: FormMalformed}}},
		{"trailing dollar", "x$", nil},
		{"unicode", "é${A}", []Interpolation{{Name: "A", Form: FormBraced, Offset: 2}}},
		{"unsupported unterminated", "${A:=x", []Interpolation{{Form: FormMalformed}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanInterpolations(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}
