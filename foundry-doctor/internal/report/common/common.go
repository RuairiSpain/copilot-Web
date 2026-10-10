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

// JSON returns indented deterministic JSON.
func JSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
