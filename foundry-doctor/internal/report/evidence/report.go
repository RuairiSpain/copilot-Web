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
	Decision    string                      `json:"decision"`
	Limitations []string                    `json:"limitations"`
	Controls    map[string][]assess.Control `json:"controls"`
}

// JSON renders the evidence pack as JSON.
func JSON(w io.Writer, a assess.Assessment) error {
	ctrls := map[string][]assess.Control{}
	for _, state := range []assess.State{assess.StatePass, assess.StateFail, assess.StateWarning, assess.StateUnknown, assess.StateQuestion, assess.StateSkipped} {
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
		Decision:    a.Decision,
		Limitations: a.Limitations,
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
	b.WriteString("> Audit-oriented output. Passed, failed, suppressed, baselined and skipped evidence remains visible. This is not a certification.\n\n")
	fmt.Fprintf(&b, "- Profile: %s\n- Decision: %s\n\n", common.Code(a.Profile), common.EscapeMarkdown(a.Decision))
	for _, state := range []assess.State{assess.StateFail, assess.StateWarning, assess.StateUnknown, assess.StateQuestion, assess.StateSkipped, assess.StatePass} {
		fmt.Fprintf(&b, "## %s\n\n", state)
		wrote := false
		for _, c := range a.Controls {
			if c.State != state {
				continue
			}
			wrote = true
			fmt.Fprintf(&b, "### %s\n\n", common.EscapeMarkdown(c.Title))
			fmt.Fprintf(&b, "- Rule: %s\n- Pillar: %s\n- Mandatory: %t\n", common.Code(c.RuleID), common.EscapeMarkdown(c.Pillar), c.Mandatory)
			for _, f := range c.Findings {
				label := "finding"
				if f.Suppressed != nil {
					label = "suppressed"
				} else if f.Baselined {
					label = "baselined"
				}
				fmt.Fprintf(&b, "- %s: %s\n", label, common.EscapeMarkdown(f.Evidence))
			}
			for _, s := range c.Skipped {
				fmt.Fprintf(&b, "- skipped: %s\n", common.EscapeMarkdown(s.Reason))
			}
			b.WriteString("\n")
		}
		if !wrote {
			b.WriteString("None.\n\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
