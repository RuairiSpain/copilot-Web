// Package diag defines the diagnostics shared by every validation phase.
//
// Codes XF001-XF025 map one-to-one to the numbered semantic rules in the
// specification. XF1xx codes cover parsing, schema and additional checks.
package diag

import (
	"fmt"
	"strings"
)

// Severity of a diagnostic.
type Severity string

// Severities.
const (
	Error   Severity = "error"
	Warning Severity = "warning"
)

// Diagnostic is one finding.
type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
}

// String renders "error XF001 at path: message".
func (d Diagnostic) String() string {
	where := ""
	if d.Path != "" {
		where = " at " + d.Path
	}
	return fmt.Sprintf("%s %s%s: %s", d.Severity, d.Code, where, d.Message)
}

// Err builds an error diagnostic.
func Err(code, path, format string, args ...any) Diagnostic {
	return Diagnostic{Code: code, Severity: Error, Path: path, Message: fmt.Sprintf(format, args...)}
}

// Warn builds a warning diagnostic.
func Warn(code, path, format string, args ...any) Diagnostic {
	return Diagnostic{Code: code, Severity: Warning, Path: path, Message: fmt.Sprintf(format, args...)}
}

// Errors returns only the error diagnostics.
func Errors(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if d.Severity == Error {
			out = append(out, d)
		}
	}
	return out
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(ds []Diagnostic) bool { return len(Errors(ds)) > 0 }

// Dedupe drops repeated diagnostics (shared items are checked once per project).
func Dedupe(ds []Diagnostic) []Diagnostic {
	seen := make(map[Diagnostic]bool, len(ds))
	out := make([]Diagnostic, 0, len(ds))
	for _, d := range ds {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

// Failure is returned when a phase produced at least one error.
type Failure struct {
	Diagnostics []Diagnostic
}

// Error implements error.
func (f *Failure) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "x-foundry validation failed with %d error(s):", len(Errors(f.Diagnostics)))
	for _, d := range f.Diagnostics {
		b.WriteString("\n  " + d.String())
	}
	return b.String()
}

// Fail wraps diagnostics in a *Failure.
func Fail(ds ...Diagnostic) *Failure { return &Failure{Diagnostics: ds} }
