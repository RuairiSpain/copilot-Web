package rules_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	rulesdata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func TestCheckImplementations(t *testing.T) {
	cat := testCatalog()
	r := func(id string, v int) rules.Rule { return fakeRule{id: id, version: v} }
	tests := []struct {
		name     string
		impls    []rules.Rule
		complete bool
		want     []string // substrings; nil means no error
	}{
		{"partial ok when not required", []rules.Rule{r("FND-SEC-001", 1)}, false, nil},
		{"complete", []rules.Rule{r("FND-SEC-001", 1), r("FND-SEC-002", 1), r("FND-NET-001", 1)}, true, nil},
		{"missing when required", []rules.Rule{r("FND-SEC-001", 1)}, true, []string{"FND-SEC-002: phase 1 rule has no implementation", "FND-NET-001: phase 1 rule has no implementation"}},
		{"bicep-owned rule needs none", []rules.Rule{r("FND-SEC-001", 1), r("FND-SEC-002", 1), r("FND-NET-001", 1)}, true, nil},
		{"version mismatch", []rules.Rule{r("FND-SEC-001", 2)}, false, []string{"FND-SEC-001: implementation is version 2, catalogue is version 1"}},
		{"non phase 1 rejected", []rules.Rule{r("FND-OPS-001", 1)}, false, []string{"FND-OPS-001: implementation registered but the rule is not a phase 1 rule"}},
		{"tool-owned rejected", []rules.Rule{r("FND-SEC-014", 1)}, false, []string{"FND-SEC-014"}},
		{"unknown rejected", []rules.Rule{r("FND-ZZZ-999", 1)}, false, []string{"FND-ZZZ-999: implementation has no catalogue rule"}},
		{"duplicate", []rules.Rule{r("FND-SEC-001", 1), r("FND-SEC-001", 1)}, false, []string{"FND-SEC-001: 2 implementations registered, want exactly one"}},
		{"nil", []rules.Rule{nil}, false, []string{"nil rule implementation"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := rules.CheckImplementations(cat, tc.impls, rules.Phase1, tc.complete)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestRegistryAccessors(t *testing.T) {
	cat := testCatalog()
	reg := mustRegistry(t, cat, fakeRule{id: "FND-SEC-001", version: 1})
	if got := len(reg.Entries()); got != 5 {
		t.Fatalf("entries = %d", got)
	}
	if got := reg.Executable(); len(got) != 1 || got[0].Meta.ID != "FND-SEC-001" {
		t.Fatalf("executable = %v", got)
	}
	if _, ok := reg.Get("FND-NET-001"); !ok {
		t.Fatal("Get known")
	}
	if _, ok := reg.Get("FND-NOPE-001"); ok {
		t.Fatal("Get unknown")
	}
	if got := reg.Catalog(); len(got) != 5 || got[0].ID != "FND-NET-001" {
		t.Fatalf("catalog not sorted: %v", got[0].ID)
	}
	err := reg.CheckComplete(rules.Phase1)
	if err == nil || !strings.Contains(err.Error(), "FND-NET-001") || !strings.Contains(err.Error(), "FND-SEC-002") {
		t.Fatalf("CheckComplete = %v", err)
	}
	if _, err := rules.NewRegistry(cat, []rules.Rule{fakeRule{id: "FND-OPS-001", version: 1}}, rules.Phase1); err == nil {
		t.Fatal("registry accepted a non-phase-1 implementation")
	}
	full := mustRegistry(t, cat, fakeRule{id: "FND-SEC-001", version: 1}, fakeRule{id: "FND-SEC-002", version: 1}, fakeRule{id: "FND-NET-001", version: 1})
	if err := full.CheckComplete(rules.Phase1); err != nil {
		t.Fatal(err)
	}
}

func TestPhaseRules(t *testing.T) {
	dropped := meta("FND-SEC-009", "SEC", func(m *catalog.Rule) { m.Status = catalog.StatusDropped })
	got := rules.PhaseRules(append(testCatalog(), dropped), rules.Phase1)
	if len(got) != 3 {
		t.Fatalf("phase rules = %d, want 3 (native, phase 1, not dropped)", len(got))
	}
}

func TestLoadCatalogEmbedded(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat) != 108 {
		t.Errorf("catalogue has %d rules, want 108", len(cat))
	}
	if n := len(rules.PhaseRules(cat, rules.Phase1)); n != 37 {
		t.Errorf("native phase 1 rules = %d, want 37 (38 minus the Bicep-owned FND-SEC-014)", n)
	}
	reg, err := rules.NewRegistry(cat, nil, rules.Phase1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Executable()) != 0 {
		t.Fatal("no implementations registered yet")
	}
}

func TestLoadCatalogErrors(t *testing.T) {
	if _, err := rules.LoadCatalog(context.Background(), fstest.MapFS{"c/sec/FND-SEC-001.yaml": {Data: []byte("id: [")}}, "c"); err == nil {
		t.Fatal("malformed YAML accepted")
	}
	if _, err := rules.LoadCatalog(context.Background(), fstest.MapFS{}, "c"); err == nil {
		t.Fatal("missing root accepted")
	}
	// Loads but fails validation (empty catalogue).
	if _, err := rules.LoadCatalog(context.Background(), fstest.MapFS{"c/x/.keep.txt": {}}, "c"); err == nil {
		t.Fatal("invalid catalogue accepted")
	}
}
