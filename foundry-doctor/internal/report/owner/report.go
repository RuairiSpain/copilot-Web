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
	for _, state := range []assess.State{assess.StateFail, assess.StateWarning, assess.StateUnknown, assess.StateQuestion, assess.StateSkipped, assess.StatePass} {
		fmt.Fprintf(&b, "| %s | %d |\n", state, a.Summary.States[state])
	}
	b.WriteString("\n## Top five actions\n\n")
	if len(a.TopActions) == 0 {
		b.WriteString("No immediate actions in the assessed scope.\n")
	} else {
		for _, x := range a.TopActions {
			fmt.Fprintf(&b, "- **%s** (%s, %s): %s", common.EscapeMarkdown(x.Title), x.State, common.EscapeMarkdown(x.Pillar), common.EscapeMarkdown(x.Recommendation))
			if x.SourceURL != "" {
				fmt.Fprintf(&b, " Source: %s", common.SafeLink(x.SourceURL))
			}
			if x.LastVerified != "" {
				fmt.Fprintf(&b, " (%s)", common.Code(x.LastVerified))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Unassessed scope\n\n")
	if len(a.Unassessed) == 0 {
		b.WriteString("None.\n")
	} else {
		for _, x := range a.Unassessed {
			fmt.Fprintf(&b, "- %s (%s, %s)\n", common.EscapeMarkdown(x.Title), x.State, common.EscapeMarkdown(x.Pillar))
		}
	}
	b.WriteString("\n## Limitations\n\n")
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "- %s\n", common.EscapeMarkdown(l))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
