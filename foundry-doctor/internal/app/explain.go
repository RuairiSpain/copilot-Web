package app

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
)

// catalogExplainer renders catalogue rules.
type catalogExplainer struct{ rules []catalog.Rule }

// NewExplainer returns an Explainer over catalogue rules.
func NewExplainer(rules []catalog.Rule) Explainer { return catalogExplainer{rules: rules} }

// Explain implements Explainer. Unknown IDs are a usage error.
func (e catalogExplainer) Explain(_ context.Context, ruleID, format string, w io.Writer) error {
	i := slices.IndexFunc(e.rules, func(r catalog.Rule) bool { return strings.EqualFold(r.ID, ruleID) })
	if i < 0 {
		return Usagef("unknown rule %q; run `foundry-doctor doctor --help` for selectors", findings.Redact(ruleID))
	}
	r := e.rules[i]
	h := "%s\n"
	sec := func(title string) string { return title + ":" }
	bullet := "  - "
	if format == "markdown" {
		h = "# %s\n"
		sec = func(title string) string { return "**" + title + "**" }
		bullet = "- "
	}
	var b strings.Builder
	fmt.Fprintf(&b, h, mdEsc(r.ID+" "+r.Title, format))
	fmt.Fprintf(&b, "\n%s %s (version %d, %s)\n", sec("Status"), r.Status, r.Version, r.Category)
	fmt.Fprintf(&b, "%s %s\n", sec("Pillar"), r.Pillar)
	fmt.Fprintf(&b, "%s %s\n", sec("Basis"), strings.Join(r.Basis, ", "))
	fmt.Fprintf(&b, "%s dev=%s test=%s prod=%s\n", sec("Severity"), r.Severity.Dev, r.Severity.Test, r.Severity.Prod)
	if !r.Compatibility.Azd.Empty() {
		fmt.Fprintf(&b, "%s azd %s\n", sec("Compatibility"), r.Compatibility.Azd.String())
	}
	for _, s := range []struct{ t, v string }{
		{"What it checks", r.Description}, {"Evidence", r.Evidence.Description},
		{"Recommendation", r.Recommendation}, {"Fix", r.Fix}, {"Notes", r.Notes},
	} {
		if strings.TrimSpace(s.v) != "" {
			fmt.Fprintf(&b, "\n%s\n%s\n", sec(s.t), mdEsc(strings.TrimSpace(s.v), format))
		}
	}
	if len(r.Sources) > 0 {
		fmt.Fprintf(&b, "\n%s\n", sec("Sources"))
		for _, s := range r.Sources {
			fmt.Fprintf(&b, "%s%s (verified %s)\n", bullet, s.URL, s.LastVerified)
		}
	}
	_, err := io.WriteString(w, findings.Redact(b.String()))
	return err
}

// mdEsc escapes Markdown control characters in markdown output only.
func mdEsc(s, format string) string {
	if format != "markdown" {
		return s
	}
	return strings.NewReplacer("|", `\|`, "<", "&lt;", ">", "&gt;", "`", "\\`").Replace(s)
}
