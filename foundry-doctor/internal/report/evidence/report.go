// Package evidence renders the auditable evidence pack.
package evidence

import (
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/common"
)

type pack struct {
	Framework   string                      `json:"framework"`
	Profile     string                      `json:"profile"`
	ToolVersion string                      `json:"toolVersion,omitempty"`
	GeneratedAt string                      `json:"generatedAt,omitempty"`
	ScopeNote   string                      `json:"scopeNote,omitempty"`
	Decision    string                      `json:"decision"`
	Limitations []string                    `json:"limitations"`
	Summary     assess.Summary              `json:"summary"`
	Unassessed  []assess.ControlRef         `json:"unassessed"`
	Costly      []assess.Action             `json:"costlyRecommendations"`
	Controls    map[string][]assess.Control `json:"controls"`
}

// JSON renders the evidence pack as JSON.
func JSON(w io.Writer, a assess.Assessment) error {
	ctrls := map[string][]assess.Control{}
	for _, state := range assess.States {
		key := strings.ToLower(string(state))
		for _, c := range a.Controls {
			if c.State == state {
				ctrls[key] = append(ctrls[key], c)
			}
		}
	}
	data, err := common.JSON(pack{
		Framework:   a.Framework,
		Profile:     a.Profile,
		ToolVersion: a.ToolVersion,
		GeneratedAt: a.GeneratedAt,
		ScopeNote:   a.ScopeNote,
		Decision:    a.Decision,
		Limitations: a.Limitations,
		Summary:     a.Summary,
		Unassessed:  a.Unassessed,
		Costly:      a.Costly,
		Controls:    ctrls,
	})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// Markdown renders the evidence pack as Markdown.
func Markdown(w io.Writer, a assess.Assessment) error {
	var b strings.Builder
	b.WriteString("# Foundry Doctor WAF evidence pack\n\n")
	b.WriteString("> Audit-oriented output. Passed, failed, suppressed, baselined, attested, not-applicable and skipped evidence remains visible. This is not a certification.\n\n")
	fmt.Fprintf(&b, "- Profile: %s\n- Decision: %s\n", common.Code(a.Profile), common.EscapeMarkdown(a.Decision))
	if a.ToolVersion != "" {
		fmt.Fprintf(&b, "- Tool version: %s\n", common.Code(a.ToolVersion))
	}
	if a.GeneratedAt != "" {
		fmt.Fprintf(&b, "- Generated: %s\n", common.Code(a.GeneratedAt))
	}
	if a.ScopeNote != "" {
		fmt.Fprintf(&b, "- Assessed scope: %s\n", common.EscapeMarkdown(a.ScopeNote))
	}
	b.WriteString("\n## Summary\n\n| State | Count |\n|---|---|\n")
	for _, state := range assess.States {
		fmt.Fprintf(&b, "| %s | %d |\n", state, a.Summary.States[state])
	}
	b.WriteString("\n")
	for _, state := range assess.States {
		fmt.Fprintf(&b, "## %s\n\n", state)
		wrote := false
		for _, c := range a.Controls {
			if c.State != state {
				continue
			}
			wrote = true
			writeControl(&b, c)
		}
		if !wrote {
			b.WriteString("None.\n\n")
		}
	}
	b.WriteString("## Costly recommendations (optional)\n\n")
	if len(a.Costly) == 0 {
		b.WriteString("None outstanding.\n\n")
	} else {
		for _, x := range a.Costly {
			fmt.Fprintf(&b, "- %s %s (%s): %s (%s", common.Code(x.RuleID), common.EscapeMarkdown(x.Title), x.State,
				common.EscapeMarkdown(x.CostBasis), common.SafeLink(x.CostSourceURL))
			if x.CostVerified != "" {
				fmt.Fprintf(&b, ", verified %s", common.Code(x.CostVerified))
			}
			b.WriteString(")\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## Limitations\n\n")
	for _, l := range a.Limitations {
		fmt.Fprintf(&b, "- %s\n", common.EscapeMarkdown(l))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeControl(b *strings.Builder, c assess.Control) {
	fmt.Fprintf(b, "### %s\n\n", common.EscapeMarkdown(c.Title))
	fmt.Fprintf(b, "- Rule: %s", common.Code(c.RuleID))
	if c.RuleVersion > 0 {
		fmt.Fprintf(b, " (version %d)", c.RuleVersion)
	}
	fmt.Fprintf(b, "\n- Pillar: %s\n- Mandatory: %t\n", common.EscapeMarkdown(assess.PillarName(c.Pillar)), c.Mandatory)
	if c.Basis != "" {
		fmt.Fprintf(b, "- Evidence basis: %s\n", c.Basis)
	}
	if c.StateReason != "" {
		fmt.Fprintf(b, "- Why this state: %s\n", common.EscapeMarkdown(c.StateReason))
	}
	if c.SourceURL != "" || c.ProjectOpinion {
		fmt.Fprintf(b, "- %s\n", common.Provenance(c.SourceURL, c.LastVerified, c.ProjectOpinion))
	}
	if c.QuestionAnswer != "" {
		fmt.Fprintf(b, "- Recorded answer (team attestation, not verified): %s\n", common.Code(c.QuestionAnswer))
	}
	for _, f := range c.Findings {
		label := "finding"
		switch {
		case f.Suppressed != nil:
			label = "suppressed"
		case f.Baselined:
			label = "baselined"
		}
		fmt.Fprintf(b, "- %s: %s", label, common.EscapeMarkdown(f.Evidence))
		if f.Severity != "" {
			fmt.Fprintf(b, " (severity %s)", f.Severity)
		}
		if f.Fingerprint != "" {
			fmt.Fprintf(b, " fingerprint %s", common.Code(f.Fingerprint))
		}
		b.WriteString("\n")
		if s := f.Suppressed; s != nil {
			fmt.Fprintf(b, "  - suppression reason: %s; owner: %s", common.EscapeMarkdown(s.Reason), common.EscapeMarkdown(s.Owner))
			if s.Expires != "" {
				fmt.Fprintf(b, "; expires: %s", common.Code(s.Expires))
			}
			b.WriteString("\n")
		}
	}
	for _, s := range c.Skipped {
		fmt.Fprintf(b, "- skipped: %s", common.EscapeMarkdown(s.Reason))
		if s.MissingCapability != "" {
			fmt.Fprintf(b, " (missing: %s)", common.EscapeMarkdown(s.MissingCapability))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
