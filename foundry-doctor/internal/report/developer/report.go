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
	fmt.Fprintf(&b, "- Profile: %s\n- Decision: %s\n\n", common.Code(a.Profile), common.EscapeMarkdown(a.Decision))
	for _, pillar := range []string{"security", "reliability", "operations", "cost", "performance"} {
		wrote := false
		for _, c := range a.Controls {
			if c.Pillar != pillar {
				continue
			}
			if !wrote {
				fmt.Fprintf(&b, "## %s\n\n", title(pillar))
				wrote = true
			}
			fmt.Fprintf(&b, "### %s — %s\n\n", common.EscapeMarkdown(c.Title), c.State)
			fmt.Fprintf(&b, "- Rule: %s\n- Mandatory: %t\n- Costly recommendation: %t\n", common.Code(c.RuleID), c.Mandatory, c.Costly)
			if c.ProjectOpinion {
				b.WriteString("- Recommendation basis: project opinion\n")
			}
			if c.SourceURL != "" {
				fmt.Fprintf(&b, "- Source: %s", common.SafeLink(c.SourceURL))
				if c.LastVerified != "" {
					fmt.Fprintf(&b, " (%s)", common.Code(c.LastVerified))
				}
				b.WriteString("\n")
			}
			if c.QuestionPrompt != "" {
				fmt.Fprintf(&b, "- Review question: %s\n", common.EscapeMarkdown(c.QuestionPrompt))
			}
			if c.QuestionAnswer != "" {
				fmt.Fprintf(&b, "- Recorded answer: %s\n", common.Code(c.QuestionAnswer))
			}
			if len(c.WARAReferences) != 0 {
				fmt.Fprintf(&b, "- WARA references: %s\n", common.Code(strings.Join(c.WARAReferences, ", ")))
			}
			if c.Recommendation != "" {
				fmt.Fprintf(&b, "- Recommendation: %s\n", common.EscapeMarkdown(c.Recommendation))
			}
			if c.Fix != "" {
				fmt.Fprintf(&b, "- Fix: %s\n", common.Code(c.Fix))
			}
			if len(c.Findings) == 0 && len(c.Skipped) == 0 {
				b.WriteString("- Evidence: no finding or skip record was present.\n\n")
				continue
			}
			b.WriteString("- Evidence:\n")
			for _, f := range c.Findings {
				fmt.Fprintf(&b, "  - %s at %s\n", common.EscapeMarkdown(f.Evidence), common.Code(loc(f.Location)))
			}
			for _, s := range c.Skipped {
				fmt.Fprintf(&b, "  - skipped: %s\n", common.EscapeMarkdown(s.Reason))
			}
			b.WriteString("\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func title(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
