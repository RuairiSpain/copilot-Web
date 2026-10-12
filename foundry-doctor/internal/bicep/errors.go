// Package bicep adapts the external Bicep CLI: discovery, version checking,
// offline compilation to ARM JSON, diagnostics normalisation and ARM
// normalisation. It never parses Bicep source (ADR-002).
package bicep

import (
	"errors"
	"fmt"
)

// ContractVersion is the Bicep CLI version pinned by docs/tool-compatibility.md.
const ContractVersion = "0.48.1"

// Kind classifies adapter failures.
type Kind string

// Failure kinds.
const (
	KindMissing        Kind = "missing"
	KindNotExecutable  Kind = "not-executable"
	KindVersionTooOld  Kind = "version-too-old"
	KindVersionUnknown Kind = "version-unknown"
	KindAdapter        Kind = "adapter"
	KindTimeout        Kind = "timeout"
)

// Sentinel errors usable with errors.Is.
var (
	// ErrCLIMissing: Bicep was requested but the CLI is absent or not executable.
	ErrCLIMissing = errors.New("bicep CLI not found or not executable")
	// ErrVersionUnsupported: the CLI is older than the compatibility contract
	// or its version cannot be determined.
	ErrVersionUnsupported = errors.New("bicep CLI version unsupported")
	// ErrAdapter: the CLI ran but its output could not be trusted/parsed.
	ErrAdapter = errors.New("bicep CLI adapter failure")
)

// ToolError is the typed error returned for tool-level failures.
type ToolError struct {
	Kind   Kind
	Detail string // already sanitised; never raw compiler output
	Err    error
}

func (e *ToolError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("bicep: %s", e.Kind)
	}
	return fmt.Sprintf("bicep: %s: %s", e.Kind, e.Detail)
}

// Unwrap exposes the cause.
func (e *ToolError) Unwrap() error { return e.Err }

// Is maps kinds onto the sentinel errors.
func (e *ToolError) Is(target error) bool {
	switch target {
	case ErrCLIMissing:
		return e.Kind == KindMissing || e.Kind == KindNotExecutable
	case ErrVersionUnsupported:
		return e.Kind == KindVersionTooOld || e.Kind == KindVersionUnknown
	case ErrAdapter:
		return e.Kind == KindAdapter || e.Kind == KindTimeout
	}
	return false
}

// IsDependencyError reports whether callers must map err to exit code 2
// (required dependency missing, not executable, or unsupported version).
func IsDependencyError(err error) bool {
	return errors.Is(err, ErrCLIMissing) || errors.Is(err, ErrVersionUnsupported)
}

func toolErr(kind Kind, detail string, cause error) *ToolError {
	return &ToolError{Kind: kind, Detail: detail, Err: cause}
}
