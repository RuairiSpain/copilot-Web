package bicep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// ToolName is the name used in the report's tools section and as the adapter name.
const ToolName = "bicep"

// DiscoverOptions controls CLI discovery. `az bicep` is never used.
type DiscoverOptions struct {
	// ExplicitPath is the configured executable (flag, config or BICEP_PATH read by the caller).
	// It must be absolute. When empty, `bicep` is looked up on PATH.
	ExplicitPath string
	// Required marks the tool as required for this run (a Bicep-backed project). The app layer
	// turns a required tool that is not available into exit code 2; this package never fails.
	Required bool
	// LookPath finds an executable on PATH. Nil means exec.LookPath.
	LookPath func(file string) (string, error)
	// Runner executes `bicep --version`. Nil means ExecRunner.
	Runner Runner
	// Timeout bounds the version probe. Zero means 15 seconds.
	Timeout time.Duration
}

// Discovery is the outcome of Discover. Path is absolute when State is available or
// unsupported-version and is what the compiler must execute.
type Discovery struct {
	Path    string
	Version Version
	Status  sdk.ToolStatus
}

// Available reports whether the compiler can be used.
func (d Discovery) Available() bool { return d.Status.State == sdk.ToolAvailable }

// Discover finds the Bicep CLI and reads its version. It never returns an error: a missing,
// unsupported or broken CLI is a sdk.ToolStatus.
func Discover(ctx context.Context, opts DiscoverOptions) Discovery {
	st := sdk.ToolStatus{Name: ToolName, Required: opts.Required, State: sdk.ToolMissing}
	look := opts.LookPath
	if look == nil {
		look = exec.LookPath
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	path := opts.ExplicitPath
	switch {
	case path != "":
		if !filepath.IsAbs(path) {
			st.State = sdk.ToolFailed
			st.Detail = "the configured Bicep path must be absolute"
			return Discovery{Status: st}
		}
	default:
		p, err := look("bicep")
		switch {
		case errors.Is(err, exec.ErrDot):
			st.Detail = "bicep resolves to a relative PATH entry; refusing to execute it"
			return Discovery{Status: st}
		case err != nil:
			st.Detail = "bicep was not found on PATH"
			return Discovery{Status: st}
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			st.Detail = "bicep found on PATH but its path cannot be made absolute"
			return Discovery{Status: st}
		}
		path = abs
	}
	st.Path = path

	fi, err := os.Stat(path)
	switch {
	case err != nil:
		st.Detail = "the Bicep executable does not exist"
		return Discovery{Status: st}
	case fi.IsDir() || fi.Mode()&0o111 == 0:
		st.Detail = "the Bicep path is not an executable file"
		return Discovery{Status: st}
	}

	home, cleanup, err := tempHome()
	if err != nil {
		st.State = sdk.ToolFailed
		st.Detail = "cannot create a temporary home for the version probe"
		return Discovery{Path: path, Status: st}
	}
	defer cleanup()
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	res, err := runner.Run(ctx, RunSpec{
		Path: path, Args: []string{"--version"}, Dir: home, Env: MinimalEnv(home),
		Timeout: timeout, MaxStdout: 64 << 10, MaxStderr: 64 << 10,
	})
	if err != nil || res.ExitCode != 0 {
		st.State = sdk.ToolFailed
		st.Detail = "`bicep --version` failed"
		return Discovery{Path: path, Status: st}
	}
	v, err := ParseVersion(string(res.Stdout))
	if err != nil {
		st.State = sdk.ToolFailed
		st.Detail = "`bicep --version` printed an unrecognised version"
		return Discovery{Path: path, Status: st}
	}
	st.Version = v.String()
	if v.Compare(MinVersion()) < 0 {
		st.State = sdk.ToolUnsupported
		st.Detail = fmt.Sprintf("Bicep %s is older than the minimum supported %s", v, MinVersion())
		return Discovery{Path: path, Version: v, Status: st}
	}
	st.State = sdk.ToolAvailable
	return Discovery{Path: path, Version: v, Status: st}
}

// tempHome creates an empty private directory used as HOME for one compiler run.
func tempHome() (string, func(), error) {
	dir, err := os.MkdirTemp("", "fdoctor-bicep-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp home: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}
