package sec

import (
	"reflect"
	"testing"
)

func TestNameSegments(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"Acct", []string{"acct"}},
		{"a/B/c", []string{"a", "b", "c"}},
		{"[format('{0}/{1}', 'Acct', 'Proj')]", []string{"acct", "proj"}},
		{"[format('{0}/{1}', parameters('n'), 'p')]", []string{"{parameters('n')}", "p"}},
		{"[format('{0}-x/{1}', 'a', variables('v'))]", []string{"a-x", "{variables('v')}"}},
		{"[parameters('n')]", []string{"{parameters('n')}"}},
		{"[ Parameters( 'n' ) ]", []string{"{parameters('n')}"}},
		{"[concat('a', '/b')]", []string{"{concat('a','/b')}"}},
		{"[format('{9}', 'a')]", []string{"{format('{9}','a')}"}},
		{"[format('{x}', 'a')]", []string{"{format('{x}','a')}"}},
		{"[format(variables('f'), 'a')]", []string{"{format(variables('f'),'a')}"}},
	}
	for _, tt := range tests {
		if got := nameSegments(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("nameSegments(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLastNameAndIdentity(t *testing.T) {
	if got := lastName("[format('{0}/{1}', 'a', 'Conn')]"); got != "conn" {
		t.Errorf("lastName = %q", got)
	}
	if got := lastName("[format('{0}/{1}', 'a', parameters('c'))]"); got != "" {
		t.Errorf("lastName of expression = %q", got)
	}
	if got := lastName(""); got != "" {
		t.Errorf("lastName empty = %q", got)
	}
	if identityKey("T/x", "") != "" || identityKey("", "n") != "" {
		t.Error("identityKey of empty input")
	}
	if got := identityKey("Microsoft.Search/searchServices", "S"); got != "microsoft.search/searchservices|s" {
		t.Errorf("identityKey = %q", got)
	}
}

func TestIDKey(t *testing.T) {
	want := "microsoft.search/searchservices|srch"
	tests := []struct {
		in, want string
	}{
		{"[resourceId('Microsoft.Search/searchServices', 'Srch')]", want},
		{"[resourceId('rg', 'Microsoft.Search/searchServices', 'srch')]", want},
		{"[resourceId(subscriptionId(), 'rg', 'Microsoft.Search/searchServices', 'srch')]", want},
		{"/subscriptions/s/resourceGroups/r/providers/Microsoft.Search/searchServices/Srch", want},
		{"/subscriptions/s/resourceGroups/r/providers/Microsoft.CognitiveServices/accounts/a/projects/p",
			"microsoft.cognitiveservices/accounts/projects|a/p"},
		{"[resourceId('Microsoft.Search/searchServices', parameters('n'))]", "microsoft.search/searchservices|{parameters('n')}"},
		{"[resourceId('Microsoft.Search/searchServices')]", ""},
		{"[resourceId('nothing', 'x')]", ""},
		{"[parameters('id')]", ""},
		{"[reference('x').id]", ""},
		{"/subscriptions/s/no-providers", ""},
		{"/subscriptions/s/providers/Microsoft.Search/searchServices", ""},
		{"plain", ""},
	}
	for _, tt := range tests {
		if got := idKey(tt.in); got != tt.want {
			t.Errorf("idKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitHelpers(t *testing.T) {
	for _, bad := range []string{"[f('a)]", "[f(a))]", "[f(a]", "[]", "[1+2]", "[a b(c)]", "[f(a)", "x"} {
		if _, _, ok := splitCall(bad); ok {
			t.Errorf("splitCall(%q) accepted", bad)
		}
	}
	fn, args, ok := splitCall("[Format('{0}, x', f(1, 2), 'it''s', [1,2])]")
	if !ok || fn != "format" || len(args) != 4 {
		t.Fatalf("splitCall = %q %q %v", fn, args, ok)
	}
	if s, ok := quoted(args[2]); !ok || s != "it's" {
		t.Errorf("quoted = %q %v", s, ok)
	}
	for _, bad := range []string{"x", "'", "'a'b'", ""} {
		if _, ok := quoted(bad); ok {
			t.Errorf("quoted(%q) accepted", bad)
		}
	}
	if a, ok := splitArgs(""); !ok || len(a) != 0 {
		t.Errorf("splitArgs empty = %v %v", a, ok)
	}
	if a, ok := splitArgs("a,"); !ok || len(a) != 2 {
		t.Errorf("splitArgs trailing = %q %v", a, ok)
	}
}

func TestParseRangeAndHelpers(t *testing.T) {
	if _, err := parseRange("1.2.3.4/24"); err != nil {
		t.Error(err)
	}
	if p, _ := parseRange("1.2.3.77/24"); p.String() != "1.2.3.0/24" {
		t.Errorf("masked = %s", p)
	}
	if isSafeToken("") || isSafeToken("a b") || !isSafeToken("a_b-1") {
		t.Error("isSafeToken")
	}
	if got := dedupe([]string{"a", "a", "b"}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("dedupe = %v", got)
	}
	if safeAPI("2025-01-01") != "2025-01-01" || safeAPI("x y") != "(unknown)" {
		t.Error("safeAPI")
	}
}
