// Package owner renders the owner-facing WAF summary.
package owner

import (
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/common"
)

// Markdown renders the owner summary as Markdown.
func Markdown(w io.Writer, a assess.Assessment) error {
	var b strings.Builder
	b.WriteString("# Foundry Doctor WAF owner summary\n\n")
	b.WriteString("> Scope-limited architecture summary. This is not a certification or a complete Well-Architected review.\n\n")
	fmt.Fprintf(&b, "- Profile: %s\n- Decision: %s\n", common.Code(a.Profile), common.EscapeMarkdown(a.Decision))
	if a.ScopeNote != "" {
		fmt.Fprintf(&b, "- Assessed scope: %s\n", common.EscapeMarkdown(a.ScopeNote))
	}
	b.WriteString("\n## Summary\n\n| State | Count |\n|---|---|\n")
	for _, state := range assess.States {
		fmt.Fprintf(&b, "| %s | %d |\n", state, a.Summary.States[state])
	}
	b.WriteString("\n## Top five actions\n\n")
	if len(a.TopActions) == 0 {
		b.WriteString("No immediate actions in the assessed scope.\n")
	} else {
		for _, x := range a.TopActions {
			kind := "optional"
			if x.Mandatory {
				kind = "mandatory"
			}
			fmt.Fprintf(&b, "- **%s** (%s, %s, %s): %s %s.\n", common.EscapeMarkdown(x.Title), x.State,
				common.EscapeMarkdown(assess.PillarName(x.Pillar)), kind, common.EscapeMarkdown(x.Recommendation),
				common.Provenance(x.SourceURL, x.LastVerified, x.ProjectOpinion))
		}
	}
	b.WriteString("\n## Costly recommendations (optional)\n\n")
	if len(a.Costly) == 0 {
		b.WriteString("None outstanding.\n")
	} else {
		b.WriteString("These optional controls add Azure cost. They are kept out of the top actions; decide them against your requirements.\n\n")
		for _, x := range a.Costly {
			fmt.Fprintf(&b, "- **%s** (%s, %s): %s Cost basis: %s (%s", common.EscapeMarkdown(x.Title), x.State,
				common.EscapeMarkdown(assess.PillarName(x.Pillar)), common.EscapeMarkdown(x.Recommendation),
				common.EscapeMarkdown(x.CostBasis), common.SafeLink(x.CostSourceURL))
			if x.CostVerified != "" {
				fmt.Fprintf(&b, ", verified %s", common.Code(x.CostVerified))
			}
			fmt.Fprintf(&b, "). %s.\n", common.Provenance(x.SourceURL, x.LastVerified, x.ProjectOpinion))
		}
	}
	attested := false
	for _, c := range a.Controls {
		if c.Basis != assess.BasisAttested {
			continue
		}
		if !attested {
			b.WriteString("\n## Team-attested answers\n\nRecorded by the team in Foundry Doctor configuration, including settings delegated to Azure Policy. Foundry Doctor did not verify them. A mandatory control that rests on an attestation keeps the decision at \"Proceed with conditions\".\n\n")
			attested = true
		}
		kind := "optional"
		if c.Mandatory {
			kind = "mandatory"
		}
		answer := common.EscapeMarkdown(c.StateReason)
		if c.QuestionAnswer != "" {
			answer = common.Code(c.QuestionAnswer)
		}
		fmt.Fprintf(&b, "- %s (%s, %s): %s\n", common.EscapeMarkdown(c.Title), c.State, kind, answer)
	}
	b.WriteString("\n## Unassessed scope\n\n")
	if len(a.Unassessed) == 0 {
		b.WriteString("None.\n")
	} else {
		for _, x := range a.Unassessed {
			fmt.Fprintf(&b, "- %s (%s, %s)", common.EscapeMarkdown(x.Title), x.State, common.EscapeMarkdown(assess.PillarName(x.Pillar)))
			if x.StateReason != "" {
				fmt.Fprintf(&b, ": %s", common.EscapeMarkdown(x.StateReason))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Limitations\n\n")
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "- %s\n", common.EscapeMarkdown(l))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
