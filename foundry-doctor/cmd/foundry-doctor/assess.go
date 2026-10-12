package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

func assessCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assess",
		Short: "Render WAF-oriented owner, developer and evidence views from Foundry Doctor findings",
	}
	cmd.AddCommand(assessWAFCmd(svc, exit))
	return cmd
}

func assessWAFCmd(svc app.Services, exit *int) *cobra.Command {
	var (
		profile, baseline, suppress, audience, format, out, dir, llmProvider string
		strict, evidencePack, llmExplain                                     bool
		llmTimeout                                                           time.Duration
	)
	cmd := &cobra.Command{
		Use:   "waf",
		Short: "Aggregate WAF-aligned controls into owner, developer or evidence reports",
		Long: "Map selected Foundry Doctor rules to WAF-aligned controls for three audiences.\n" +
			"The assessment is scoped to available evidence and never claims complete WAF compliance.\n" +
			"Controls that need business or operational context remain QUESTION or UNKNOWN.",
		Example: "  foundry-doctor assess waf --profile prod\n" +
			"  foundry-doctor assess waf --audience developer --format html --out assess.html\n" +
			"  foundry-doctor assess waf --evidence-pack --format json --out evidence.json\n" +
			"  foundry-doctor assess waf --llm-explain --audience owner",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, err := app.AssessWAF(cmd.Context(), svc, app.AssessRequest{
				Dir: dir, Profile: profile, Baseline: baseline, Suppress: suppress,
				Strict: strict, Audience: app.AssessAudience(audience), Format: format, Out: out, EvidencePack: evidencePack,
				LLMExplain: llmExplain, LLMProvider: llmProvider, LLMTimeout: llmTimeout,
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.StringVar(&baseline, "baseline", "", "baseline file; accepted debt remains visible in the evidence pack")
	f.StringVar(&suppress, "suppressions", "", "suppressions file (reason, owner and expiry required)")
	f.BoolVar(&strict, "strict", false, "exit 3 when any assessed control is skipped")
	f.StringVar(&audience, "audience", string(app.AssessAudienceOwner), "report audience: owner, developer or evidence")
	f.StringVar(&format, "format", "markdown", "report format: markdown, html or json (json only for evidence)")
	f.BoolVar(&evidencePack, "evidence-pack", false, "render the evidence-pack view (same as --audience evidence)")
	f.BoolVar(&llmExplain, "llm-explain", false, "append advisory generated narrative for owner/developer audiences (explicit opt-in)")
	f.StringVar(&llmProvider, "llm-provider", "", "advisory provider override (allow-listed; default azure-openai)")
	f.DurationVar(&llmTimeout, "llm-timeout", 0, "advisory provider timeout when --llm-explain is set (1s to 30s)")
	f.StringVar(&out, "out", "", "write the rendered report to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	return cmd
}
