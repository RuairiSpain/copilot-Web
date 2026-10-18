package html

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func fixture() assess.Assessment {
	return assess.Assessment{
		Profile:     "foundry-prod",
		Decision:    "Proceed with conditions.",
		ScopeNote:   "azure.yaml only",
		Limitations: []string{"Not a certification.", "PSRule and Azure Advisor evidence is not imported."},
		Summary:     assess.Summary{States: map[assess.State]int{assess.StateFail: 1, assess.StateAttested: 1}},
		Controls: []assess.Control{
			{
				Title: `<img src=x onerror=1>`, RuleID: "FND-IDN-001", Pillar: "security", State: assess.StateFail, Mandatory: true,
				Basis: assess.BasisDeterministic, Recommendation: `Escape <script>`, Fix: "identity:\n  type: SystemAssigned",
				SourceURL: "https://learn.microsoft.com/example", LastVerified: "2026-10-05",
				Findings: []sdk.Finding{{Severity: sdk.SeverityError, Evidence: "identity missing", Location: sdk.Location{File: "infra/main.bicep", Line: 3}}},
			},
			{
				Title: "DR plan", RuleID: "FND-REL-007", Pillar: "reliability", State: assess.StateAttested, Basis: assess.BasisAttested,
				ProjectOpinion: true, QuestionAnswer: "true; reference: https://contoso.example/dr",
				WARAReferences: []assess.WARARef{{ID: "dff62efe-c3a3-4621-98b3-c877c65cb195", Label: "AI Search multi-region", PGVerified: "false", SourceURL: "https://github.com/Azure/x"}},
			},
			{
				Title: "Bad link", RuleID: "FND-OPS-002", Pillar: "operations", State: assess.StateUnknown, SourceURL: "javascript:alert(1)",
			},
		},
		TopActions: []assess.Action{{Title: "Managed identity", RuleID: "FND-IDN-001", Pillar: "operations", State: assess.StateFail, Mandatory: true, ProjectOpinion: true, Recommendation: "Use it."}},
		Costly:     []assess.Action{{Title: "Search replicas", RuleID: "FND-REL-001", Pillar: "reliability", State: assess.StateWarning, CostBasis: "Replicas are billed", CostSourceURL: "https://learn.microsoft.com/azure/search/search-sku-manage-costs", CostVerified: "2026-10-18"}},
		Unassessed: []assess.ControlRef{{Title: "Telemetry", Pillar: "operations", State: assess.StateSkipped, StateReason: "Skipped: not-implemented."}},
	}
}

func render(t *testing.T, f func(*bytes.Buffer, assess.Assessment) error) string {
	t.Helper()
	var b bytes.Buffer
	if err := f(&b, fixture()); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func ownerFn(b *bytes.Buffer, a assess.Assessment) error { return RenderOwner(b, a) }
func devFn(b *bytes.Buffer, a assess.Assessment) error   { return RenderDeveloper(b, a) }

// Documented WCAG-oriented checklist for the static pages: language set, one
// h1, labelled sections, table caption and header scope, state conveyed by text
// (not colour alone), dark-mode colours defined, no script, unsafe URLs neutralised.
func TestAccessibilityChecklistAndEscaping(t *testing.T) {
	for name, s := range map[string]string{"owner": render(t, ownerFn), "developer": render(t, devFn)} {
		for _, want := range []string{`<html lang="en">`, "<caption>", `scope="col"`, `scope="row"`, "aria-labelledby=", "prefers-color-scheme: dark", "Limitations", "Not a certification."} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: missing %s", name, want)
			}
		}
		if strings.Count(s, "<h1>") != 1 {
			t.Errorf("%s: want exactly one h1", name)
		}
		for _, bad := range []string{"<script", "<img src=x onerror=1>", `href="javascript:`} {
			if strings.Contains(s, bad) {
				t.Errorf("%s: unsafe html %q", name, bad)
			}
		}
	}
}

func TestOwnerAndDeveloperDiffer(t *testing.T) {
	o, d := render(t, ownerFn), render(t, devFn)
	for _, want := range []string{"Top five actions", "Costly recommendations (optional)", "Replicas are billed", "Unassessed scope", "Skipped: not-implemented.", "Team-attested answers", "<strong>Project opinion</strong>"} {
		if !strings.Contains(o, want) {
			t.Errorf("owner missing %q", want)
		}
	}
	for _, want := range []string{"Evidence basis", "identity missing", "infra/main.bicep:3", "<pre><code>identity:\n  type: SystemAssigned</code></pre>",
		`href="https://learn.microsoft.com/example"`, "(verified 2026-10-05)", "dff62efe-c3a3-4621-98b3-c877c65cb195", "pgVerified: false",
		"Recorded answer (team attestation, not verified)", "<strong>Project opinion</strong>", "Operational Excellence"} {
		if !strings.Contains(d, want) {
			t.Errorf("developer missing %q", want)
		}
	}
	if strings.Contains(d, "Top five actions") || strings.Contains(o, "Evidence basis") {
		t.Error("owner and developer HTML must be materially different")
	}
}
