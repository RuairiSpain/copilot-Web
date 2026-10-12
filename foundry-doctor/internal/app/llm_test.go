package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fakeNarrator struct {
	ruleCalled   bool
	assessCalled bool
	narrative    llm.Narrative
	err          error
}

func (f *fakeNarrator) ExplainRule(_ context.Context, _ string, _ llm.Audience, _ llm.Options) (llm.Narrative, error) {
	f.ruleCalled = true
	return f.narrative, f.err
}

func (f *fakeNarrator) ExplainAssessment(_ context.Context, _ assess.Assessment, _ llm.Audience, _ llm.Options) (llm.Narrative, error) {
	f.assessCalled = true
	return f.narrative, f.err
}

func TestExplainRuleDefaultOff(t *testing.T) {
	svc := Services{Explainer: fakeExplainer{}}
	n := &fakeNarrator{}
	svc.Narrator = n
	var out bytes.Buffer
	code, err := ExplainRule(context.Background(), svc, ExplainRequest{RuleID: "FND-CFG-001", Format: "markdown"}, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if n.ruleCalled {
		t.Fatal("narrator should not be called without explicit opt-in")
	}
}

func TestExplainRuleAppendsNarrativeAndFallback(t *testing.T) {
	svc := Services{Explainer: fakeExplainer{}}
	n := &fakeNarrator{narrative: llm.Narrative{
		Summary:  "Advisory summary.",
		Sections: []llm.Section{{Heading: "Priority", Summary: "Do the fix.", Actions: []string{"Rotate config"}, RuleIDs: []string{"FND-CFG-001"}}},
		Metadata: llm.Metadata{Provider: "fake", PromptVersion: "v1"},
	}}
	svc.Narrator = n
	var out bytes.Buffer
	code, err := ExplainRule(context.Background(), svc, ExplainRequest{
		RuleID: "FND-CFG-001", Format: "markdown", LLMExplain: true, LLMAudience: llm.AudienceOwner, LLMTimeout: time.Second,
	}, &out)
	if err != nil || code != ExitOK || !strings.Contains(out.String(), "Advisory LLM narrative") {
		t.Fatalf("code=%d err=%v out=%s", code, err, out.String())
	}

	n.err = errors.New("provider down")
	out.Reset()
	code, err = ExplainRule(context.Background(), svc, ExplainRequest{
		RuleID: "FND-CFG-001", Format: "console", LLMExplain: true, LLMAudience: llm.AudienceDeveloper, LLMTimeout: time.Second,
	}, &out)
	if err != nil || code != ExitOK || !strings.Contains(out.String(), "Deterministic rule documentation remains authoritative") {
		t.Fatalf("code=%d err=%v out=%s", code, err, out.String())
	}
}

func TestAssessWAFFallbackKeepsExitCode(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{
		Findings:  []sdk.Finding{{RuleID: "FND-IDN-001", Severity: sdk.SeverityError, Evidence: "identity missing"}},
		Evaluated: []string{"FND-IDN-001"},
	}}
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {Profile: "foundry-prod"}}
	svc.Narrator = &fakeNarrator{err: errors.New("timeout")}
	var out bytes.Buffer
	code, err := AssessWAF(context.Background(), svc, AssessRequest{
		Audience: AssessAudienceOwner, Format: "markdown", LLMExplain: true, LLMTimeout: time.Second,
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if code != ExitFindings {
		t.Fatalf("exit=%d out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "LLM advisory unavailable") {
		t.Fatalf("missing fallback note: %s", out.String())
	}
}

func TestExplainRuleTimeoutFallsBack(t *testing.T) {
	svc := Services{Explainer: fakeExplainer{}, Narrator: &fakeNarrator{err: fmt.Errorf("%w: provider timed out", llm.ErrUnavailable)}}
	var out bytes.Buffer
	code, err := ExplainRule(context.Background(), svc, ExplainRequest{
		RuleID: "FND-CFG-001", Format: "console", LLMExplain: true, LLMAudience: llm.AudienceDeveloper, LLMTimeout: time.Second,
	}, &out)
	if err != nil || code != ExitOK || !strings.Contains(out.String(), "LLM advisory unavailable") {
		t.Fatalf("code=%d err=%v out=%s", code, err, out.String())
	}
}

func TestExplainRulePropagatesOuterCancellation(t *testing.T) {
	svc := Services{Explainer: fakeExplainer{}, Narrator: &fakeNarrator{err: context.Canceled}}
	var out bytes.Buffer
	code, err := ExplainRule(context.Background(), svc, ExplainRequest{
		RuleID: "FND-CFG-001", Format: "console", LLMExplain: true, LLMAudience: llm.AudienceDeveloper, LLMTimeout: time.Second,
	}, &out)
	if err == nil || code != ExitInternal {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestLLMFlagsIgnoredWithoutOptIn(t *testing.T) {
	svc := Services{Explainer: fakeExplainer{}}
	var out bytes.Buffer
	code, err := ExplainRule(context.Background(), svc, ExplainRequest{
		RuleID: "FND-CFG-001", Format: "console", LLMAudience: "bogus", LLMTimeout: 99 * time.Second,
	}, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestNarrativeRenderingSanitizesMetadataAndNotes(t *testing.T) {
	rendered := string(appendRenderedNarrative([]byte("base\n"), "console", &llm.Narrative{
		Summary:  "Summary",
		Sections: []llm.Section{{Heading: "Heading", Summary: "Section", RuleIDs: []string{"FND-IDN-001"}}},
		Metadata: llm.Metadata{
			Provider:   "azure\u202etext",
			Model:      "gpt\x00model",
			Deployment: "dep\r\nname",
		},
	}, "bad\u202enote"))
	for _, forbidden := range []string{"\u202e", "\x00", "\r"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("rendered=%q", rendered)
		}
	}
}
