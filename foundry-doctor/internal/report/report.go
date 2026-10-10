// Package report renders findings and run metadata as console, JSON,
// Markdown and SARIF 2.1.0. Output is deterministic (sorted, no wall-clock
// time unless injected via Run.GeneratedAt), redacted with the findings
// redaction policy and free of absolute paths.
package report

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/location"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Format names a report format.
type Format string

// Supported formats.
const (
	FormatConsole  Format = "console"
	FormatJSON     Format = "json"
	FormatMarkdown Format = "markdown"
	FormatSARIF    Format = "sarif"
)

// ToolName is the reported tool name.
const ToolName = "foundry-doctor"

// ParseFormat parses a format name ("md" is accepted for markdown).
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "console", "text":
		return FormatConsole, nil
	case "json":
		return FormatJSON, nil
	case "markdown", "md":
		return FormatMarkdown, nil
	case "sarif":
		return FormatSARIF, nil
	}
	return "", fmt.Errorf("unknown report format %q (want console, json, markdown or sarif)", s)
}

// Run is the run metadata accompanying the findings.
type Run struct {
	ToolVersion string
	Profile     string
	// Skipped lists checks that did not run. Skipped is never a pass.
	Skipped []sdk.Skip
	// ExitCode is the classified exit code (see ClassifyExit).
	ExitCode int
	// GeneratedAt is an optional injected timestamp (RFC 3339). Empty means
	// no timestamp is emitted, keeping output byte-for-byte reproducible.
	GeneratedAt string
	// Readiness is the optional deployment readiness summary (preflight).
	Readiness *Readiness
}

// Write renders in the given format.
func Write(w io.Writer, f Format, fs []sdk.Finding, run Run) error {
	switch f {
	case FormatConsole:
		return Console(w, fs, run)
	case FormatJSON:
		return JSON(w, fs, run)
	case FormatMarkdown:
		return Markdown(w, fs, run)
	case FormatSARIF:
		return SARIF(w, fs, run)
	}
	return fmt.Errorf("unknown report format %q", string(f))
}

// prepared returns redacted, path-sanitised, sorted copies. Inputs are never
// mutated.
func prepared(in []sdk.Finding) []sdk.Finding {
	out := make([]sdk.Finding, len(in))
	for i, f := range in {
		if f.Fingerprint == "" {
			f.Fingerprint = findings.Fingerprint(f)
		}
		f = findings.RedactFinding(f)
		f.Location.File = SanitizePath(f.Location.File)
		f.Profile = findings.Redact(f.Profile)
		f.Pillar = findings.Redact(f.Pillar)
		f.DocsURL = findings.Redact(f.DocsURL)
		f.Adapter = findings.Redact(f.Adapter)
		f.Basis = append([]string(nil), f.Basis...)
		for j := range f.Basis {
			f.Basis[j] = findings.Redact(f.Basis[j])
		}
		if f.Suppressed != nil {
			s := *f.Suppressed
			s.Reason = findings.Redact(s.Reason)
			s.Owner = findings.Redact(s.Owner)
			f.Suppressed = &s
		}
		out[i] = f
	}
	findings.Sort(out)
	return out
}

func preparedSkips(in []sdk.Skip) []sdk.Skip {
	out := make([]sdk.Skip, len(in))
	for i, s := range in {
		s.Reason = findings.Redact(s.Reason)
		s.RuleID = findings.Redact(s.RuleID)
		out[i] = s
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RuleID != out[j].RuleID {
			return out[i].RuleID < out[j].RuleID
		}
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		return !out[i].Required && out[j].Required
	})
	return out
}

// active reports whether a finding is neither suppressed nor baselined.
func active(f sdk.Finding) bool { return f.Suppressed == nil && !f.Baselined }

type counts struct {
	Error, Warning, Info, Suppressed, Baselined int
}

func count(fs []sdk.Finding) counts {
	var c counts
	for _, f := range fs {
		switch {
		case f.Suppressed != nil:
			c.Suppressed++
		case f.Baselined:
			c.Baselined++
		default:
			switch f.Severity {
			case sdk.SeverityError:
				c.Error++
			case sdk.SeverityWarning:
				c.Warning++
			default:
				c.Info++
			}
		}
	}
	return c
}

// SanitizePath turns an untrusted path into a safe repo-relative,
// slash-separated path. Absolute paths, drive letters, UNC paths and
// traversal are reduced to their final element; control characters are
// removed. The result is never absolute and never contains "..".
func SanitizePath(p string) string {
	return location.SanitizePath(p)
}

// cleanText collapses control characters (including ANSI escapes and
// newlines) and bidi overrides to single spaces so text can never inject
// terminal sequences or extra lines.
func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsControl(r) || (r >= '\u2028' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

var ruleIDInvalid = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SafeRuleID returns a SARIF-valid rule id (letters, digits, '.', '_', '-').
func SafeRuleID(id string) string {
	id = ruleIDInvalid.ReplaceAllString(id, "_")
	id = strings.TrimLeft(id, "._-")
	if id == "" {
		return "unknown"
	}
	return id
}
