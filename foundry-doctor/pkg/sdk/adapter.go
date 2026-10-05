package sdk

import "context"

// ToolState is the detected state of an external tool.
type ToolState string

// ToolState values.
const (
	ToolAvailable   ToolState = "available"
	ToolMissing     ToolState = "missing"
	ToolUnsupported ToolState = "unsupported-version"
	ToolDisabled    ToolState = "disabled"
	ToolFailed      ToolState = "failed"
)

// ToolStatus is one entry of the report's tools section (ADR-001 decision 6).
type ToolStatus struct {
	Name     string    `json:"name"`
	Path     string    `json:"path,omitempty"` // never absolute in a report; the engine redacts it to the tool's base name
	Version  string    `json:"version,omitempty"`
	State    ToolState `json:"state"`
	Required bool      `json:"required"`
	Detail   string    `json:"detail,omitempty"`
}

// AdapterRequest is everything an adapter may use. Adapters are read-only and must not
// read outside ProjectRoot.
type AdapterRequest struct {
	ProjectRoot string            `json:"projectRoot"`
	Profile     string            `json:"profile"`
	Paths       []string          `json:"paths,omitempty"` // files to analyse, relative to ProjectRoot
	Options     map[string]string `json:"options,omitempty"`
}

// Adapter wraps an external tool and returns normalised findings with Adapter set.
// Adapters never decide severity; the engine applies the profile (ADR-001).
type Adapter interface {
	// Name is the stable adapter name, for example "psrule".
	Name() string
	// Detect reports whether the tool is usable. It must not fail the run.
	Detect(ctx context.Context) ToolStatus
	// Run executes the tool. A protocol failure or crash is an error (exit 4).
	Run(ctx context.Context, req AdapterRequest) ([]Finding, error)
}
