package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

func preflightCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		profile, minSev, failOn, baseline, suppress, format, out, dir string
		subscription, tenant, location, resourceGroup                 string
		strict, whatIf, preflightAlias                                bool
		rules, approved, allowDelete                                  []string
	)
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Check Azure readiness (permissions, quota, names, policy) before deploying",
		Long: "Run the FND-DEP rules against live Azure state using read-only calls.\n\n" +
			"The report ends with a readiness summary (ready / blocked / uncertain / skipped).\n" +
			"Readiness is advisory and never guarantees that a deployment will succeed.\n" +
			"A check that lacks a permission is skipped and names the missing capability.\n" +
			"The ARM what-if operation (FND-DEP-008) runs only with --what-if (alias --preflight).\n" +
			"Nothing is created, changed or deleted. Exit codes match `doctor`.",
		Example: "  foundry-doctor preflight --subscription <id> --resource-group rg-app --location swedencentral\n" +
			"  foundry-doctor preflight --what-if --profile prod --strict --format json --out preflight.json",
		Args: cobra.NoArgs,
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
