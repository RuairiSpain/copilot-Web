package azurenames_test

import (
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/azurenames"
)

func TestRules(t *testing.T) {
	cases := []struct {
		kind      string
		good, bad []string
	}{
		{"storage", []string{"stfin01", "abc"}, []string{"ab", "St-Fin", strings.Repeat("x", 25), "st_fin"}},
		{"key-vault", []string{"kv-fin", "kvfin1"}, []string{"1kv", "kv--x", "kv-", "ab"}},
		{"search", []string{"srch-1", "ab"}, []string{"-ab", "Ab", "ab-", "a--b"}},
		{"redis", []string{"r", "Redis-1"}, []string{"-r", "r--1", strings.Repeat("x", 64)}},
		{"cosmos", []string{"cosmos-1", "abc"}, []string{"ab", "Cosmos", "-abc", "abc-", strings.Repeat("x", 45)}},
		{"apim", []string{"apim1", "a"}, []string{"1apim", "apim-"}},
		{"container-app", []string{"app-1", "ab"}, []string{"App", "1app", "a--b", strings.Repeat("x", 33)}},
		{"storage-container", []string{"abc", "a-b-c"}, []string{"ab", "A-b", "a--b", "-ab"}},
		{"registry", []string{"acr12345"}, []string{"acr-1", "abcd"}},
		{"service-bus", []string{"bus-name"}, []string{"1bus-name", "bus"}},
		{"managed-identity", []string{"id-fin_1"}, []string{"ab", "-id"}},
		{"resource-group", []string{"rg-fin", "rg.(1)"}, []string{"rg.", ""}},
	}
	for _, c := range cases {
		for _, name := range c.good {
			if p := azurenames.Problems(c.kind, name); len(p) != 0 {
				t.Errorf("%s %q should be valid: %v", c.kind, name, p)
			}
		}
		for _, name := range c.bad {
			if p := azurenames.Problems(c.kind, name); len(p) == 0 {
				t.Errorf("%s %q should be invalid", c.kind, name)
			}
		}
	}
}

func TestEveryRuleIsKeyedByItsKind(t *testing.T) {
	for kind, rule := range azurenames.Rules {
		if rule.Kind != kind {
			t.Errorf("%s keyed as %s", rule.Kind, kind)
		}
	}
}

func TestUnknownKindPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for an unknown kind")
		}
	}()
	azurenames.Problems("nope", "x")
}
