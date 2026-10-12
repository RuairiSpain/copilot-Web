package main

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

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
		Short: "Diagnose deployed runtime state with read-only control-plane and metadata-only data-plane probes",
		Long: "Run the FND-RUN rules against a deployed Foundry project using read-only ARM calls,\n" +
			"metadata-only project/Search queries, and optional VNet DNS resolution. Document bodies,\n" +
			"prompts, completions, and tool outputs are never retrieved. Exit codes match `doctor`.",
		Example: "  foundry-doctor runtime --subscription <id> --resource-group rg-app --account acct --project proj\n" +
			"  foundry-doctor runtime --vantage vnet --timeout 45s --format json --out runtime.json",
		Args: cobra.NoArgs,
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
