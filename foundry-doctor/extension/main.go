package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const rootUse = "foundry"

var version = app.ToolVersion

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	svc := app.DefaultServices(os.Stderr)
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, svc))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, svc app.Services) int {
	exit := app.ExitOK
	root := newRoot(ctx, svc, &exit)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", err)
		if exit == app.ExitOK {
			exit = app.ExitUnavailable
		}
	}
	return exit
}

func newRoot(ctx context.Context, svc app.Services, exit *int) *cobra.Command {
	root := &cobra.Command{
		Use:           rootUse,
		Short:         "Run Foundry Doctor as an azd extension",
		Long:          "Foundry Doctor runs under azd as `azd foundry <command>` and shares the same\nread-only command surface as the standalone `foundry-doctor` binary.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		doctorCmd(ctx, svc, exit),
		annotateCmd(ctx, svc, exit),
		explainCmd(ctx, svc, exit),
		compareCmd(ctx, svc, exit),
		costCmd(ctx, svc, exit),
		preflightCmd(ctx, svc, exit),
		runtimeCmd(ctx, svc, exit),
		graphCmd(ctx, svc, exit),
		assessCmd(ctx, svc, exit),
		versionCmd(exit),
	)
	return root
}

func finish(exit *int, code int, err error) error {
	*exit = code
	return err
}

func doctorCmd(ctx context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		profile, minSev, failOn, baseline, suppress, format, out, dir string
		local, strict                                                 bool
		rules                                                         []string
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate the azd project in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := app.DoctorRequest{
				Dir: dir, Profile: profile, Local: local, Rules: rules,
				Baseline: baseline, Suppress: suppress, Strict: strict, Format: format, Out: out,
			}
			var err error
			if req.MinSeverity, err = optSeverity("--min-severity", minSev); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			if req.FailOn, err = optSeverity("--fail-on", failOn); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			code, err := app.Doctor(cmd.Context(), svc, req, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.BoolVar(&local, "local", false, "offline mode: validate local files only, no Azure access")
	f.StringSliceVar(&rules, "rules", nil, "rule selectors: IDs or globs such as FND-CFG-*; repeatable or comma-separated")
	f.StringVar(&minSev, "min-severity", "", "lowest severity to report: info, warning or error (default info)")
	f.StringVar(&failOn, "fail-on", "", "lowest severity that fails the run: info, warning or error (default error)")
	f.StringVar(&baseline, "baseline", "", "baseline file; hides only findings with matching fingerprints")
	f.StringVar(&suppress, "suppressions", "", "suppressions file (reason, owner and expiry required)")
	f.BoolVar(&strict, "strict", false, "exit 3 when any check was skipped")
	f.StringVar(&format, "format", "console", "report format: "+strings.Join(app.Formats, ", "))
	f.StringVar(&out, "out", "", "write the report to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	return cmd
}

func optSeverity(flag, v string) (sdk.Severity, error) {
	if v == "" {
		return "", nil
	}
	s, err := sdk.ParseSeverity(v)
	if err != nil {
		return "", app.Usagef("%s: %s", flag, err)
	}
	return s, nil
}

func explainCmd(ctx context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		format, llmProvider, audience string
		llmExplain                    bool
		llmTimeout                    time.Duration
	)
	cmd := &cobra.Command{
		Use:   "explain <rule-id>",
		Short: "Explain a rule: what it checks, why, how to fix it and its sources",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := app.ExplainRule(cmd.Context(), svc, app.ExplainRequest{
				RuleID: args[0], Format: format, LLMExplain: llmExplain,
				LLMProvider: llmProvider, LLMTimeout: llmTimeout, LLMAudience: llm.Audience(audience),
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	cmd.Flags().StringVar(&format, "format", "console", "output format: console or markdown")
	cmd.Flags().BoolVar(&llmExplain, "llm-explain", false, "append an advisory generated explanation (explicit opt-in)")
	cmd.Flags().StringVar(&audience, "audience", string(llm.AudienceDeveloper), "advisory audience when --llm-explain is set: owner or developer")
	cmd.Flags().StringVar(&llmProvider, "llm-provider", "", "advisory provider override (allow-listed; default azure-openai)")
	cmd.Flags().DurationVar(&llmTimeout, "llm-timeout", 0, "advisory provider timeout when --llm-explain is set (1s to 30s)")
	return cmd
}

func compareCmd(ctx context.Context, svc app.Services, exit *int) *cobra.Command {
	var format, out, dir string
	var failOnDiff bool
	cmd := &cobra.Command{
		Use:   "compare <left-environment> <right-environment>",
		Short: "Compare the effective policy of two environments offline",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := app.Compare(cmd.Context(), svc, app.CompareRequest{
				Dir: dir, Left: args[0], Right: args[1], Format: format, Out: out, FailOnDiff: failOnDiff,
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&format, "format", "console", "output format: console or json")
	f.StringVar(&out, "out", "", "write the comparison to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.BoolVar(&failOnDiff, "fail-on-diff", false, "exit 1 when any effective-policy difference is found")
	return cmd
}

func versionCmd(exit *int) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Foundry Doctor build and compatibility versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"foundry-doctor-azd %s\nconfig: %s\nminimum azd: 1.34.2\nminimum bicep: 0.48.1\n",
				version, app.RepoConfigPath,
			)
			return finish(exit, app.ExitOK, err)
		},
	}
}
