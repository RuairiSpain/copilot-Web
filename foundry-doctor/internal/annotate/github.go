package annotate

import (
	"fmt"
	"io"
	"strings"
)

// GitHubWorkflowCommands writes GitHub Actions workflow command annotations for
// exact source locations only.
func GitHubWorkflowCommands(w io.Writer, entries []Entry) error {
	for _, e := range entries {
		if !e.Inline {
			continue
		}
		level := "notice"
		switch e.Severity {
		case "error":
			level = "error"
		case "warning":
			level = "warning"
		}
		title := e.RuleID
		if title == "" {
			title = "Foundry Doctor"
		}
		msg := e.Message
		if e.Recommendation != "" {
			msg += " — " + e.Recommendation
		}
		if _, err := fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s::%s\n",
			level, escapeCommandProperty(e.File), e.Line, max(1, e.Column), escapeCommandProperty(singleLine(title)), escapeCommandData(singleLine(msg))); err != nil {
			return err
		}
	}
	return nil
}

// escapeCommandData and escapeCommandProperty mirror escapeData and
// escapeProperty in actions/toolkit packages/core/src/command.ts. They are
// applied once, here, at the GitHub sink only; Entry text stays plain.
func escapeCommandData(s string) string {
	repl := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return repl.Replace(s)
}

func escapeCommandProperty(s string) string {
	repl := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	return repl.Replace(s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ReviewComment returns the human-facing inline review comment text.
func (e Entry) ReviewComment() string {
	label := strings.TrimSpace(strings.Join([]string{singleLine(e.RuleID), singleLine(string(e.Severity)), singleLine(e.Category)}, " "))
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Foundry Doctor"
	}
	msg := singleLine(e.Message)
	if e.Recommendation != "" {
		msg += " Recommendation: " + singleLine(e.Recommendation)
	}
	return singleLine(fmt.Sprintf("%s (ref %s). %s", label, singleLine(e.ID), strings.TrimSpace(msg)))
}

func singleLine(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if isFormatRune(r) {
			continue
		}
		if isBreakRune(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	return strings.TrimSpace(b.String())
}
