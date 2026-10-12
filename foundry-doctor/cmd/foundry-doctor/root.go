package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/buildinfo"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// deps are the process-level inputs of the command layer. Tests replace them.
type deps struct {
	Dir       string               // working directory; the project is searched from here
	Now       func() time.Time     // the only wall clock in the program
	Getenv    func(string) string  // NO_COLOR only
	IsTTY     func(io.Writer) bool // colour only on a terminal
	Catalogue *engine.Catalogue    // nil means the real registry
	Bicep     app.BicepOptions     // nil fields mean the real CLI lookup
	Comparer  app.Comparer         // nil means internal/compare
	Version   string
	Commit    string
	Date      string
}

func osDeps() deps {
	wd, _ := os.Getwd()
	return deps{
		Dir: wd, Now: time.Now, Getenv: os.Getenv,
		IsTTY: func(w io.Writer) bool {
			f, ok := w.(*os.File)
			if !ok {
				return false
			}
			fi, err := f.Stat()
			return err == nil && fi.Mode()&os.ModeCharDevice != 0
		},
		Version: buildinfo.Version, Commit: buildinfo.Commit, Date: buildinfo.Date,
	}
}

// laterPhaseFlags are registered so the help text is stable, but are rejected (ADR-010 decision 3).
var laterPhaseFlags = []struct{ name, usage string }{
	{"preflight", "Azure deployment preflight"},
	{"runtime", "runtime diagnosis"},
	{"security", "live security checks"},
	{"what-if", "ARM what-if"},
	{"export-tool-config", "export generated tool configuration"},
	{"llm-explain", "LLM explanations"},
}

// usageError makes every flag or argument error exit 2.
func usageError(_ *cobra.Command, err error) error {
	return &usageErr{err: err}
}

func newRoot(d deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "foundry-doctor",
		Short: "Validate a Microsoft Foundry azd project offline",
		Long: `Foundry Doctor checks a Microsoft Foundry azd project against a catalogue of rules.
It is read-only, deterministic and offline: it reads azure.yaml, Bicep (compiled with the Bicep CLI)
and .azure/<env>/.env from the current directory and never calls Azure.

Exit codes: 0 no finding at or above --fail-on; 1 findings; 2 cannot run (input, config or required
tool); 3 a check was skipped and --strict was given; 4 internal error. When several apply: 4, 2, 1, 3, 0.`,
		Version:       d.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	root.SetVersionTemplate(buildinfo.Describe(d.Version, d.Commit, d.Date) + "\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetFlagErrorFunc(usageError)
	root.PersistentFlags().Bool("debug", false, "print the effective policy, tool versions and the redacted adapter command to stderr")
	root.PersistentFlags().Bool("no-color", false, "disable ANSI colour (the NO_COLOR environment variable is also respected)")

	root.AddCommand(newDoctor(d), newExplain(d), newCompare(d), newVersion(d))
	return root
}

func newVersion(d deps) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print the version",
		Example: "  foundry-doctor version",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(c.OutOrStdout(), buildinfo.Describe(d.Version, d.Commit, d.Date))
			return err
		},
	}
}

func color(c *cobra.Command, d deps) bool {
	if nc, _ := c.Flags().GetBool("no-color"); nc {
		return false
	}
	if d.Getenv != nil && d.Getenv("NO_COLOR") != "" {
		return false
	}
	return d.IsTTY != nil && d.IsTTY(c.OutOrStdout())
}

func newDoctor(d deps) *cobra.Command {
	var o app.Options
	var formats []string
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the project in the current directory",
		Long: `Check the azd project in the current directory (or the nearest parent holding azure.yaml).
Only --local is available in this version; it is the default.`,
		Example: `  # Check with the default (dev) profile
  foundry-doctor doctor

  # Production profile, SARIF for GitHub code scanning
  foundry-doctor doctor --profile prod --format sarif --out foundry-doctor.sarif

  # Warnings and above only, fail the build on warnings, treat skipped checks as failures
  foundry-doctor doctor --min-severity warning --fail-on warning --strict

  # Accept today's findings as a baseline, then fail only on new ones
  foundry-doctor doctor --write-baseline .foundry-doctor/baseline.yaml
  foundry-doctor doctor --baseline .foundry-doctor/baseline.yaml`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			o.Dir, o.Now, o.Catalogue, o.Bicep = d.Dir, d.Now, d.Catalogue, d.Bicep
			o.Formats = formats
			o.Tool = sdk.ToolInfo{Name: buildinfo.ToolName, Version: d.Version}
			o.Debug, _ = c.Flags().GetBool("debug")
			o.Color = color(c, d)
			for _, f := range laterPhaseFlags {
				if c.Flags().Changed(f.name) {
					o.Unavailable = append(o.Unavailable, "--"+f.name)
				}
			}
			return app.Run(c.Context(), o, c.OutOrStdout(), c.ErrOrStderr())
		},
	}
	f := c.Flags()
	f.Bool("local", true, "run the offline checks (the default and the only mode in this version)")
	for _, lf := range laterPhaseFlags {
		f.Bool(lf.name, false, "not available in this version ("+lf.usage+")")
	}
	f.StringVar(&o.Profile, "profile", "", "severity profile: dev, test or prod (default dev; foundry-dev|foundry-test|foundry-prod also accepted)")
	f.StringVar(&o.Environment, "environment", "", "azd environment to read (.azure/<name>/.env); default the azd default environment")
	f.StringVar(&o.ConfigPath, "config", "", "config file (default .foundry-doctor/config.yaml)")
	f.StringSliceVar(&o.Rules, "rules", nil, "rule selector, repeatable: ID, glob, group, must-have|nice-to-have, phase:N, !exclusion")
	f.StringVar(&o.MinSeverity, "min-severity", "", "hide findings below info, warning or error in the report (default info)")
	f.StringVar(&o.FailOn, "fail-on", "", "exit 1 for a finding at or above info, warning or error (default error)")
	f.StringVar(&o.Baseline, "baseline", "", "baseline file; hides findings with an exact fingerprint match")
	f.StringVar(&o.Suppressions, "suppressions", "", "suppressions file (rule, target, reason, owner, expiry)")
	f.StringVar(&o.WriteBaseline, "write-baseline", "", "write a baseline of the current findings to this project file and accept them for this run")
	f.BoolVar(&o.Strict, "strict", false, "exit 3 when a check was skipped and nothing else applies")
	f.StringSliceVar(&formats, "format", nil, "console, json, markdown or sarif; repeatable (default console)")
	f.StringVar(&o.Out, "out", "", "write the report to this file instead of stdout")
	return c
}

func newExplain(d deps) *cobra.Command {
	var o app.ExplainOptions
	c := &cobra.Command{
		Use:   "explain <rule-id>",
		Short: "Explain a rule from the catalogue",
		Long:  "Print the catalogue entry of a rule: rationale, evidence, remediation, severity per profile, owner, compatibility and sources.",
		Example: `  foundry-doctor explain FND-SEC-001
  foundry-doctor explain FND-CFG-001 --format markdown`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			o.RuleID, o.Catalogue = args[0], d.Catalogue
			return app.Explain(c.Context(), o, c.OutOrStdout(), c.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&o.Format, "format", "", "console or markdown (default console)")
	return c
}

func newCompare(d deps) *cobra.Command {
	var o app.CompareOptions
	c := &cobra.Command{
		Use:   "compare <left-env> <right-env>",
		Short: "Compare the keys of two azd environments",
		Long:  "Compare .azure/<left>/.env with .azure/<right>/.env. Key names and the kind of difference are shown; values never are.",
		Example: `  foundry-doctor compare dev prod
  foundry-doctor compare dev prod --format json`,
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			o.Dir, o.Left, o.Right, o.Comparer = d.Dir, args[0], args[1], d.Comparer
			return app.Compare(c.Context(), o, c.OutOrStdout(), c.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&o.Format, "format", "", "console, json or markdown (default console)")
	return c
}

// usageErr is a flag or argument error. It is always exit 2 (invalid input).
type usageErr struct{ err error }

func (e *usageErr) Error() string { return e.err.Error() }
func (e *usageErr) Unwrap() error { return e.err }
func (e *usageErr) ExitCode() int { return sdk.ExitCannotRun }

// execute runs the command line and returns the process exit code. The message of an *app.ExitError has
// already been written by the app layer; usage errors are written here.
func execute(ctx context.Context, args []string, stdout, stderr io.Writer, d deps) int {
	root := newRoot(d)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return sdk.ExitOK
	}
	var ee *app.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	// A usage error, or an error cobra raised itself (unknown command, wrong argument count).
	fmt.Fprintf(stderr, "error: %s\nRun 'foundry-doctor --help' for usage\n", err)
	return sdk.ExitCannotRun
}
