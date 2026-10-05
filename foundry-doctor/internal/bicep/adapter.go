package bicep

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Adapter exposes the compiler's diagnostics as sdk.Adapter findings. ARM output is not part of
// the sdk contract; the app layer uses Compiler and Normalise directly for it.
type Adapter struct {
	d    Discovery
	opts CompilerOptions
}

var _ sdk.Adapter = (*Adapter)(nil)

// NewAdapter wraps a discovery result. The adapter can be built from an unavailable discovery;
// Detect then reports it and Run returns ErrCLIUnavailable.
func NewAdapter(d Discovery, opts CompilerOptions) *Adapter { return &Adapter{d: d, opts: opts} }

// Name implements sdk.Adapter.
func (a *Adapter) Name() string { return ToolName }

// Detect implements sdk.Adapter. It reports the discovery result and never fails the run.
func (a *Adapter) Detect(context.Context) sdk.ToolStatus { return a.d.Status }

// Run implements sdk.Adapter: it compiles every .bicep and .bicepparam path in req.Paths and
// returns one finding per compiler diagnostic, in file then compiler order. A compile error is
// a finding, not a Go error.
func (a *Adapter) Run(ctx context.Context, req sdk.AdapterRequest) ([]sdk.Finding, error) {
	opts := a.opts
	opts.ProjectDir = req.ProjectRoot
	c, err := NewCompiler(a.d, opts)
	if err != nil {
		return nil, fmt.Errorf("bicep adapter: %w", err)
	}
	var out []sdk.Finding
	for _, p := range req.Paths {
		var ds []sdk.Finding
		switch strings.ToLower(filepath.Ext(p)) {
		case ".bicep":
			res, err := c.Compile(ctx, p)
			if err != nil {
				return out, fmt.Errorf("bicep adapter: %w", err)
			}
			for _, d := range res.Diagnostics {
				ds = append(ds, ToFinding(d, req.Profile))
			}
		case ".bicepparam":
			res, err := c.CompileParams(ctx, p)
			if err != nil {
				return out, fmt.Errorf("bicep adapter: %w", err)
			}
			for _, d := range res.Diagnostics {
				ds = append(ds, ToFinding(d, req.Profile))
			}
		default:
			return out, fmt.Errorf("bicep adapter: %q: %w", filepath.Base(p), ErrBadInput)
		}
		out = append(out, ds...)
	}
	return out, nil
}
