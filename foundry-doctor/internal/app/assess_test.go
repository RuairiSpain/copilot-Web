package app

import (
	"bytes"
	"context"
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
