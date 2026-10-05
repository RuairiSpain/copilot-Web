// Package markdown renders a report as GitHub-flavoured Markdown. Every value that came from the
// project, Azure or an adapter is untrusted: it is stripped of control characters and escaped so
// that it cannot add markup, HTML, links, mentions or table columns.
package markdown

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/norm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Reporter is the Markdown sdk.Reporter.
type Reporter struct{}

var _ sdk.Reporter = Reporter{}

// New returns a Markdown reporter.
func New() Reporter { return Reporter{} }

// Format returns "markdown".
func (Reporter) Format() string { return "markdown" }

var escaper = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", "@", "&#64;",
	"\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]",
	"(", "\\(", ")", "\\)", "|", "\\|", "#", "\\#", "!", "\\!", "~", "\\~",
)

// Escape returns untrusted text, sanitised to one line, safe to place in a Markdown paragraph or table cell.
func Escape(s string) string { return escaper.Replace(norm.Text(s, true)) }

// code renders text as an inline code span that cannot be closed from inside.
func code(s string) string {
	s = norm.Text(s, true)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "<", "‹")
	s = strings.ReplaceAll(s, ">", "›")
	s = strings.ReplaceAll(s, "|", "∣")
	return "`" + s + "`"
}

// link returns a Markdown link for an https URL, otherwise escaped text.
func link(label, raw string) string {
	raw = norm.Text(raw, true)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.ContainsAny(raw, " ") {
		return Escape(raw)
	}
	safe := strings.NewReplacer("(", "%28", ")", "%29", "|", "%7C", "<", "%3C", ">", "%3E", "`", "%60", "[", "%5B", "]", "%5D", "\\", "%5C").Replace(u.String())
	return "[" + Escape(label) + "](" + safe + ")"
}

func cell(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return " "
	}
	return strings.Join(out, "<br>")
}

// Write renders r to w.
func (Reporter) Write(w io.Writer, r *sdk.Report) error {
	rep := norm.Normalised(r)
	var b strings.Builder
	name := norm.Text(rep.Tool.Name, true)
	if name == "" {
		name = "foundry-doctor"
	}
	fmt.Fprintf(&b, "# %s report\n\n", Escape(name))
	fmt.Fprintf(&b, "- Tool: %s %s\n- Profile: %s\n\n", Escape(name), Escape(rep.Tool.Version), code(rep.Profile))

	b.WriteString("## Effective policy\n\n")
	lines := norm.PolicyLines(rep.EffectivePolicy)
	if len(lines) == 0 {
		b.WriteString("None.\n\n")
	} else {
		b.WriteString("| Key | Value |\n| --- | --- |\n")
		for _, kv := range lines {
			fmt.Fprintf(&b, "| %s | %s |\n", cell(code(kv.Key)), cell(Escape(kv.Value)))
		}
		b.WriteByte('\n')
	}

	if len(rep.Tools) > 0 {
		b.WriteString("## Tools\n\n| Tool | Version | State | Required | Detail |\n| --- | --- | --- | --- | --- |\n")
		for _, t := range rep.Tools {
			fmt.Fprintf(&b, "| %s | %s | %s | %t | %s |\n", cell(Escape(t.Name)), cell(Escape(t.Version)), cell(Escape(string(t.State))), t.Required, cell(Escape(t.Detail)))
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "## Findings (%d)\n\n", len(rep.Findings))
	if len(rep.Findings) == 0 {
		b.WriteString("None.\n\n")
	}
	for _, g := range norm.GroupByFile(rep.Findings) {
		fmt.Fprintf(&b, "### %s\n\n", cell(code(g.File)))
		b.WriteString("| Severity | Rule | Position | Resource | Details | Status |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, f := range g.Findings {
			docs := ""
			if f.DocsURL != "" {
				docs = link("docs", f.DocsURL)
			}
			details := cell(
				"Evidence: "+Escape(f.Evidence),
				optional("Recommendation: ", f.Recommendation),
				optional("Fix: ", f.Fix),
				docs,
			)
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
				cell(Escape(string(f.Severity))), cell(code(f.RuleID)), cell(Escape(norm.Position(f.Location))),
				cell(Escape(norm.ResourceLabel(f.Resource))), details, cell(status(f)))
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "## Skipped checks (%d)\n\n", len(rep.Skipped))
	if len(rep.Skipped) == 0 {
		b.WriteString("None.\n\n")
	} else {
		b.WriteString("| Rule | Reason | Missing capability | Detail |\n| --- | --- | --- | --- |\n")
		for _, s := range rep.Skipped {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", cell(code(s.RuleID)), cell(Escape(s.Reason)), cell(Escape(s.MissingCapability)), cell(Escape(s.Detail)))
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Owner summary\n\n")
	s := rep.Summary
	b.WriteString("| Errors | Warnings | Info | Suppressed | Baselined | Skipped | Hidden |\n| --- | --- | --- | --- | --- | --- | --- |\n")
	fmt.Fprintf(&b, "| %d | %d | %d | %d | %d | %d | %d |\n\n", s.Error, s.Warning, s.Info, s.Suppressed, s.Baselined, norm.SkippedCount(&rep), s.Hidden)
	b.WriteString("Top rules:\n\n")
	top := norm.TopRules(rep.Findings, 5)
	if len(top) == 0 {
		b.WriteString("- none\n")
	}
	for _, t := range top {
		fmt.Fprintf(&b, "- %s: %d (%s)\n", code(t.RuleID), t.Count, Escape(string(t.Severity)))
	}
	if mk := norm.MissingProfileKeys(rep.Skipped); len(mk) > 0 {
		b.WriteString("\nRules skipped because a profile key is missing:\n\n")
		for _, m := range mk {
			rules := make([]string, len(m.Rules))
			for i, id := range m.Rules {
				rules[i] = code(id)
			}
			fmt.Fprintf(&b, "- %s: %s\n", code(norm.ProfileKeyMissingPrefix+m.Key), strings.Join(rules, ", "))
		}
	}
	fmt.Fprintf(&b, "\nExit code: %d\n\n", rep.ExitCode)
	fmt.Fprintf(&b, "**%s**\n", Escape(norm.SkippedStatement))

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write markdown report: %w", err)
	}
	return nil
}

func optional(label, v string) string {
	if e := Escape(v); e != "" {
		return label + e
	}
	return ""
}

func status(f sdk.Finding) string {
	var p []string
	if f.Suppressed != nil {
		p = append(p, "suppressed until "+Escape(f.Suppressed.Expires))
		if f.Suppressed.Reason != "" {
			p = append(p, "reason: "+Escape(f.Suppressed.Reason))
		}
	}
	if f.Baselined {
		p = append(p, "baselined")
	}
	if f.Confidence != "" {
		p = append(p, Escape(string(f.Confidence)))
	}
	return strings.Join(p, "<br>")
}
