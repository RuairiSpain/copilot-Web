package bicep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Errors returned by NewCompiler and the compile functions. A Bicep compile error is NOT a Go
// error: it is reported in Result.Diagnostics with Result.OK false.
var (
	// ErrCLIUnavailable means the Bicep CLI is missing, unsupported or broken. When Bicep is
	// required the app layer maps it to exit code 2.
	ErrCLIUnavailable = errors.New("bicep CLI is not available")
	// ErrBadInput means the file is not a .bicep or .bicepparam file inside the project.
	ErrBadInput = errors.New("bicep: input file is not allowed")
	// ErrNoDiagnostics means the compiler failed without printing a diagnostic: an adapter error (exit 4).
	ErrNoDiagnostics = errors.New("bicep: compiler failed without a parsable diagnostic")
)

// Skip reasons reported when a compile cannot provide ARM.
const (
	// SkipParamEnvVar: a .bicepparam reads an environment variable that the doctor deliberately
	// does not forward (BCP427). Checks that need the compiled template or parameters are skipped.
	SkipParamEnvVar = "bicepparam-env-var-unavailable"
	// SkipCompileFailed: the compiler reported errors, so no ARM exists.
	SkipCompileFailed = "bicep-compile-failed"
)

// codeEnvVarMissing is the compiler code for readEnvironmentVariable without a value or default.
const codeEnvVarMissing = "BCP427"

// Skip says why dependent checks cannot run after a compile.
type Skip struct {
	Reason            string
	Detail            string
	MissingCapability string
}

// CompilerOptions configures a Compiler. Zero values select safe defaults.
type CompilerOptions struct {
	// ProjectDir is the working directory of the compiler and the root every input must stay in.
	ProjectDir string
	Runner     Runner
	Timeout    time.Duration
	MaxStdout  int
	MaxStderr  int
	// Home is the HOME handed to the compiler. Empty means a fresh temporary directory per run.
	Home string
}

// Compiler runs the Bicep CLI for one project. It is safe for concurrent use.
type Compiler struct {
	d    Discovery
	opts CompilerOptions
	root string // ProjectDir with symlinks resolved
}

// NewCompiler returns a Compiler for an available discovery. It returns ErrCLIUnavailable
// (wrapped, with the tool detail) otherwise.
func NewCompiler(d Discovery, opts CompilerOptions) (*Compiler, error) {
	if !d.Available() {
		return nil, fmt.Errorf("new compiler (%s): %w: %s", d.Status.State, ErrCLIUnavailable, d.Status.Detail)
	}
	if opts.ProjectDir == "" {
		return nil, fmt.Errorf("new compiler: project directory is empty: %w", ErrBadInput)
	}
	abs, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("new compiler: %w", err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("new compiler: project directory: %w", err)
	}
	if opts.Runner == nil {
		opts.Runner = ExecRunner{}
	}
	return &Compiler{d: d, opts: opts, root: root}, nil
}

// Version is the detected CLI version.
func (c *Compiler) Version() Version { return c.d.Version }

// Result is the outcome of Compile.
type Result struct {
	OK          bool            // true when the compiler exited 0 and produced ARM
	ARM         json.RawMessage // compiled template; nil when !OK
	Diagnostics []model.Diagnostic
	Skipped     []Skip
	Version     Version
}

// ParamsResult is the outcome of CompileParams.
type ParamsResult struct {
	OK             bool
	Template       json.RawMessage // the template named by `using`
	Parameters     json.RawMessage // the parameters file (parametersJson)
	TemplateSpecID string
	// Values holds the literal parameter values from the parameters file, by name. Key Vault
	// references are not included. The values may be sensitive; do not print them.
	Values      map[string]any
	Diagnostics []model.Diagnostic
	Skipped     []Skip
	Version     Version
}

// Compile runs `bicep build <file> --stdout --no-restore`. Compiler diagnostics are returned in
// the result; `#disable-next-line` directives in the entry file are added as informational ones.
func (c *Compiler) Compile(ctx context.Context, file string) (Result, error) {
	abs, err := c.checkInput(file, ".bicep")
	if err != nil {
		return Result{}, err
	}
	run, err := c.run(ctx, []string{"build", abs, "--stdout", "--no-restore"})
	if err != nil {
		return Result{}, fmt.Errorf("compile %s: %w", filepath.Base(abs), err)
	}
	res := Result{Version: c.d.Version}
	res.Diagnostics = c.diagnostics(run.Stderr)
	switch {
	case run.ExitCode == 0:
		if !json.Valid(run.Stdout) {
			return res, fmt.Errorf("compile %s: stdout is not JSON: %w", filepath.Base(abs), ErrNoDiagnostics)
		}
		res.OK, res.ARM = true, json.RawMessage(run.Stdout)
	case HasError(res.Diagnostics):
		res.Skipped = append(res.Skipped, Skip{Reason: SkipCompileFailed, Detail: "the Bicep compiler reported errors", MissingCapability: "compiled ARM template"})
	default:
		return res, fmt.Errorf("compile %s: exit %d: %w", filepath.Base(abs), run.ExitCode, ErrNoDiagnostics)
	}
	if ds, derr := ScanDisableDirectives(c.root, abs); derr == nil {
		res.Diagnostics = append(res.Diagnostics, ds...)
	}
	return res, nil
}

// CompileParams runs `bicep build-params <file> --stdout --no-restore` and decodes templateJson
// and parametersJson. Environment variables are never forwarded, so readEnvironmentVariable
// without a default fails with BCP427; that is mapped to a diagnostic plus a Skip.
func (c *Compiler) CompileParams(ctx context.Context, file string) (ParamsResult, error) {
	abs, err := c.checkInput(file, ".bicepparam")
	if err != nil {
		return ParamsResult{}, err
	}
	run, err := c.run(ctx, []string{"build-params", abs, "--stdout", "--no-restore"})
	if err != nil {
		return ParamsResult{}, fmt.Errorf("compile params %s: %w", filepath.Base(abs), err)
	}
	res := ParamsResult{Version: c.d.Version}
	res.Diagnostics = c.diagnostics(run.Stderr)
	for _, d := range res.Diagnostics {
		if isEnvVarMissing(d) {
			res.Skipped = append(res.Skipped, Skip{
				Reason:            SkipParamEnvVar,
				Detail:            "the parameters file reads an environment variable; the doctor never forwards environment variables to the compiler",
				MissingCapability: "parameters resolved without environment variables",
			})
			break
		}
	}
	switch {
	case run.ExitCode == 0:
		if err := res.decodeParams(run.Stdout); err != nil {
			return res, fmt.Errorf("compile params %s: %w", filepath.Base(abs), err)
		}
		res.OK = true
	case HasError(res.Diagnostics):
		if len(res.Skipped) == 0 {
			res.Skipped = append(res.Skipped, Skip{Reason: SkipCompileFailed, Detail: "the Bicep compiler reported errors", MissingCapability: "compiled ARM template"})
		}
	default:
		return res, fmt.Errorf("compile params %s: exit %d: %w", filepath.Base(abs), run.ExitCode, ErrNoDiagnostics)
	}
	return res, nil
}

func (r *ParamsResult) decodeParams(stdout []byte) error {
	var raw struct {
		TemplateJSON   string  `json:"templateJson"`
		ParametersJSON string  `json:"parametersJson"`
		TemplateSpecID *string `json:"templateSpecId"`
	}
	if err := json.Unmarshal(stdout, &raw); err != nil {
		return fmt.Errorf("decode build-params output: %w", err)
	}
	if !json.Valid([]byte(raw.TemplateJSON)) || !json.Valid([]byte(raw.ParametersJSON)) {
		return fmt.Errorf("build-params output has no valid templateJson/parametersJson: %w", ErrNoDiagnostics)
	}
	r.Template = json.RawMessage(raw.TemplateJSON)
	r.Parameters = json.RawMessage(raw.ParametersJSON)
	if raw.TemplateSpecID != nil {
		r.TemplateSpecID = *raw.TemplateSpecID
	}
	var pf struct {
		Parameters map[string]struct {
			Value     any `json:"value"`
			Reference any `json:"reference"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(r.Parameters, &pf); err != nil {
		return fmt.Errorf("decode parametersJson: %w", err)
	}
	r.Values = make(map[string]any, len(pf.Parameters))
	for name, p := range pf.Parameters {
		if p.Reference == nil {
			r.Values[name] = p.Value
		}
	}
	return nil
}

func (c *Compiler) diagnostics(stderr []byte) []model.Diagnostic {
	parsed := ParseDiagnostics(string(stderr))
	out := make([]model.Diagnostic, 0, len(parsed))
	for _, p := range parsed {
		out = append(out, toModel(p, c.root))
	}
	return out
}

func (c *Compiler) run(ctx context.Context, args []string) (RunResult, error) {
	home, cleanup := c.opts.Home, func() {}
	if home == "" {
		var err error
		if home, cleanup, err = tempHome(); err != nil {
			return RunResult{}, err
		}
	}
	defer cleanup()
	return c.opts.Runner.Run(ctx, RunSpec{
		Path: c.d.Path, Args: args, Dir: c.root, Env: MinimalEnv(home),
		Timeout: c.opts.Timeout, MaxStdout: c.opts.MaxStdout, MaxStderr: c.opts.MaxStderr,
	})
}

// checkInput resolves file against the project, requires the extension and refuses paths that
// leave the project (including through symlinks).
func (c *Compiler) checkInput(file, ext string) (string, error) {
	if !strings.EqualFold(filepath.Ext(file), ext) {
		return "", fmt.Errorf("%q does not have the %s extension: %w", filepath.Base(file), ext, ErrBadInput)
	}
	p := file
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.root, p)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%q does not exist: %w", filepath.Base(file), ErrBadInput)
		}
		return "", fmt.Errorf("resolve %q: %w", filepath.Base(file), err)
	}
	rel, err := filepath.Rel(c.root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is outside the project: %w", filepath.Base(file), ErrBadInput)
	}
	return real, nil
}

// isEnvVarMissing recognises the "environment variable does not exist" error: BCP427 on current
// compilers, BCP338 ("Failed to evaluate parameter") with the same cause on 0.24.24.
func isEnvVarMissing(d model.Diagnostic) bool {
	if d.Severity != sdk.SeverityError {
		return false
	}
	return d.Code == codeEnvVarMissing || (d.Code == "BCP338" && strings.Contains(d.Message, "Environment variable"))
}
