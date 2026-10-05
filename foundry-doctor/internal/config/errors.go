package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Error is one problem in the configuration. Every Error belongs to the exit-2 class
// (PRD section 7: the requested validation could not run because input was unusable).
type Error struct {
	File string // file name as given by the caller; empty when the value came from a flag
	Line int    // 1-based line of the offending key or value; 0 when unknown
	Path string // dotted key path, for example policy.tags.required[1].format
	Msg  string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("config")
	if e.File != "" {
		b.WriteString(" ")
		b.WriteString(e.File)
		if e.Line > 0 {
			fmt.Fprintf(&b, ":%d", e.Line)
		}
	}
	b.WriteString(": ")
	if e.Path != "" {
		b.WriteString(e.Path)
		b.WriteString(": ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// ExitCode is sdk.ExitCannotRun.
func (e *Error) ExitCode() int { return sdk.ExitCannotRun }

// InvalidError groups all problems found in one configuration so the user fixes them in one pass.
type InvalidError struct{ Problems []*Error }

func (e *InvalidError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.Error()
	}
	return strings.Join(lines, "\n")
}

// Unwrap exposes every problem to errors.As and errors.Is.
func (e *InvalidError) Unwrap() []error {
	out := make([]error, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = p
	}
	return out
}

// ExitCode is sdk.ExitCannotRun.
func (e *InvalidError) ExitCode() int { return sdk.ExitCannotRun }

// ExitCoder is implemented by every error this package returns for unusable configuration.
type ExitCoder interface{ ExitCode() int }

// newInvalid sorts problems by line, then path, then message, and returns nil for an empty list.
func newInvalid(ps []*Error) error {
	if len(ps) == 0 {
		return nil
	}
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Msg < b.Msg
	})
	return &InvalidError{Problems: ps}
}

// ioError wraps a file-system failure; it is also exit 2 and keeps the cause for errors.Is.
type ioError struct {
	file string
	msg  string
	err  error
}

func (e *ioError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("config %s: %s: %v", e.file, e.msg, e.err)
	}
	return fmt.Sprintf("config %s: %s", e.file, e.msg)
}
func (e *ioError) Unwrap() error { return e.err }
func (e *ioError) ExitCode() int { return sdk.ExitCannotRun }
