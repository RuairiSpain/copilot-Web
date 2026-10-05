package rules_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

func selReg(t *testing.T) *rules.Registry {
	t.Helper()
	return mustRegistry(t, testCatalog(),
		fakeRule{id: "FND-SEC-001", version: 1}, fakeRule{id: "FND-SEC-002", version: 1}, fakeRule{id: "FND-NET-001", version: 1})
}

func ids(es []rules.Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Meta.ID)
	}
	return out
}

func TestSelect(t *testing.T) {
	reg := selReg(t)
	tests := []struct {
		name string
		expr []string
		want []string
	}{
		{"empty selects all executable", nil, []string{"FND-NET-001", "FND-SEC-001", "FND-SEC-002"}},
		{"id", []string{"FND-SEC-001"}, []string{"FND-SEC-001"}},
		{"id lower case", []string{"fnd-sec-001"}, []string{"FND-SEC-001"}},
		{"glob", []string{"FND-SEC-*"}, []string{"FND-SEC-001", "FND-SEC-002"}},
		{"group bare", []string{"net"}, []string{"FND-NET-001"}},
		{"group prefixed", []string{"group:SEC"}, []string{"FND-SEC-001", "FND-SEC-002"}},
		{"must-have", []string{"must-have"}, []string{"FND-NET-001", "FND-SEC-001"}},
		{"nice-to-have", []string{"category:nice-to-have"}, []string{"FND-SEC-002"}},
		{"phase", []string{"phase:1"}, []string{"FND-NET-001", "FND-SEC-001", "FND-SEC-002"}},
		{"exclude only starts from all", []string{"!FND-SEC-001"}, []string{"FND-NET-001", "FND-SEC-002"}},
		{"include minus exclude", []string{"sec,!nice-to-have"}, []string{"FND-SEC-001"}},
		{"repeated flag unions", []string{"FND-NET-001", "FND-SEC-002"}, []string{"FND-NET-001", "FND-SEC-002"}},
		{"exclusion wins over inclusion", []string{"FND-SEC-*,!FND-SEC-*"}, nil},
		{"spaces tolerated", []string{" sec , ! FND-SEC-002 "}, []string{"FND-SEC-001"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := rules.ParseSelector(tc.expr...)
			if err != nil {
				t.Fatal(err)
			}
			got, err := reg.Select(sel)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("got %v want %v", ids(got), tc.want)
			}
		})
	}
}

func TestParseSelectorErrors(t *testing.T) {
	for _, expr := range []string{"a,,b", "!", "FND-SEC-[", "phase:x", "category:other", "group:1", "FND-SEC-1x", "12"} {
		if _, err := rules.ParseSelector(expr); err == nil {
			t.Errorf("%q accepted", expr)
		}
	}
	sel, err := rules.ParseSelector("sec, !FND-SEC-001")
	if err != nil || sel.String() != "sec,!FND-SEC-001" || sel.IsZero() {
		t.Fatalf("String/IsZero: %q %v", sel, err)
	}
	if z, _ := rules.ParseSelector(""); !z.IsZero() {
		t.Fatal("empty expression must give the zero selector")
	}
}

func TestSelectValidation(t *testing.T) {
	reg := selReg(t)
	tests := []struct{ expr, want string }{
		{"FND-ZZZ-001", "matches no catalogue rule"},
		{"zzz", "matches no catalogue rule"},
		{"!FND-ZZZ-*", "matches no catalogue rule"},
		{"FND-OPS-001", "not implemented in this version"},
		{"phase:9", "matches no catalogue rule"},
	}
	for _, tc := range tests {
		sel, err := rules.ParseSelector(tc.expr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Select(sel); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want %q", tc.expr, err, tc.want)
		}
	}
	// Excluding a catalogued but unimplemented rule is fine; a glob over unimplemented rules is not an error.
	sel, _ := rules.ParseSelector("!FND-OPS-001,FND-OPS-*")
	if _, err := reg.Select(sel); err != nil {
		t.Errorf("exclude/glob of unimplemented rules: %v", err)
	}
}

func TestResolve(t *testing.T) {
	reg := selReg(t)
	packs := []rules.Pack{
		{Schema: "pack-v1", ID: "foundry-core", Version: 1, Rules: []string{"FND-NET-001", "FND-SEC-001", "FND-SEC-014"}},
		{Schema: "pack-v1", ID: "extra", Version: 1, Rules: []string{"FND-SEC-002"}},
		{Schema: "pack-v1", ID: "broken", Version: 1, Rules: []string{"FND-OPS-001"}},
	}
	cli := func(s string) rules.Selector { x, err := rules.ParseSelector(s); must(t, err); return x }
	tests := []struct {
		name string
		cfg  rules.Selection
		cli  rules.Selector
		want []string
		err  string
	}{
		{"default pack skips tool-owned rule", rules.Selection{}, rules.Selector{}, []string{"FND-NET-001", "FND-SEC-001"}, ""},
		{"named packs union", rules.Selection{Packs: []string{"foundry-core", "extra"}}, rules.Selector{}, []string{"FND-NET-001", "FND-SEC-001", "FND-SEC-002"}, ""},
		{"include adds", rules.Selection{Include: []string{"FND-SEC-002"}}, rules.Selector{}, []string{"FND-NET-001", "FND-SEC-001", "FND-SEC-002"}, ""},
		{"exclude removes", rules.Selection{Exclude: []string{"net"}}, rules.Selector{}, []string{"FND-SEC-001"}, ""},
		{"exclude beats include", rules.Selection{Include: []string{"sec"}, Exclude: []string{"FND-SEC-*"}}, rules.Selector{}, []string{"FND-NET-001"}, ""},
		{"cli replaces config", rules.Selection{Packs: []string{"extra"}}, cli("FND-NET-001"), []string{"FND-NET-001"}, ""},
		{"unknown pack", rules.Selection{Packs: []string{"nope"}}, rules.Selector{}, nil, `rule pack "nope" not found`},
		{"pack with unimplemented native rule", rules.Selection{Packs: []string{"broken"}}, rules.Selector{}, nil, "no implementation in this build"},
		{"negation in list", rules.Selection{Include: []string{"!sec"}}, rules.Selector{}, nil, "do not use !"},
		{"bad term in list", rules.Selection{Exclude: []string{"FND-SEC-["}}, rules.Selector{}, nil, "rules.exclude"},
		{"term matching nothing", rules.Selection{Include: []string{"zzz"}}, rules.Selector{}, nil, "matches no catalogue rule"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reg.Resolve(packs, tc.cfg, tc.cli)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error %v, want %q", err, tc.err)
				}
				return
			}
			must(t, err)
			if !slices.Equal(ids(got), tc.want) {
				t.Fatalf("got %v want %v", ids(got), tc.want)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
