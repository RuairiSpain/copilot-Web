package report

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// UncertainPrefix marks a skipped check whose outcome could not be decided
// either way (for example a what-if result that cannot prove a deployment
// will succeed). It must match the prefix used by the preflight rules.
const UncertainPrefix = "uncertain: "

// Readiness is the deployment readiness summary. It never claims that a
// deployment is guaranteed to succeed: Ready only means "no preflight check
// found a problem".
type Readiness struct {
	Ready     []string `json:"ready"`
	Blocked   []string `json:"blocked"`
	Uncertain []string `json:"uncertain"`
	Skipped   []string `json:"skipped"`
}

// BuildReadiness classifies every evaluated rule ID from the raw, unfiltered
// rule outcomes. A rule with any finding (any severity, even if later
// baselined or suppressed) is blocked; an "uncertain: " skip is uncertain; any
// other skip is skipped; the remainder are ready. Skipped is never ready.
func BuildReadiness(ruleIDs []string, fs []sdk.Finding, skips []sdk.Skip) *Readiness {
	blocked := map[string]bool{}
	for _, f := range fs {
		blocked[f.RuleID] = true
	}
	unc := map[string]bool{}
	skp := map[string]bool{}
	for _, s := range skips {
		if strings.HasPrefix(s.Reason, UncertainPrefix) {
			unc[s.RuleID] = true
		} else {
			skp[s.RuleID] = true
		}
	}
	r := &Readiness{Ready: []string{}, Blocked: []string{}, Uncertain: []string{}, Skipped: []string{}}
	for _, id := range ruleIDs {
		switch {
		case blocked[id]:
			r.Blocked = append(r.Blocked, id)
		case skp[id]:
			r.Skipped = append(r.Skipped, id)
		case unc[id]:
			r.Uncertain = append(r.Uncertain, id)
		default:
			r.Ready = append(r.Ready, id)
		}
	}
	for _, s := range [][]string{r.Ready, r.Blocked, r.Uncertain, r.Skipped} {
		slices.Sort(s)
	}
	return r
}

const readinessCaveat = "Readiness is advisory; it does not guarantee that a deployment will succeed."

func (r *Readiness) line(label string, ids []string) string {
	return fmt.Sprintf("%s (%d): %s", label, len(ids), strings.Join(ids, ", "))
}

func (r *Readiness) console(b *strings.Builder) {
	b.WriteString("Deployment readiness:\n")
	for _, l := range []struct {
		n   string
		ids []string
	}{{"ready", r.Ready}, {"blocked", r.Blocked}, {"uncertain", r.Uncertain}, {"skipped", r.Skipped}} {
		b.WriteString("  " + cleanText(r.line(l.n, l.ids)) + "\n")
	}
	b.WriteString("  " + readinessCaveat + "\n\n")
}

func (r *Readiness) markdown(b *strings.Builder) {
	b.WriteString("\n## Deployment readiness\n\n| Status | Count | Rules |\n|---|---|---|\n")
	for _, l := range []struct {
		n   string
		ids []string
	}{{"ready", r.Ready}, {"blocked", r.Blocked}, {"uncertain", r.Uncertain}, {"skipped", r.Skipped}} {
		cells := make([]string, len(l.ids))
		for i, id := range l.ids {
			cells[i] = codeSpan(id)
		}
		fmt.Fprintf(b, "| %s | %d | %s |\n", l.n, len(l.ids), strings.Join(cells, " "))
	}
	b.WriteString("\n" + readinessCaveat + "\n")
}

func readinessProps(r *Readiness) map[string]any {
	if r == nil {
		return nil
	}
	return map[string]any{"readiness": map[string]any{"ready": r.Ready, "blocked": r.Blocked, "uncertain": r.Uncertain, "skipped": r.Skipped, "caveat": readinessCaveat}}
}
