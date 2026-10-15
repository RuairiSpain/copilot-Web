package redact

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	maxExplanationBytes    = 320
	maxEvidenceBytes       = 320
	maxRecommendationBytes = 240
	maxAssessmentFindings  = 10
)

// Rule converts one catalogue rule into the allow-listed model payload.
func Rule(rule catalog.Rule, audience llm.Audience) llm.RuleInput {
	return llm.RuleInput{
		Audience: audience,
		Finding: llm.Finding{
			RuleID:         strings.TrimSpace(rule.ID),
			Explanation:    truncate(clean(strings.TrimSpace(rule.Description)), maxExplanationBytes),
			Evidence:       truncate(clean(strings.TrimSpace(rule.Evidence.Description)), maxEvidenceBytes),
			Recommendation: truncate(clean(strings.TrimSpace(rule.Recommendation)), maxRecommendationBytes),
		},
	}
}

// Assessment converts a deterministic WAF assessment into the allow-listed
// narrative payload.
func Assessment(rules []catalog.Rule, a assess.Assessment, audience llm.Audience) (llm.AssessmentInput, error) {
	rulesByID := make(map[string]catalog.Rule, len(rules))
	for _, r := range rules {
		rulesByID[r.ID] = r
	}
	controls := append([]assess.Control(nil), a.Controls...)
	slices.SortFunc(controls, func(x, y assess.Control) int {
		rx, ry := rank(x.State), rank(y.State)
		if rx != ry {
			return rx - ry
		}
		return strings.Compare(x.RuleID, y.RuleID)
	})

	projected := make([]llm.Finding, 0, len(controls))
	seen := map[string]bool{}
	for _, c := range controls {
		if c.State == assess.StatePass || seen[c.RuleID] {
			continue
		}
		r, ok := rulesByID[c.RuleID]
		if !ok {
			continue
		}
		projected = append(projected, llm.Finding{
			RuleID:         c.RuleID,
			Explanation:    truncate(clean(strings.TrimSpace(r.Description)), maxExplanationBytes),
			Evidence:       truncate(clean(controlEvidence(c)), maxEvidenceBytes),
			Recommendation: truncate(clean(firstNonEmpty(c.Recommendation, r.Recommendation)), maxRecommendationBytes),
		})
		seen[c.RuleID] = true
		if len(projected) == maxAssessmentFindings {
			break
		}
	}
	if len(projected) == 0 {
		return llm.AssessmentInput{}, fmt.Errorf("%w: no non-pass findings available for advisory narrative", llm.ErrUnavailable)
	}
	return llm.AssessmentInput{
		Audience: audience,
		Findings: projected,
	}, nil
}

func controlEvidence(c assess.Control) string {
	parts := make([]string, 0, len(c.Findings)+len(c.Skipped)+1)
	for _, f := range c.Findings {
		rf := findings.RedactFinding(f)
		if loc := location(rf.Location); loc != "" {
			parts = append(parts, fmt.Sprintf("deterministic finding recorded at %s", loc))
		} else {
			parts = append(parts, "deterministic finding recorded")
		}
	}
	for _, s := range c.Skipped {
		reason := clean(strings.TrimSpace(findings.Redact(s.Reason)))
		if reason != "" {
			parts = append(parts, "skipped: "+reason)
		}
	}
	if len(parts) == 0 {
		return "No deterministic evidence summary was available."
	}
	return strings.Join(parts, "; ")
}

func location(loc sdk.Location) string {
	file := report.SanitizePath(loc.File)
	switch {
	case file == "":
		return ""
	case loc.Line <= 0:
		return file
	case loc.Column <= 0:
		return fmt.Sprintf("%s:%d", file, loc.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", file, loc.Line, loc.Column)
	}
}

func clean(s string) string {
	s = findings.Redact(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return strings.TrimSpace(s[:max-3]) + "..."
}

func rank(state assess.State) int {
	switch state {
	case assess.StateFail:
		return 0
	case assess.StateWarning:
		return 1
	case assess.StateQuestion:
		return 2
	case assess.StateUnknown:
		return 3
	case assess.StateSkipped:
		return 4
	default:
		return 5
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
