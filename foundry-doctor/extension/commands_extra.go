package main

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
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
		Args:  cobra.NoArgs,
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
	return cmd
}

func costCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		dir, profile, environment, currency, priceCache, format, out string
		compare                                                      []string
		hours                                                        float64
		offline                                                      bool
	)
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Estimate fixed-capacity monthly cost from the compiled plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, err := app.Cost(cmd.Context(), svc, app.CostRequest{
				Dir: dir, Profile: profile, Environment: environment, Compare: compare,
				Currency: currency, HoursPerMonth: hours, Offline: offline,
				PriceCache: priceCache, Format: format, Out: out,
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.StringVar(&environment, "environment", "", "azd environment name to resolve before estimating")
	f.StringSliceVar(&compare, "compare", nil, "additional azd environment name(s) to compare against the primary estimate")
	f.StringVar(&currency, "currency", "USD", "ISO currency code for retail lookup (default USD)")
	f.Float64Var(&hours, "hours-per-month", 730, "documented monthly-hours assumption for hourly meters")
	f.BoolVar(&offline, "offline", false, "use only the local price cache; do not call the Retail Prices API")
	f.StringVar(&priceCache, "price-cache", "", "price cache file path (default .foundry-doctor/cache/prices.json)")
	f.StringVar(&format, "format", "console", "report format: console, json or markdown")
	f.StringVar(&out, "out", "", "write the report to this file instead of stdout")
	return cmd
}

func graphCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		dir, format, out, subscription, resourceGroup, account, project string
		redactIDs                                                       bool
		maxNodes, maxEdges, collapseAfter                               int
		findingsReports                                                 []string
		source, deployed, combined                                      bool
	)
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Render source, deployed, or combined Foundry dependency graphs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := view.ModeSource
			count := 0
			if source {
				mode = view.ModeSource
				count++
			}
			if deployed {
				mode = view.ModeDeployed
				count++
			}
			if combined {
				mode = view.ModeCombined
				count++
			}
			if count > 1 {
				return finish(exit, app.ExitUnavailable, app.Usagef("choose at most one of --source, --deployed or --combined"))
			}
			code, err := app.Graph(cmd.Context(), svc, app.GraphRequest{
				Dir: dir, Mode: mode, Format: format, Out: out, RedactIDs: redactIDs,
				MaxNodes: maxNodes, MaxEdges: maxEdges, CollapseAfter: collapseAfter, FindingsReport: findingsReports,
				Target: app.GraphTarget{SubscriptionID: subscription, ResourceGroup: resourceGroup, Account: account, Project: project},
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&source, "source", false, "render the source-only graph (default)")
	f.BoolVar(&deployed, "deployed", false, "render the Azure-deployed graph (read-only inventory)")
	f.BoolVar(&combined, "combined", false, "render the combined source + Azure graph")
	f.StringVar(&format, "format", "mermaid", "graph format: mermaid, json, markdown, dot or html")
	f.StringVar(&out, "out", "", "write the graph to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.StringSliceVar(&findingsReports, "findings-report", nil, "Foundry Doctor JSON report to overlay; repeatable")
	f.StringVar(&subscription, "subscription", "", "subscription ID for --deployed or --combined")
	f.StringVar(&resourceGroup, "resource-group", "", "resource group name for --deployed or --combined")
	f.StringVar(&account, "account", "", "Foundry account name for runtime overlay enrichment")
	f.StringVar(&project, "project", "", "Foundry project name for runtime overlay enrichment")
	f.BoolVar(&redactIDs, "redact-ids", false, "replace friendly identifiers with deterministic obfuscated labels")
	f.IntVar(&maxNodes, "max-nodes", 0, "maximum nodes to emit after collapsing (0 = unlimited)")
	f.IntVar(&maxEdges, "max-edges", 0, "maximum edges to emit after collapsing (0 = unlimited)")
	f.IntVar(&collapseAfter, "collapse-after", 80, "collapse large graphs after this many sorted nodes (0 = never)")
	return cmd
}

func assessCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	cmd := &cobra.Command{Use: "assess", Short: "Render WAF-oriented owner, developer and evidence views"}
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
		Args:  cobra.NoArgs,
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

func preflightCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		profile, minSev, failOn, baseline, suppress, format, out, dir string
		subscription, tenant, location, resourceGroup                 string
		strict, whatIf, preflightAlias                                bool
		rules, approved, allowDelete                                  []string
	)
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check Azure readiness before deploying",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			req := app.PreflightRequest{
				DoctorRequest: app.DoctorRequest{
					Dir: dir, Profile: profile, Rules: rules, Baseline: baseline,
					Suppress: suppress, Strict: strict, Format: format, Out: out,
				},
				Target: app.PreflightTarget{
					TenantID: tenant, SubscriptionID: subscription,
					ResourceGroup: resourceGroup, Location: location,
				},
				WhatIf:         whatIf || preflightAlias,
				ApprovedScopes: approved, AllowedDeletes: allowDelete,
			}
			var err error
			if req.MinSeverity, err = optSeverity("--min-severity", minSev); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			if req.FailOn, err = optSeverity("--fail-on", failOn); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			code, err := app.Preflight(cmd.Context(), svc, req, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.StringSliceVar(&rules, "rules", nil, "rule selectors (default FND-DEP-*)")
	f.StringVar(&minSev, "min-severity", "", "lowest severity to report: info, warning or error (default info)")
	f.StringVar(&failOn, "fail-on", "", "lowest severity that fails the run (default error)")
	f.StringVar(&baseline, "baseline", "", "baseline file; hides only findings with matching fingerprints")
	f.StringVar(&suppress, "suppressions", "", "suppressions file (reason, owner and expiry required)")
	f.BoolVar(&strict, "strict", false, "exit 3 when any check was skipped")
	f.StringVar(&format, "format", "console", "report format: "+strings.Join(app.Formats, ", "))
	f.StringVar(&out, "out", "", "write the report to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.StringVar(&subscription, "subscription", "", "subscription ID (default AZURE_SUBSCRIPTION_ID)")
	f.StringVar(&tenant, "tenant", "", "tenant ID (default AZURE_TENANT_ID)")
	f.StringVar(&location, "location", "", "deployment location (default AZURE_LOCATION)")
	f.StringVar(&resourceGroup, "resource-group", "", "target resource group (default AZURE_RESOURCE_GROUP)")
	f.BoolVar(&whatIf, "what-if", false, "opt in to the ARM what-if operation (FND-DEP-008)")
	f.BoolVar(&preflightAlias, "preflight", false, "alias of --what-if")
	f.StringSliceVar(&approved, "approved-scope", nil, "extra scope (resource ID) that cross-resource-group references may target; repeatable")
	f.StringSliceVar(&allowDelete, "allow-delete", nil, "resource ID whose predicted deletion is accepted; repeatable")
	return cmd
}

func runtimeCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		profile, minSev, failOn, baseline, suppress, format, out, dir string
		subscription, tenant, resourceGroup, account, project         string
		vantage                                                       string
		timeout                                                       time.Duration
		strict                                                        bool
		rules                                                         []string
	)
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Diagnose deployed runtime state with read-only probes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := parseVantage(vantage)
			if err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			req := app.RuntimeRequest{
				DoctorRequest: app.DoctorRequest{
					Dir: dir, Profile: profile, Rules: rules, Baseline: baseline,
					Suppress: suppress, Strict: strict, Format: format, Out: out,
				},
				Target: app.RuntimeTarget{
					TenantID:       tenant,
					SubscriptionID: subscription,
					ResourceGroup:  resourceGroup,
					Account:        account,
					Project:        project,
				},
				Vantage: v,
				Timeout: timeout,
			}
			if req.Timeout <= 0 {
				req.Timeout = 30 * time.Second
			}
			if req.MinSeverity, err = optSeverity("--min-severity", minSev); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			if req.FailOn, err = optSeverity("--fail-on", failOn); err != nil {
				return finish(exit, app.ExitCodeForError(err), err)
			}
			code, err := app.Runtime(cmd.Context(), svc, req, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.StringVar(&profile, "profile", "", "curated profile: dev, test or prod (default from config)")
	f.StringSliceVar(&rules, "rules", nil, "rule selectors (default FND-RUN-*)")
	f.StringVar(&minSev, "min-severity", "", "lowest severity to report: info, warning or error (default info)")
	f.StringVar(&failOn, "fail-on", "", "lowest severity that fails the run (default error)")
	f.StringVar(&baseline, "baseline", "", "baseline file; hides only findings with matching fingerprints")
	f.StringVar(&suppress, "suppressions", "", "suppressions file (reason, owner and expiry required)")
	f.BoolVar(&strict, "strict", false, "exit 3 when any check was skipped")
	f.StringVar(&format, "format", "console", "report format: "+strings.Join(app.Formats, ", "))
	f.StringVar(&out, "out", "", "write the report to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.StringVar(&subscription, "subscription", "", "subscription ID (default AZURE_SUBSCRIPTION_ID)")
	f.StringVar(&tenant, "tenant", "", "tenant ID (reserved for future use)")
	f.StringVar(&resourceGroup, "resource-group", "", "target resource group (default AZURE_RESOURCE_GROUP)")
	f.StringVar(&account, "account", "", "target Foundry account name")
	f.StringVar(&project, "project", "", "target Foundry project name")
	f.StringVar(&vantage, "vantage", string(runtime.VantageNone), "network vantage: none, local or vnet")
	f.DurationVar(&timeout, "timeout", 30*time.Second, "per-probe timeout")
	return cmd
}

func parseVantage(v string) (runtime.Vantage, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", string(runtime.VantageNone):
		return runtime.VantageNone, nil
	case string(runtime.VantageLocal):
		return runtime.VantageLocal, nil
	case string(runtime.VantageVNet):
		return runtime.VantageVNet, nil
	default:
		return "", app.Usagef("invalid --vantage %q (want none, local or vnet)", v)
	}
}
