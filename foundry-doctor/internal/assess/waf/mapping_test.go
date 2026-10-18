package waf

import (
	"context"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func loadCatalogue(t *testing.T) []catalog.Rule {
	t.Helper()
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	return rules
}

// TestMappingCoversEveryCatalogueRule checks the mapping file against the real
// catalogue. It does not use Definitions' own selection, so a rule added to the
// catalogue without a mapping entry fails here.
func TestMappingCoversEveryCatalogueRule(t *testing.T) {
	rules := loadCatalogue(t)
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m, rules); err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, e := range m.Mapped {
		listed[e.Rule] = true
	}
	for _, e := range m.Excluded {
		listed[e.Rule] = true
	}
	for _, r := range rules {
		if !listed[r.ID] {
			t.Errorf("catalogue rule %s is neither mapped nor excluded", r.ID)
		}
	}
	defs, err := Definitions(rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != len(m.Mapped) {
		t.Fatalf("definitions=%d mapped=%d", len(defs), len(m.Mapped))
	}
	for _, d := range defs {
		if d.Pillar == "" || d.ControlID == "" || d.ControlTitle == "" {
			t.Fatalf("incomplete definition: %+v", d)
		}
	}
}

// Phase 1-3 rules that carry deployed or local evidence must be in the view
// (review round 1: CFG/ENV/DEP/RUN were silently excluded).
func TestPhaseOneToThreeRulesAreMapped(t *testing.T) {
	defs, err := Definitions(loadCatalogue(t))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range defs {
		got[d.RuleID] = true
	}
	for _, id := range []string{"FND-CFG-005", "FND-CFG-007", "FND-ENV-003", "FND-DEP-003", "FND-RUN-001", "FND-RUN-007"} {
		if !got[id] {
			t.Errorf("%s missing from WAF definitions", id)
		}
	}
}

func TestValidateRejectsGaps(t *testing.T) {
	rules := []catalog.Rule{
		{ID: "FND-SEC-001", Pillar: "security", Category: "must-have", Phases: []string{"1"}, Status: catalog.StatusVerified},
		{ID: "FND-SEC-002", Pillar: "security", Category: "nice-to-have", Phases: []string{"1"}, Status: catalog.StatusVerified},
		{ID: "FND-GW-001", Pillar: "security", Category: "must-have", Phases: []string{"8"}, Status: catalog.StatusVerified},
	}
	base := func() Mapping {
		return Mapping{
			Version: 1, LastReviewed: "2026-10-18",
			PillarSource: Source{URL: "https://learn.microsoft.com/azure/well-architected/pillars", LastVerified: "2026-10-18"},
			Mapped:       []Mapped{{Rule: "FND-SEC-001", Pillar: "security"}, {Rule: "FND-SEC-002", Pillar: "security"}},
			Excluded:     []Excluded{{Rule: "FND-GW-001", Reason: "phase 8"}},
		}
	}
	if err := Validate(base(), rules); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	cases := map[string]func(*Mapping){
		"unmapped rule": func(m *Mapping) { m.Mapped = m.Mapped[:1] },
		"wrong pillar":  func(m *Mapping) { m.Mapped[1].Pillar = "reliability" },
		"duplicate":     func(m *Mapping) { m.Excluded = append(m.Excluded, Excluded{Rule: "FND-SEC-002", Reason: "x"}) },
		"phase-1 excluded": func(m *Mapping) {
			m.Mapped = m.Mapped[:1]
			m.Excluded = append(m.Excluded, Excluded{Rule: "FND-SEC-002", Reason: "x"})
		},
		"exclusion no reason": func(m *Mapping) { m.Excluded[0].Reason = "" },
		"unknown rule":        func(m *Mapping) { m.Mapped = append(m.Mapped, Mapped{Rule: "FND-SEC-999", Pillar: "security"}) },
		"costly must-have": func(m *Mapping) {
			m.Costly = []Costly{{Rule: "FND-SEC-001", Basis: "b", Source: "https://learn.microsoft.com/x", LastVerified: "2026-10-18"}}
		},
		"costly unsourced": func(m *Mapping) { m.Costly = []Costly{{Rule: "FND-SEC-002", Basis: "b"}} },
		"question unmapped": func(m *Mapping) {
			m.Questions = []Question{{Rule: "FND-GW-001", Prompt: "p"}}
		},
	}
	for name, mutate := range cases {
		m := base()
		mutate(&m)
		if err := Validate(m, rules); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCostlyIsSourcedNotPillarDriven(t *testing.T) {
	defs, err := Definitions(loadCatalogue(t))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]assess.Definition{}
	for _, d := range defs {
		byID[d.RuleID] = d
	}
	for _, id := range []string{"FND-COST-001", "FND-COST-002", "FND-OPS-001"} {
		if byID[id].Costly {
			t.Errorf("%s must not be costly", id)
		}
	}
	for _, d := range defs {
		if d.Costly && (d.CostBasis == "" || !strings.HasPrefix(d.CostSourceURL, "https://") || d.CostVerified == "") {
			t.Errorf("%s costly without sourced basis: %+v", d.RuleID, d)
		}
	}
	if !byID["FND-SEC-010"].Costly {
		t.Error("FND-SEC-010 should carry a sourced cost flag")
	}
}

func TestQuestionDefinitions(t *testing.T) {
	defs, err := Definitions(loadCatalogue(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.RuleID != "FND-REL-007" {
			continue
		}
		if d.QuestionKey != "disasterRecovery.declared" || d.QuestionReferenceKey != "disasterRecovery.reference" || d.NoEvidenceState != assess.StateQuestion {
			t.Fatalf("REL-007 definition=%+v", d)
		}
		return
	}
	t.Fatal("FND-REL-007 not mapped")
}

func TestParseRejectsUnknownFields(t *testing.T) {
	if _, err := parse([]byte("version: 1\nbogus: true\n")); err == nil {
		t.Fatal("expected unknown-field error")
	}
}
