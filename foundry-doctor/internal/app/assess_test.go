package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestAssessWAF(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{
		Findings:  []sdk.Finding{{RuleID: "FND-IDN-001", Severity: sdk.SeverityError, Evidence: "identity missing"}},
		Skipped:   []sdk.Skip{{RuleID: "FND-REL-007", Reason: sdk.SkipNotImplemented}},
		Evaluated: []string{"FND-IDN-001"},
	}}
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {Profile: "foundry-prod"}}
	var out bytes.Buffer
	code, err := AssessWAF(context.Background(), svc, AssessRequest{Audience: AssessAudienceEvidence, Format: "json"}, &out)
	if err != nil || code != ExitFindings {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(out.String(), `"framework": "azure-waf"`) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestAssessWAFValidation(t *testing.T) {
	svc, _ := newSvc(&fakeEngine{})
	for _, tc := range []AssessRequest{
		{Audience: "bogus"},
		{Audience: AssessAudienceEvidence, Format: "html"},
		{Audience: AssessAudienceOwner, Format: "json"},
		{Audience: AssessAudienceEvidence, Format: "json", LLMExplain: true},
	} {
		if code, err := AssessWAF(context.Background(), svc, tc, &bytes.Buffer{}); err == nil || code != ExitUnavailable {
			t.Fatalf("req=%+v code=%d err=%v", tc, code, err)
		}
	}
}

func TestAssessWAFIgnoresLLMFlagsWithoutOptIn(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Evaluated: []string{"FND-IDN-001"}}}
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {Profile: "foundry-prod"}}
	var out bytes.Buffer
	code, err := AssessWAF(context.Background(), svc, AssessRequest{
		Audience: AssessAudienceOwner,
		Format:   "markdown",
		// Ignored because --llm-explain is false.
		LLMTimeout: 99 * time.Second,
	}, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v out=%s", code, err, out.String())
	}
}

func runAssess(t *testing.T, eng *fakeEngine, req AssessRequest) string {
	t.Helper()
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {Profile: "foundry-prod"}}
	var out bytes.Buffer
	if _, err := AssessWAF(context.Background(), svc, req, &out); err != nil {
		t.Fatalf("req=%+v err=%v", req, err)
	}
	return out.String()
}

// Review round 1 (lead B4, black belt B6): owner and developer HTML differ.
func TestAssessWAFHTMLAudiencesDiffer(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{
		Findings:  []sdk.Finding{{RuleID: "FND-IDN-001", Severity: sdk.SeverityError, Evidence: "identity missing"}},
		Evaluated: []string{"FND-IDN-001"},
	}}
	owner := runAssess(t, eng, AssessRequest{Audience: AssessAudienceOwner, Format: "html"})
	dev := runAssess(t, eng, AssessRequest{Audience: AssessAudienceDeveloper, Format: "html"})
	if owner == dev {
		t.Fatal("owner and developer HTML are identical")
	}
	if !strings.Contains(owner, "Top five actions") || strings.Contains(owner, "Evidence basis") {
		t.Fatal("owner HTML is not the owner summary")
	}
	if !strings.Contains(dev, "Evidence basis") || !strings.Contains(dev, "identity missing") {
		t.Fatal("developer HTML lacks evidence detail")
	}
}

// Review round 1 (lead B1/N6, black belt B4/B7/N10): partial skips are not PASS
// and the limitations disclose PSRule/Advisor and the offline scope.
func TestAssessWAFEvidencePackDisclosesScope(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{
		Evaluated: []string{"FND-REL-002"},
		Skipped:   []sdk.Skip{{RuleID: "FND-REL-002", Reason: "unresolved-expression: storage st"}},
	}}
	out := runAssess(t, eng, AssessRequest{Audience: AssessAudienceEvidence, Format: "json"})
	var pack struct {
		ToolVersion string                       `json:"toolVersion"`
		GeneratedAt string                       `json:"generatedAt"`
		Limitations []string                     `json:"limitations"`
		Controls    map[string][]json.RawMessage `json:"controls"`
	}
	if err := json.Unmarshal([]byte(out), &pack); err != nil {
		t.Fatal(err)
	}
	if pack.GeneratedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("generatedAt=%q", pack.GeneratedAt)
	}
	all := strings.Join(pack.Limitations, "\n")
	for _, want := range []string{"Azure Advisor", "were not imported", "Deployed Azure state was not queried", "FND-DEP-001", "Compiled infrastructure"} {
		if !strings.Contains(all, want) {
			t.Errorf("limitations missing %q:\n%s", want, all)
		}
	}
	for _, raw := range pack.Controls["pass"] {
		if strings.Contains(string(raw), `"FND-REL-002"`) {
			t.Fatal("partly skipped control reported as PASS")
		}
	}
	found := false
	for _, raw := range pack.Controls["unknown"] {
		found = found || strings.Contains(string(raw), `"FND-REL-002"`)
	}
	if !found {
		t.Fatal("partly skipped control not reported as UNKNOWN")
	}
}
