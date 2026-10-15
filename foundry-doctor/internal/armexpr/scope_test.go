package armexpr

import "testing"

func TestRewriteRefs(t *testing.T) {
	bound := func(kind, name string) (string, bool) {
		switch {
		case kind == RefParameter && name == "name":
			return "format('srch-{0}', variables('u'))", true
		case kind == RefParameter && name == "lit":
			return "'a''b'", true
		case kind == RefVariable:
			return "variables('" + ScopedName(name, "m") + "')", true
		}
		return "", false
	}
	tests := []struct{ in, want string }{
		{"[parameters('name')]", "[format('srch-{0}', variables('u'))]"},
		{"[ PARAMETERS ( 'Name' ) ]", "[ PARAMETERS ( 'Name' ) ]"}, // unbound by case: callback is case-exact
		{"[format('{0}/x', parameters('name'))]", "[format('{0}/x', format('srch-{0}', variables('u')))]"},
		{"[parameters('lit')]", "a'b"},
		{"[parameters('lit').x]", "[parameters('lit').x]"},
		{"[variables('v')]", "[variables('v@m')]"},
		{"[concat('parameters(''name'')', parameters('other'))]", "[concat('parameters(''name'')', parameters('other'))]"},
		{"[myparameters('name')]", "[myparameters('name')]"},
		{"plain", "plain"},
		{"[[parameters('name')]", "[[parameters('name')]"},
	}
	for _, tc := range tests {
		if got := RewriteRefs(tc.in, bound); got != tc.want {
			t.Errorf("RewriteRefs(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRewriteRefsLiteralBracketEscaped(t *testing.T) {
	got := RewriteRefs("[parameters('x')]", func(string, string) (string, bool) { return "'[abc'", true })
	if got != "[[abc" {
		t.Errorf("got %q", got)
	}
}

func TestStripScope(t *testing.T) {
	in := "[format('{0}', parameters('name@m0-a/b'), variables('v@m0-a'), 'x@y')]"
	want := "[format('{0}', parameters('name'), variables('v'), 'x@y')]"
	if got := StripScope(in); got != want {
		t.Errorf("StripScope = %q, want %q", got, want)
	}
	if StripScope("[parameters('a')]") != "[parameters('a')]" {
		t.Error("unqualified text must be unchanged")
	}
}

func TestScopedNamesDoNotCompareSame(t *testing.T) {
	a := ParseName("Microsoft.Search/searchServices", "[parameters('name@m1-a')]")
	b := ParseName("Microsoft.Search/searchServices", "[parameters('name@m2-b')]")
	c := ParseName("Microsoft.Search/searchServices", "[parameters('name')]")
	if got := Compare(a, b); got != Unknown {
		t.Errorf("different scopes: %v, want Unknown", got)
	}
	if got := Compare(a, c); got != Unknown {
		t.Errorf("module-local vs entry: %v, want Unknown", got)
	}
	if got := Compare(a, a); got != Same {
		t.Errorf("same scope: %v, want Same", got)
	}
}

func TestCompareValues(t *testing.T) {
	tests := []struct {
		a, b string
		want Result
	}{
		{"[parameters('location')]", "[parameters('location')]", Same},
		{"[parameters('location')]", "[ Parameters('Location') ]", Same},
		{"[resourceGroup().location]", "[resourceGroup().location]", Same},
		{"[parameters('location')]", "[resourceGroup().location]", Unknown},
		{"[parameters('location@m1')]", "[parameters('location@m2')]", Unknown},
		{"eastus", "EastUS", Same},
		{"eastus", "westus", Different},
		{"eastus", "[parameters('location')]", Unknown},
		{"", "[parameters('location')]", Unknown},
		{"", "", Unknown},
	}
	for _, tc := range tests {
		if got := CompareValues(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareValues(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestReadsClientIDIgnoresScopeSuffix(t *testing.T) {
	if !ReadsClientID("[parameters('uamiClientId@m1-a')]") {
		t.Error("scoped clientId parameter should still match")
	}
	if ReadsClientID("[parameters('name@clientId')]") {
		t.Error("scope text must not be read as the parameter name")
	}
}
