// Package common holds helpers for the assessment report renderers.
package common

import (
	"encoding/json"
	"net/url"
	"strings"

	base "github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
)

// EscapeMarkdown escapes untrusted text for markdown contexts.
func EscapeMarkdown(s string) string { return base.EscapeMarkdown(s) }

// SafeLink renders a trusted-looking https URL as a markdown link and falls
// back to escaped text for anything else.
func SafeLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		strings.ContainsAny(raw, " <>()[]\\\"'`|") {
		return Code(raw)
	}
	return "<" + u.String() + ">"
}

// Code renders an inline code span.
func Code(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "|", "\u2223")
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
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// Fenced renders s as a fenced code block whose lines are prefixed with indent,
// so multi-line YAML or Bicep keeps its newlines inside a list item.
func Fenced(s, indent string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", max(3, longest+1))
	var b strings.Builder
	b.WriteString(indent + fence + "\n")
	for _, line := range strings.Split(s, "\n") {
		b.WriteString(strings.TrimRight(indent+line, " ") + "\n")
	}
	b.WriteString(indent + fence + "\n")
	return b.String()
}

// Provenance renders where a recommendation comes from: the first catalogue
// source with its verification date, labelled as project opinion when the
// rule is product opinion.
func Provenance(sourceURL, lastVerified string, opinion bool) string {
	src := ""
	if sourceURL != "" {
		src = SafeLink(sourceURL)
		if lastVerified != "" {
			src += " (verified " + Code(lastVerified) + ")"
		}
	}
	switch {
	case opinion && src != "":
		return "Project opinion, not Microsoft guidance; context source: " + src
	case opinion:
		return "Project opinion, not Microsoft guidance; no source recorded"
	case src != "":
		return "Source: " + src
	default:
		return "No source recorded; treat as project opinion"
	}
}

// JSON returns indented deterministic JSON.
func JSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
