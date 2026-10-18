// Package developer renders the developer-focused WAF detail report.
package developer

import (
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/common"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Markdown renders detailed developer remediation guidance.
func Markdown(w io.Writer, a assess.Assessment) error {
	var b strings.Builder
	b.WriteString("# Foundry Doctor WAF developer detail\n\n")
	b.WriteString("> Detailed control evidence for remediation. This report is scoped to available evidence and does not claim complete WAF compliance.\n\n")
	fmt.Fprintf(&b, "- Profile: %s\n- Decision: %s\n", common.Code(a.Profile), common.EscapeMarkdown(a.Decision))
	if a.ScopeNote != "" {
		fmt.Fprintf(&b, "- Assessed scope: %s\n", common.EscapeMarkdown(a.ScopeNote))
	}
	b.WriteString("\n")
	for _, pillar := range assess.Pillars {
		wrote := false
		for _, c := range a.Controls {
			if c.Pillar != pillar {
				continue
			}
			if !wrote {
				fmt.Fprintf(&b, "## %s\n\n", assess.PillarName(pillar))
				wrote = true
			}
			writeControl(&b, c)
		}
	}
	b.WriteString("## Limitations\n\n")
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "- %s\n", common.EscapeMarkdown(l))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeControl(b *strings.Builder, c assess.Control) {
	fmt.Fprintf(b, "### %s — %s\n\n", common.EscapeMarkdown(c.Title), c.State)
	fmt.Fprintf(b, "- Rule: %s", common.Code(c.RuleID))
	if c.RuleVersion > 0 {
		fmt.Fprintf(b, " (version %d)", c.RuleVersion)
	}
	fmt.Fprintf(b, "\n- Mandatory: %t\n", c.Mandatory)
	if c.Basis != "" {
		fmt.Fprintf(b, "- Evidence basis: %s\n", c.Basis)
	}
	if c.StateReason != "" {
		fmt.Fprintf(b, "- Why this state: %s\n", common.EscapeMarkdown(c.StateReason))
	}
	if c.Costly {
		fmt.Fprintf(b, "- Costly recommendation: %s (%s", common.EscapeMarkdown(c.CostBasis), common.SafeLink(c.CostSourceURL))
		if c.CostVerified != "" {
			fmt.Fprintf(b, ", verified %s", common.Code(c.CostVerified))
		}
		b.WriteString(")\n")
	} else {
		b.WriteString("- Costly recommendation: false\n")
	}
	if c.ProjectOpinion {
		b.WriteString("- Recommendation basis: project opinion\n")
	}
	fmt.Fprintf(b, "- %s\n", common.Provenance(c.SourceURL, c.LastVerified, c.ProjectOpinion))
	if c.QuestionPrompt != "" {
		fmt.Fprintf(b, "- Review question: %s\n", common.EscapeMarkdown(c.QuestionPrompt))
	}
	if c.QuestionAnswer != "" {
		fmt.Fprintf(b, "- Recorded answer (team attestation, not verified): %s\n", common.Code(c.QuestionAnswer))
	}
	if len(c.WARAReferences) != 0 {
		b.WriteString("- WARA references (recommendations, not platform facts):\n")
		for _, r := range c.WARAReferences {
			fmt.Fprintf(b, "  - %s %s (pgVerified: %s)\n", common.Code(r.ID), common.EscapeMarkdown(r.Label), r.PGVerified)
		}
	}
	if len(c.ExternalIDs) != 0 {
		fmt.Fprintf(b, "- Overlapping external checks (not ingested): %s\n", common.Code(strings.Join(c.ExternalIDs, ", ")))
	}
	if c.Recommendation != "" {
		fmt.Fprintf(b, "- Recommendation: %s\n", common.EscapeMarkdown(c.Recommendation))
	}
	if c.Fix != "" {
		b.WriteString("- Fix:\n\n")
		b.WriteString(common.Fenced(c.Fix, "  "))
		b.WriteString("\n")
	}
	if len(c.Findings) == 0 && len(c.Skipped) == 0 {
		b.WriteString("- Evidence: no finding or skip record was present.\n\n")
		return
	}
	b.WriteString("- Evidence:\n")
	for _, f := range c.Findings {
		label := string(f.Severity)
		if label == "" {
			label = "finding"
		}
		if f.Suppressed != nil {
			label += ", suppressed"
		} else if f.Baselined {
			label += ", baselined"
		}
		fmt.Fprintf(b, "  - %s: %s at %s\n", label, common.EscapeMarkdown(f.Evidence), common.Code(loc(f.Location)))
	}
	for _, s := range c.Skipped {
		fmt.Fprintf(b, "  - skipped: %s\n", common.EscapeMarkdown(s.Reason))
	}
	b.WriteString("\n")
}

func loc(l sdk.Location) string {
	if l.File == "" {
		return "unknown"
	}
	if l.Line == 0 {
		return l.File
	}
	if l.Column == 0 {
		return fmt.Sprintf("%s:%d", l.File, l.Line)
	}
	return fmt.Sprintf("%s:%d:%d", l.File, l.Line, l.Column)
}
