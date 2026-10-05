// Package app composes the Foundry Doctor pipeline (PRD section 5):
//
//	discovery -> config -> azure.yaml -> optional Bicep/ARM -> rules ->
//	baseline/suppress -> report -> exit code
//
// Every stage is an interface defined here, at the consumer, so the CLI and
// the tests can inject fakes and no stage needs Azure credentials. The
// production wiring lives in wire.go.
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Exit codes (PRD section 7).
const (
	ExitOK          = 0 // no unsuppressed finding at or above --fail-on
	ExitFindings    = 1 // findings meet or exceed the threshold
	ExitUnavailable = 2 // the requested validation could not run
	ExitStrictSkip  = 3 // a check was skipped and --strict was given
	ExitInternal    = 4 // internal error or adapter protocol failure
)

// ErrUnavailable marks an input, dependency, authentication or permission
// that is required but unavailable. It maps to exit code 2.
var ErrUnavailable = errors.New("required input unavailable")

// UsageError marks invalid flags or configuration. It maps to exit code 2.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef builds a UsageError.
func Usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// ExitCodeForError maps a pipeline error to an exit code. Unavailable inputs
// and usage/config errors are 2; everything else is an internal error (4).
func ExitCodeForError(err error) int {
	if err == nil {
		return ExitOK
	}
	var ue *UsageError
	if errors.Is(err, ErrUnavailable) || errors.As(err, &ue) {
		return ExitUnavailable
	}
	return ExitInternal
}

// Outcome is the input to exit-code selection.
type Outcome struct {
	// Findings are the post-baseline, post-suppression findings.
	Findings []sdk.Finding
	// Skips are every check that did not run.
	Skips []sdk.Skip
	// FailOn is the failure threshold.
	FailOn sdk.Severity
	// Strict turns any skipped check into exit 3.
	Strict bool
}

// ExitCode applies PRD section 7. Precedence: required-skip (2) over strict
// skip (3) over findings (1) over success (0). Internal errors (4) are
// decided by ExitCodeForError before an Outcome exists.
func ExitCode(o Outcome) int {
	return report.ClassifyExit(report.Outcome{
		Findings: o.Findings, FailOn: o.FailOn, Strict: o.Strict, Skipped: o.Skips,
	})
}

// Inputs are the effective project input locations from configuration.
type Inputs struct {
	AzureYAML   string // path relative to the project directory
	InfraPath   string // path relative to the project directory
	Environment string // azd environment name, may be empty
}

// Source is the discovered project input (read by ProjectLoader).
type Source struct {
	AzureYAML   []byte // contents of azure.yaml
	AzureYAMLAt string // repo-relative, slash-separated path of azure.yaml
	InfraPath   string // repo-relative infra directory, may be empty
	Environment string // azd environment name, may be empty
	Warnings    []string
	// Dir is the project directory the paths above are relative to.
	Dir string
	// Environments lists the azd environments found under .azure.
	Environments []string
}

// ProjectLoader discovers azure.yaml and the infra path under dir. A missing
// azure.yaml must wrap ErrUnavailable.
type ProjectLoader interface {
	Load(ctx context.Context, dir string, in Inputs) (Source, error)
}

// ConfigRequest carries the flag layer of the config precedence chain.
type ConfigRequest struct {
	Dir         string // project directory holding the repo config
	Profile     string // flag value, empty when unset
	Environment string // environment to resolve policy for
	Rules       []string
	Baseline    string
	Suppress    string
}

// Settings is the effective, merged configuration.
type Settings struct {
	Profile     string // curated profile name, for example foundry-prod
	Environment string
	Inputs      Inputs
	// Exclude holds selectors removed from Rules.
	Exclude []string
	// Policy is the effective policy flattened to dotted keys (ADR-007).
	Policy       map[string]any
	Rules        []string // selectors after merging flags over config
	Baseline     string
	Suppressions string
	AzdVersion   string
}

// ConfigResolver resolves flags > environment > repo config > profile.
// Invalid or unknown keys must return a *UsageError.
type ConfigResolver interface {
	Resolve(ctx context.Context, req ConfigRequest) (Settings, error)
}

// ErrARMUnavailable marks an ARM model that could not be produced (Bicep CLI
// missing or unsupported, compile errors, no entry file). The pipeline warns on
// stderr and continues with no model, so ARM-dependent rules are skipped as
// input-unavailable and never reported as passed.
var ErrARMUnavailable = errors.New("bicep/ARM model unavailable")

// ARMLoader optionally compiles Bicep to an ARM model. It is nil when the
// Bicep stage is not wired; a nil model means rules skip ARM-dependent checks.
// Implementations return an error wrapping ErrARMUnavailable (never a typed
// nil model) when the stage cannot run.
type ARMLoader interface {
	Load(ctx context.Context, src Source) (sdk.ARMModel, error)
}

// RunInput is the rule engine input.
type RunInput struct {
	Profile     string
	Environment string
	AzdVersion  string
	Selectors   []string
	Exclude     []string
	Local       bool
	// Source is the discovered project, for providers that need the layout
	// and azd environments.
	Source    Source
	AzureYAML sdk.AzureYAMLView
	ARM       sdk.ARMModel
	Policy    sdk.Policy
}

// RunOutput is the rule engine output. Skips are never converted to passes.
type RunOutput struct {
	Findings []sdk.Finding
	Skipped  []sdk.Skip
	// Evaluated lists the rule IDs that ran. Optional; the preflight
	// readiness summary falls back to every FND-DEP rule when it is empty.
	Evaluated []string
}

// Engine runs the selected rules.
type Engine interface {
	Run(ctx context.Context, in RunInput) (RunOutput, error)
}

// FindingFilter applies baseline or suppressions to findings. Matching
// findings are marked (Baselined / Suppressed) and expired suppressions add
// an error finding.
type FindingFilter interface {
	Apply(ctx context.Context, path string, in []sdk.Finding, now time.Time) ([]sdk.Finding, error)
}

// Report is the data handed to a Reporter.
type Report struct {
	Profile     string
	Environment string
	Policy      map[string]any
	Findings    []sdk.Finding
	Skipped     []sdk.Skip
	ExitCode    int
	// Readiness is set by the preflight command only.
	Readiness *report.Readiness
}

// Reporter renders a report in console, json, markdown or sarif.
type Reporter interface {
	Render(w io.Writer, format string, r Report) error
}

// Explainer renders documentation for one rule.
type Explainer interface {
	Explain(ctx context.Context, ruleID, format string, w io.Writer) error
}

// Services bundles the injected pipeline stages.
type Services struct {
	Project     ProjectLoader
	Config      ConfigResolver
	ARM         ARMLoader // optional
	Engine      Engine
	Baseline    FindingFilter
	Suppress    FindingFilter
	Reporter    Reporter
	Explainer   Explainer
	Preflight   PreflightEngine // optional; preflight reports unavailable when nil
	Runtime     RuntimeEngine   // optional; runtime reports unavailable when nil
	Now         func() time.Time
	Stderr      io.Writer
	WriteOutput func(path string, data []byte) error // defaults to os.WriteFile
}

// Formats lists the supported report formats.
var Formats = []string{"console", "json", "markdown", "sarif"}

// DoctorRequest mirrors the doctor flags (PRD section 4).
type DoctorRequest struct {
	Dir         string
	Profile     string
	Local       bool
	Rules       []string
	MinSeverity sdk.Severity
	FailOn      sdk.Severity
	Baseline    string
	Suppress    string
	Strict      bool
	Format      string
	Out         string
}

// Validate checks flag values and applies defaults.
func (r *DoctorRequest) Validate() error {
	if r.Format == "" {
		r.Format = "console"
	}
	if !slices.Contains(Formats, r.Format) {
		return Usagef("invalid --format %q (want %v)", r.Format, Formats)
	}
	if r.Profile != "" && !slices.Contains([]string{"dev", "test", "prod"}, r.Profile) {
		return Usagef("invalid --profile %q (want dev, test or prod)", r.Profile)
	}
	if r.MinSeverity == "" {
		r.MinSeverity = sdk.SeverityInfo
	}
	if r.FailOn == "" {
		r.FailOn = sdk.SeverityError
	}
	if !r.MinSeverity.Valid() {
		return Usagef("invalid --min-severity %q", r.MinSeverity)
	}
	if !r.FailOn.Valid() {
		return Usagef("invalid --fail-on %q", r.FailOn)
	}
	if r.FailOn.Rank() < r.MinSeverity.Rank() {
		return Usagef("--fail-on %s is below --min-severity %s", r.FailOn, r.MinSeverity)
	}
	if r.Dir == "" {
		r.Dir = "."
	}
	return nil
}

// Doctor runs the pipeline, writes the report to stdout (or --out) and
// returns the exit code. A non-nil error accompanies exit codes 2 and 4 and
// has already been mapped by ExitCodeForError.
func Doctor(ctx context.Context, svc Services, req DoctorRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	code, err := doctor(ctx, svc, req, stdout)
	if err != nil {
		return ExitCodeForError(err), err
	}
	return code, nil
}

func doctor(ctx context.Context, svc Services, req DoctorRequest, stdout io.Writer) (int, error) {
	set, err := svc.Config.Resolve(ctx, ConfigRequest{
		Dir: req.Dir, Profile: req.Profile,
		Rules: req.Rules, Baseline: req.Baseline, Suppress: req.Suppress,
	})
	if err != nil {
		return 0, fmt.Errorf("resolve config: %w", err)
	}
	src, err := svc.Project.Load(ctx, req.Dir, set.Inputs)
	if err != nil {
		return 0, fmt.Errorf("discover project: %w", err)
	}
	if svc.Stderr != nil {
		for _, w := range src.Warnings {
			_, _ = fmt.Fprintf(svc.Stderr, "warning: %s\n", findings.Redact(w))
		}
	}

	doc, err := azureyaml.Parse(src.AzureYAML, src.AzureYAMLAt)
	if err != nil {
		return 0, fmt.Errorf("%w: parse azure.yaml: %w", ErrUnavailable, err)
	}
	for _, d := range doc.Diagnostics() {
		if svc.Stderr != nil {
			_, _ = fmt.Fprintf(svc.Stderr, "warning: %s:%d:%d: %s\n",
				d.Location.File, d.Location.Line, d.Location.Column, findings.Redact(d.Message))
		}
	}

	var arm sdk.ARMModel
	if svc.ARM != nil {
		arm, err = svc.ARM.Load(ctx, src)
		switch {
		case err == nil:
		case errors.Is(err, ErrARMUnavailable) && ctx.Err() == nil:
			arm = nil
			if svc.Stderr != nil {
				_, _ = fmt.Fprintf(svc.Stderr, "warning: %s; Bicep-dependent rules are skipped\n", findings.Redact(err.Error()))
			}
		default:
			return 0, fmt.Errorf("load bicep/arm model: %w", err)
		}
	}

	out, err := svc.Engine.Run(ctx, RunInput{
		Profile: set.Profile, Environment: set.Environment, AzdVersion: set.AzdVersion,
		Selectors: set.Rules, Exclude: set.Exclude, Local: req.Local, Source: src,
		AzureYAML: doc, ARM: arm, Policy: MapPolicy(set.Policy),
	})
	if err != nil {
		return 0, fmt.Errorf("run rules: %w", err)
	}
	fs := out.Findings
	if set.Baseline != "" && svc.Baseline != nil {
		if fs, err = svc.Baseline.Apply(ctx, set.Baseline, fs, svc.now()); err != nil {
			return 0, fmt.Errorf("apply baseline: %w", err)
		}
	}
	if set.Suppressions != "" && svc.Suppress != nil {
		if fs, err = svc.Suppress.Apply(ctx, set.Suppressions, fs, svc.now()); err != nil {
			return 0, fmt.Errorf("apply suppressions: %w", err)
		}
	}
	for i := range fs {
		fs[i] = findings.RedactFinding(fs[i])
	}
	// The fail-on decision is made on the displayed findings, so a threshold
	// below --min-severity could never trigger and is rejected up front.
	shown := make([]sdk.Finding, 0, len(fs))
	for _, f := range fs {
		if f.Severity.Rank() >= req.MinSeverity.Rank() {
			shown = append(shown, f)
		}
	}
	findings.Sort(shown)
	skips := slices.Clone(out.Skipped)
	slices.SortFunc(skips, func(a, b sdk.Skip) int {
		if a.RuleID != b.RuleID {
			if a.RuleID < b.RuleID {
				return -1
			}
			return 1
		}
		if a.Reason < b.Reason {
			return -1
		} else if a.Reason > b.Reason {
			return 1
		}
		return 0
	})

	// The exit code is part of the report, so classify before rendering.
	code := ExitCode(Outcome{Findings: shown, Skips: skips, FailOn: req.FailOn, Strict: req.Strict})
	var buf bytes.Buffer
	rep := Report{Profile: set.Profile, Environment: set.Environment, Policy: set.Policy, Findings: shown, Skipped: skips, ExitCode: code}
	if err := svc.Reporter.Render(&buf, req.Format, rep); err != nil {
		return 0, fmt.Errorf("render %s report: %w", req.Format, err)
	}
	if err := svc.emit(req.Out, buf.Bytes(), stdout); err != nil {
		return 0, err
	}
	return code, nil
}

func (s Services) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Services) emit(path string, data []byte, stdout io.Writer) error {
	if path == "" {
		_, err := stdout.Write(data)
		if err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		return nil
	}
	write := s.WriteOutput
	if write == nil {
		write = func(p string, b []byte) error {
			if dir := filepath.Dir(p); dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}
			return os.WriteFile(p, b, 0o644)
		}
	}
	if err := write(path, data); err != nil {
		return fmt.Errorf("write --out %q: %w", path, err)
	}
	return nil
}

// Explain renders rule documentation. Unknown rules are a usage error (2).
func Explain(ctx context.Context, svc Services, ruleID, format string, stdout io.Writer) (int, error) {
	if format == "" {
		format = "console"
	}
	if format != "console" && format != "markdown" {
		err := Usagef("invalid --format %q for explain (want console or markdown)", format)
		return ExitCodeForError(err), err
	}
	if ruleID == "" {
		err := Usagef("rule ID is required")
		return ExitCodeForError(err), err
	}
	if err := svc.Explainer.Explain(ctx, ruleID, format, stdout); err != nil {
		return ExitCodeForError(err), err
	}
	return ExitOK, nil
}
