// Package app is the application layer of the standalone binary. It orchestrates one run:
// project discovery, configuration, azure.yaml parsing, the optional Bicep compile, the ARM
// model, the graph, rule execution, sanitising, fingerprints, baseline, suppressions, reporting
// and the exit code (PRD section 5, ADR-010). The cobra handlers in cmd/foundry-doctor call Run,
// Explain and Compare and contain no logic of their own.
//
// The package reads no environment variable, starts no network connection and links no Azure SDK.
// The clock, the rule set and the Bicep lookup are injected, so a run is a pure function of the
// project directory and the Options.
package app

import (
	"errors"
	"fmt"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// UnavailableMessage is the text printed for a flag that is registered but not implemented in this version
// (ADR-010 decision 3).
const UnavailableMessage = "not available in this version"

// ExitError carries the process exit code. Run returns nil for exit 0 and an *ExitError otherwise.
// Err is nil when the exit code is explained by the report itself (exit 1 and 3).
type ExitError struct {
	Code int
	Err  error
}

// Error implements error.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap returns the cause.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode implements the ExitCoder interface of cmd/foundry-doctor and internal/config.
func (e *ExitError) ExitCode() int { return e.Code }

// CodeOf returns the exit code for an error returned by Run, Explain or Compare: 0 for nil, the code
// of an *ExitError (or any error with an ExitCode method), and sdk.ExitInternal for anything else.
func CodeOf(err error) int {
	if err == nil {
		return sdk.ExitOK
	}
	var c interface{ ExitCode() int }
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	return sdk.ExitInternal
}

func exitErr(code int, format string, a ...any) *ExitError {
	return &ExitError{Code: code, Err: fmt.Errorf(format, a...)}
}

// BicepOptions injects the pieces of Bicep discovery that tests replace.
type BicepOptions struct {
	// LookPath finds an executable on PATH. Nil means exec.LookPath.
	LookPath func(file string) (string, error)
	// Runner executes the compiler. Nil means the real process runner.
	Runner bicep.Runner
}

// Options configure Run. The zero value of every field means "not given".
type Options struct {
	// Dir is the directory the project is searched from. Empty means ".".
	Dir string
	// ConfigPath overrides .foundry-doctor/config.yaml (--config).
	ConfigPath string
	// Profile is --profile: dev, test, prod or the long form.
	Profile string
	// Environment is --environment, an azd environment name.
	Environment string
	// Rules are --rules selector expressions; a non-empty list replaces the configured selection.
	Rules []string
	// MinSeverity is --min-severity: info (default), warning or error. It filters the display only.
	MinSeverity string
	// FailOn is --fail-on: the threshold for exit 1. Default error.
	FailOn string
	// Baseline and Suppressions override the files named in the configuration (--baseline, --suppressions).
	Baseline     string
	Suppressions string
	// WriteBaseline writes a baseline of the current findings to this project-relative path and treats
	// them as accepted for this run (the flag --write-baseline; ADR-010 defines no generator, see the
	// hand-off report).
	WriteBaseline string
	// Strict turns a skipped check into exit 3 when nothing else applies (--strict).
	Strict bool
	// Formats are the --format values: console, json, markdown or sarif. Empty means the configured formats.
	Formats []string
	// Out is --out: the file the (single) report is written to.
	Out string
	// Debug prints the effective policy, tool versions and the redacted adapter command to stderr.
	Debug bool
	// Color enables ANSI colour in the console report. The command layer decides (--no-color, NO_COLOR, terminal).
	Color bool
	// Unavailable lists the later-phase flags the user gave (for example "--preflight"). Any entry is exit 2.
	Unavailable []string
	// Tool is written into the report header.
	Tool sdk.ToolInfo
	// Now is the clock used for suppression expiry and the baseline date. It is required; the command layer
	// passes time.Now, tests pass a fixed time. The reports themselves carry no time.
	Now func() time.Time
	// Catalogue is the rule registry. Nil means engine.DefaultCatalogue.
	Catalogue *engine.Catalogue
	// Bicep injects the CLI lookup and runner.
	Bicep BicepOptions
}

// CompareOptions configure Compare.
type CompareOptions struct {
	Dir         string
	Left, Right string
	Format      string // console (default), json or markdown
	// Comparer compares two environments; nil means compare.Environments.
	Comparer Comparer
}

// ExplainOptions configure Explain.
type ExplainOptions struct {
	RuleID    string
	Format    string // console (default) or markdown
	Catalogue *engine.Catalogue
}
