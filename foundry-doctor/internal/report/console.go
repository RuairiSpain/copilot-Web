package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func locString(l sdk.Location) string {
	if l.File == "" {
		return "(no file)"
	}
	s := l.File
	if l.Line > 0 {
		s += fmt.Sprintf(":%d", l.Line)
		if l.Column > 0 {
			s += fmt.Sprintf(":%d", l.Column)
		}
	}
	return s
}

// Console writes a plain-text report without colour or terminal sequences.
func Console(w io.Writer, fs []sdk.Finding, run Run) error {
	p := prepared(fs)
	skips := preparedSkips(run.Skipped)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s profile=%s\n", ToolName, cleanText(run.ToolVersion), cleanText(run.Profile))
	if run.GeneratedAt != "" {
		fmt.Fprintf(&b, "generated: %s\n", cleanText(run.GeneratedAt))
	}
	b.WriteString("\n")
	c := count(p)
	for _, f := range p {
		if !active(f) {
			continue
		}
		fmt.Fprintf(&b, "%s %s %s\n", strings.ToUpper(cleanText(string(f.Severity))), cleanText(f.RuleID), cleanText(locString(f.Location)))
		if f.Resource.Type != "" || f.Resource.Name != "" {
			fmt.Fprintf(&b, "  resource: %s\n", cleanText(f.Resource.Type+" "+f.Resource.Name))
		}
		if f.Evidence != "" {
			fmt.Fprintf(&b, "  evidence: %s\n", cleanText(f.Evidence))
		}
		if f.Recommendation != "" {
			fmt.Fprintf(&b, "  recommendation: %s\n", cleanText(f.Recommendation))
		}
		if f.Fix != "" {
			fmt.Fprintf(&b, "  fix: %s\n", cleanText(f.Fix))
		}
		if f.DocsURL != "" {
			fmt.Fprintf(&b, "  docs: %s\n", cleanText(f.DocsURL))
		}
		fmt.Fprintf(&b, "  fingerprint: %s\n\n", cleanText(f.Fingerprint))
	}
	if len(skips) > 0 {
		b.WriteString("Skipped checks (skipped is not passed):\n")
		for _, s := range skips {
			req := ""
			if s.Required {
				req = " [required]"
			}
			fmt.Fprintf(&b, "  %s%s: %s\n", cleanText(s.RuleID), req, cleanText(s.Reason))
		}
		b.WriteString("\n")
	}
	if run.Readiness != nil {
		run.Readiness.console(&b)
	}
	fmt.Fprintf(&b, "Summary: %d error, %d warning, %d info; %d suppressed, %d baselined; %d skipped; exit code %d\n",
		c.Error, c.Warning, c.Info, c.Suppressed, c.Baselined, len(skips), run.ExitCode)
	_, err := io.WriteString(w, b.String())
	return err
}
