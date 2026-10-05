package graph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseExpressionOK(t *testing.T) {
	tests := []struct {
		name, in, canon string
	}{
		{"string", "['a']", "'a'"},
		{"escaped quote", "['it''s']", "'it''s'"},
		{"number", "[-12.5]", "-12.5"},
		{"call", "[ResourceId( 'A/b' , 'n' )]", "resourceid('A/b','n')"},
		{"nested", "[concat('a',parameters('p'))]", "concat('a',parameters('p'))"},
		{"member", "[reference('x').outputs.id]", "reference('x').outputs.id"},
		{"index", "[parameters('l')[0]]", "parameters('l')[0]"},
		{"empty call", "[subscription()]", "subscription()"},
		{"unicode ident", "[nämé]", "nämé"},
		{"unicode string", "['日本']", "'日本'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := parseExpression(tt.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got := canonical(e); got != tt.canon {
				t.Errorf("canonical = %q, want %q", got, tt.canon)
			}
		})
	}
}

func TestParseExpressionErrors(t *testing.T) {
	deep := "[" + strings.Repeat("f(", 70) + "1" + strings.Repeat(")", 70) + "]"
	chain := "[a" + strings.Repeat(".b", 100) + "]"
	tests := []struct {
		name, in, reason string
	}{
		{"not expression", "plain", ReasonNotExpression},
		{"escaped bracket", "[[x]", ReasonNotExpression},
		{"empty", "[]", ReasonUnexpectedEnd},
		{"unterminated string", "['abc]", ReasonUnterminated},
		{"missing paren", "[f('a'", ReasonNotExpression},
		{"unclosed call", "[f('a' ]", ReasonUnexpectedEnd},
		{"trailing", "[f() g]", ReasonTrailing},
		{"bad member", "[a.]", ReasonUnexpectedEnd},
		{"bad member 2", "[a.1]", ReasonUnexpected},
		{"bad index", "[a[1]", ReasonUnexpectedEnd},
		{"lone minus", "[-]", ReasonUnexpected},
		{"deep", deep, ReasonDepthLimit},
		{"long chain", chain, ReasonDepthLimit},
		{"too long", "[" + strings.Repeat("a", maxExprLen) + "]", ReasonLengthLimit},
		{"call missing close", "[f(1", ReasonNotExpression},
		{"args end", "[f(1,]", ReasonUnexpectedEnd},
		{"bad utf8", "[\xff]", ReasonUnexpected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseExpression(tt.in)
			var pe *ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want ParseError", err)
			}
			if pe.Reason != tt.reason {
				t.Errorf("reason = %q, want %q", pe.Reason, tt.reason)
			}
			if strings.Contains(pe.Error(), "abc") {
				t.Errorf("error leaks text: %q", pe.Error())
			}
		})
	}
}

func TestDepthBoundary(t *testing.T) {
	ok := "[" + strings.Repeat("f(", maxExprDepth) + "1" + strings.Repeat(")", maxExprDepth) + "]"
	if _, err := parseExpression(ok); err != nil {
		t.Errorf("depth %d should parse: %v", maxExprDepth, err)
	}
}

func TestAnalyzeExpr(t *testing.T) {
	tests := []struct {
		name, in string
		want     []finding
	}{
		{"resourceId", "[resourceId('Microsoft.Storage/storageAccounts', 'sa')]",
			[]finding{{kind: findResource, key: "microsoft.storage/storageaccounts/sa"}}},
		{"resourceId with sub and rg", "[resourceId(subscription().subscriptionId, 'rg', 'Microsoft.Network/virtualNetworks/subnets', 'v', 's')]",
			[]finding{{kind: findResource, key: "microsoft.network/virtualnetworks/subnets/v/s"}, {kind: findUnresolved, reason: "unsupported-function:subscription"}}},
		{"resourceId expression name", "[resourceId('A.B/c', parameters('n'))]",
			[]finding{{kind: findResource, key: "a.b/c/parameters('n')"}, {kind: findParameter, key: "n"}}},
		{"concat folded", "[resourceId('A.B/c', concat('x','y'))]",
			[]finding{{kind: findResource, key: "a.b/c/xy"}}},
		{"resourceId no type", "[resourceId(parameters('t'), 'n')]",
			[]finding{{kind: findUnresolved, reason: ReasonUnresolvedTarget}, {kind: findParameter, key: "t"}}},
		{"resourceId no name", "[resourceId('A.B/c')]", []finding{{kind: findUnresolved, reason: ReasonUnresolvedTarget}}},
		{"reference string", "[reference('sym').x]", []finding{{kind: findNamed, key: "sym"}}},
		{"reference resourceId", "[reference(resourceId('A.B/c','n'))]",
			[]finding{{kind: findResource, key: "a.b/c/n"}}},
		{"reference dynamic", "[reference(variables('v'))]",
			[]finding{{kind: findUnresolved, reason: ReasonUnresolvedTarget}, {kind: findUnresolved, reason: "unsupported-function:variables"}}},
		{"reference empty", "[reference()]", []finding{{kind: findUnresolved, reason: ReasonMalformedExpression}}},
		{"parameters dynamic", "[parameters(variables('x'))]",
			[]finding{{kind: findUnresolved, reason: ReasonUnresolvedTarget}, {kind: findUnresolved, reason: "unsupported-function:variables"}}},
		{"unsupported", "[uniqueString('a')]", []finding{{kind: findUnresolved, reason: "unsupported-function:uniquestring"}}},
		{"malformed", "[resourceId(", []finding{{kind: findUnresolved, reason: ReasonMalformedExpression}}},
		{"index base", "[parameters('l')[parameters('i')]]",
			[]finding{{kind: findParameter, key: "l"}, {kind: findParameter, key: "i"}}},
		{"literal only", "['x']", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := analyzeExpr(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestFold(t *testing.T) {
	e, _ := parseExpression("[concat('a', 1, concat('b'))]")
	if s, ok := fold(e); !ok || s != "a1b" {
		t.Errorf("fold = %q, %v", s, ok)
	}
	e, _ = parseExpression("[concat()]")
	if _, ok := fold(e); ok {
		t.Error("empty concat must not fold")
	}
	e, _ = parseExpression("[x]")
	if _, ok := fold(e); ok {
		t.Error("ident must not fold")
	}
}

func FuzzParseExpression(f *testing.F) {
	for _, s := range []string{
		"[resourceId('A/b','n')]", "[concat('a',parameters('p'))]", "['it''s']", "[a.b[0].c]", "[", "[]", "[f(", "['x",
		"[[x]", "[-1.5e]", "[f(f(f(1)))]", "[\xff]",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		e, err := parseExpression(s)
		if err != nil {
			var pe *ParseError
			if !errors.As(err, &pe) || pe.Offset < 0 || pe.Offset > len(s) {
				t.Fatalf("bad error %v", err)
			}
			return
		}
		_ = canonical(e)
		_ = analyzeExpr(s)
	})
}

func FuzzScanInterpolations(f *testing.F) {
	for _, s := range []string{"${A}", "${A:-b}", "${{ x }}", "$${A}", "${", "${{", "${A:-${B}}", "$", "$$$", "${1}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, it := range ScanInterpolations(s) {
			if it.Offset < 0 || it.Offset >= len(s) {
				t.Fatalf("offset %d out of range", it.Offset)
			}
		}
	})
}
