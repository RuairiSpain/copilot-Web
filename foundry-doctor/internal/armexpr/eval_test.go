package armexpr

import (
	"strings"
	"testing"
)

func TestEval(t *testing.T) {
	cases := []struct {
		in   string
		want any
		ok   bool
	}{
		{"['plain']", "plain", true},
		{"[true()]", true, true},
		{"[not(true())]", false, true},
		{"[and(true(), equals('Enabled', 'enabled'))]", true, true},
		{"[or(false(), false())]", false, true},
		{"[equals('a', 'b')]", false, true},
		{"[equals(1, 1)]", true, true},
		{"[if(equals('a','a'), 'x', 'y')]", "x", true},
		{"[concat('a', 'b', 3)]", "ab3", true},
		{"[format('{0}-ai-{1}', 'dev', 'x')]", "dev-ai-x", true},
		{"[format('{{0}}')]", "{0}", true},
		{"[format('{0:N}', 'dev')]", nil, false},
		{"[format('{1}', 'dev')]", nil, false},
		{"[toLower('AbC')]", "abc", true},
		{"[toUpper('abc')]", "ABC", true},
		{"[empty('')]", true, true},
		{"[it''s]", nil, false},
		{"['it''s']", "it's", true},
		{"[parameters('x')]", nil, false},
		{"[uniqueString(resourceGroup().id)]", nil, false},
		{"[concat('a', parameters('x'))]", nil, false},
		{"[concat('a','b').x]", nil, false},
		{"[equals('a','a'))]", nil, false},
		{"[and(true())]", nil, false},
		{"not an expression", nil, false},
		{"[[escaped]", nil, false},
	}
	for _, c := range cases {
		got, ok := Eval(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Eval(%q) = %v, %v; want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestEvalBounds(t *testing.T) {
	deep := "[" + strings.Repeat("not(", 100) + "true()" + strings.Repeat(")", 100) + "]"
	if _, ok := Eval(deep); ok {
		t.Error("nesting beyond the depth bound must not evaluate")
	}
	if _, ok := Eval("[concat('" + strings.Repeat("a", maxEvalBytes) + "')]"); ok {
		t.Error("an oversize expression must not evaluate")
	}
}

func TestSimplify(t *testing.T) {
	cases := map[string]string{
		"[format('{0}-ai', 'dev')]":      "dev-ai",
		"[true()]":                       "[true()]",
		"[not(true())]":                  "[false()]",
		"[concat('[', 'x')]":             "[[x",
		"[concat('a', 1)]":               "a1",
		"[parameters('x')]":              "[parameters('x')]",
		"plain":                          "plain",
		"[add(1, 2)]":                    "[add(1, 2)]",
		"[if(true(), 'a', 'b')]":         "a",
		"[equals('A', 'a')]":             "[true()]",
		"[concat(parameters('a'), 'x')]": "[concat(parameters('a'), 'x')]",
	}
	for in, want := range cases {
		if got := Simplify(in); got != want {
			t.Errorf("Simplify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSimplifyValueTypes(t *testing.T) {
	if v := SimplifyValue("[true()]"); v != true {
		t.Errorf("bool: %v", v)
	}
	if v := SimplifyValue("[1]"); v != float64(1) {
		t.Errorf("number literal: %v", v)
	}
	if v := SimplifyValue("[format('{0}', 'a')]"); v != "a" {
		t.Errorf("string: %v", v)
	}
	if v := SimplifyValue("[parameters('x')]"); v != "[parameters('x')]" {
		t.Errorf("unknown: %v", v)
	}
}
