package report

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// EscapeMarkdown escapes untrusted text for inline use, including inside
// table cells: markdown punctuation is backslash-escaped, HTML-significant
// characters become entities and line breaks collapse to spaces.
func EscapeMarkdown(s string) string {
	s = cleanText(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '\\', '`', '*', '_', '{', '}', '[', ']', '(', ')', '#', '+', '-', '.', '!', '|', '~', '$', '"', '\'', ':', '=', '@', '^', '%':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// codeSpan renders s as an inline code span whose fence is longer than any
// backtick run inside s, so s cannot terminate the span. Pipes are replaced
// so the span is safe in table cells.
func codeSpan(s string) string {
	s = strings.ReplaceAll(cleanText(s), "|", "\u2223")
	s = strings.ReplaceAll(s, "<", "\u2039")
	s = strings.ReplaceAll(s, ">", "\u203a")
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// safeLink renders a docs URL: only plain https URLs become links.
func safeLink(raw string) string {
	raw = cleanText(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		strings.ContainsAny(raw, " <>()[]\\\"'`|") {
		return codeSpan(raw)
	}
	return "<" + u.String() + ">"
}

// Markdown writes a Markdown report. All untrusted text is escaped.
func Markdown(w io.Writer, fs []sdk.Finding, run Run) error {
	p := prepared(fs)
	skips := preparedSkips(run.Skipped)
	c := count(p)
	var b strings.Builder
	b.WriteString("# Foundry Doctor report\n\n")
	fmt.Fprintf(&b, "- Tool version: %s\n- Profile: %s\n- Exit code: %d\n", codeSpan(run.ToolVersion), codeSpan(run.Profile), run.ExitCode)
	if run.GeneratedAt != "" {
		fmt.Fprintf(&b, "- Generated: %s\n", codeSpan(run.GeneratedAt))
	}
	fmt.Fprintf(&b, "\n## Summary\n\n| Errors | Warnings | Info | Suppressed | Baselined | Skipped |\n|---|---|---|---|---|---|\n| %d | %d | %d | %d | %d | %d |\n",
		c.Error, c.Warning, c.Info, c.Suppressed, c.Baselined, len(skips))

	b.WriteString("\n## Findings\n\n")
	n := 0
	for _, f := range p {
		if !active(f) {
			continue
		}
		if n == 0 {
			b.WriteString("| Severity | Rule | Location | Resource | Evidence | Recommendation | Docs |\n|---|---|---|---|---|---|---|\n")
		}
		n++
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
			EscapeMarkdown(string(f.Severity)), codeSpan(f.RuleID), codeSpan(locString(f.Location)),
			EscapeMarkdown(strings.TrimSpace(f.Resource.Type+" "+f.Resource.Name)),
			codeSpan(f.Evidence), EscapeMarkdown(f.Recommendation), safeLink(f.DocsURL))
	}
	if n == 0 {
		b.WriteString("No active findings.\n")
	}

	b.WriteString("\n## Skipped checks\n\nSkipped is not passed.\n\n")
	if len(skips) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Rule | Required | Reason |\n|---|---|---|\n")
		for _, s := range skips {
			req := "no"
			if s.Required {
				req = "yes"
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", codeSpan(s.RuleID), req, EscapeMarkdown(s.Reason))
		}
	}
	if run.Readiness != nil {
		run.Readiness.markdown(&b)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
