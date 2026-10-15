package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

func annotateCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		dir, profile, format, out, minSev, failOn, baseline, suppress string
		rules                                                         []string
		strict, diff                                                  bool
	)
	cmd := &cobra.Command{
		Use:   "annotate",
		Short: "Create review copies or CI annotations for source findings",
		Long: "Annotate renders line-specific source guidance without mutating the originals.\n" +
			"Review mode writes a .review tree plus annotations-manifest.json; github and sarif\n" +
			"formats write inline-annotation feeds for CI and code-scanning systems.",
		Example: "  foundry-doctor annotate --profile test --out review\\sample.review\n" +
			"  foundry-doctor annotate --format github --min-severity warning --dir samples\\bicep-backed\n" +
			"  foundry-doctor annotate --format sarif --out annotate.sarif --rules FND-CFG-*",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := app.AnnotateRequest{
				Dir: dir, Profile: profile, Rules: rules, Format: format, Out: out,
				Baseline: baseline, Suppress: suppress, Strict: strict, Diff: diff,
			}
			var err error
			if req.MinSeverity, err = optSeverity("--min-severity", minSev); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			if req.FailOn, err = optSeverity("--fail-on", failOn); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			code, err := app.Annotate(cmd.Context(), svc, req, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.StringSliceVar(&rules, "rules", nil, "rule selectors: IDs or globs such as FND-CFG-*; repeatable or comma-separated")
	f.StringVar(&minSev, "min-severity", "", "lowest severity to annotate: info, warning or error (default info)")
	f.StringVar(&failOn, "fail-on", "", "lowest severity that fails the run: info, warning or error (default error)")
	f.StringVar(&format, "format", app.AnnotateFormatDefault, "annotation format: review, github or sarif")
	f.StringVar(&out, "out", "", "review directory (review format) or output file (github/sarif)")
	f.StringVar(&baseline, "baseline", "", "baseline file; hides only findings with matching fingerprints")
	f.StringVar(&suppress, "suppressions", "", "suppressions file (reason, owner and expiry required)")
	f.BoolVar(&strict, "strict", false, "exit 3 when any check was skipped")
	f.BoolVar(&diff, "diff", false, "when using --format review, also write annotations.diff")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return strings.Split("review github sarif", " "), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
