package cfg_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	catalogfs "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func allScenarios() map[string]scenarioSet {
	return map[string]scenarioSet{
		"FND-CFG-001": scenarios001(),
		"FND-CFG-002": scenarios002(),
		"FND-CFG-003": scenarios003(),
		"FND-CFG-004": scenarios004(),
		"FND-CFG-005": scenarios005(),
		"FND-CFG-006": scenarios006(),
		"FND-CFG-007": scenarios007(),
		"FND-CFG-011": scenarios011(),
		"FND-CFG-012": scenarios012(),
	}
}

// TestScenarios runs every catalogue scenario as a subtest named exactly like the catalogue entry.
func TestScenarios(t *testing.T) {
	all := allScenarios()
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			names := make([]string, 0, len(all[id]))
			for n := range all[id] {
				names = append(names, n)
			}
			slices.Sort(names)
			for _, n := range names {
				t.Run(n, all[id][n])
			}
		})
	}
}

// TestScenarioCoverage fails if a catalogue scenario has no subtest, or a subtest names no scenario.
func TestScenarioCoverage(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	all := allScenarios()
	seen := map[string]bool{}
	for _, m := range cat {
		if m.Group != "CFG" || !rules.NeedsImplementation(m, rules.Phase1) {
			continue
		}
		seen[m.ID] = true
		have := all[m.ID]
		if have == nil {
			t.Errorf("%s: no scenarios", m.ID)
			continue
		}
		var want []string
		want = append(want, m.Tests.Positive...)
		want = append(want, m.Tests.Negative...)
		want = append(want, m.Tests.Skipped...)
		want = append(want, m.Tests.Uncertain...)
		for _, n := range want {
			if have[n] == nil {
				t.Errorf("%s: catalogue scenario %q has no subtest", m.ID, n)
			}
		}
		for n := range have {
			if !slices.Contains(want, n) {
				t.Errorf("%s: subtest %q is not a catalogue scenario", m.ID, n)
			}
		}
	}
	for id := range all {
		if !seen[id] {
			t.Errorf("%s: scenarios for a rule that needs no implementation", id)
		}
	}
}

// TestRegistry checks the implementations against the catalogue the way the engine does.
func TestRegistry(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), catalogfs.FS, catalogfs.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := rules.CheckImplementations(cat, cfgRules(), rules.Phase1, false); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range cfgRules() {
		ids = append(ids, r.ID())
	}
	if !slices.IsSorted(ids) || len(ids) != 9 {
		t.Fatalf("rules not in ID order or incomplete: %v", ids)
	}
}
