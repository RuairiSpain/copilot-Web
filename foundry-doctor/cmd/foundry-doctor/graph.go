package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func graphCmd(_ context.Context, svc app.Services, exit *int) *cobra.Command {
	var (
		dir, format, out, subscription, resourceGroup, account, project string
		diffBase, diffTarget                                            string
		redactIDs                                                       bool
		maxNodes, maxEdges, collapseAfter                               int
		findingsReports                                                 []string
		source, deployed, combined                                      bool
	)
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Render source, deployed, combined or environment-difference Foundry dependency graphs",
		Long: "Graph renders the Phase 1-3 correlation graph without changing validation semantics.\n" +
			"Source mode works offline. Deployed and combined modes use read-only Azure inventory.\n" +
			"Optional findings reports add unhealthy and baselined overlays to matching nodes.\n" +
			"--diff-base and --diff-target compare two graph JSON files (for example two environments).",
		Example: "  foundry-doctor graph --source --dir samples\\good --format mermaid\n" +
			"  foundry-doctor graph --combined --subscription <id> --resource-group rg-app --account acct --format markdown --out graph.md\n" +
			"  foundry-doctor graph --source --findings-report foundry.json --redact-ids --format json\n" +
			"  foundry-doctor graph --diff-base dev.json --diff-target prod.json --format markdown",
		Args: cobra.NoArgs,
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
			if diffBase != "" || diffTarget != "" {
				if count > 0 {
					return finish(exit, app.ExitUnavailable, app.Usagef("--diff-base/--diff-target cannot be combined with --source, --deployed or --combined"))
				}
				mode = view.ModeDiff
			}
			code, err := app.Graph(cmd.Context(), svc, app.GraphRequest{
				Dir: dir, Mode: mode, Format: format, Out: out, RedactIDs: redactIDs,
				MaxNodes: maxNodes, MaxEdges: maxEdges, CollapseAfter: collapseAfter, FindingsReport: findingsReports,
				DiffBase: diffBase, DiffTarget: diffTarget,
				Target: app.GraphTarget{SubscriptionID: subscription, ResourceGroup: resourceGroup, Account: account, Project: project},
			}, cmd.OutOrStdout())
			return finish(exit, code, err)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&source, "source", false, "render the source-only graph (default)")
	f.BoolVar(&deployed, "deployed", false, "render the Azure-deployed graph (read-only inventory)")
	f.BoolVar(&combined, "combined", false, "render the combined source + Azure graph")
	f.StringVar(&format, "format", "mermaid", "graph format: mermaid, json, markdown, dot, html, or svg/png (optional Graphviz)")
	f.StringVar(&out, "out", "", "write the graph to this file instead of stdout")
	f.StringVar(&dir, "dir", ".", "project directory containing azure.yaml")
	f.StringSliceVar(&findingsReports, "findings-report", nil, "Foundry Doctor JSON report to overlay; repeatable")
	f.StringVar(&subscription, "subscription", "", "subscription ID for --deployed or --combined")
	f.StringVar(&resourceGroup, "resource-group", "", "resource group name for --deployed or --combined")
	f.StringVar(&account, "account", "", "Foundry account name for runtime overlay enrichment")
	f.StringVar(&project, "project", "", "Foundry project name for runtime overlay enrichment")
	f.StringVar(&diffBase, "diff-base", "", "graph JSON file (from --format json) of the base environment for an environment-difference graph")
	f.StringVar(&diffTarget, "diff-target", "", "graph JSON file (from --format json) of the target environment for an environment-difference graph")
	f.BoolVar(&redactIDs, "redact-ids", false, "replace friendly identifiers with deterministic obfuscated labels")
	f.IntVar(&maxNodes, "max-nodes", 0, "soft limit on kept nodes; collapse buckets are added on top (0 = unlimited)")
	f.IntVar(&maxEdges, "max-edges", 0, "maximum edges to emit after collapsing (0 = unlimited)")
	f.IntVar(&collapseAfter, "collapse-after", 80, "collapse large graphs after this many sorted nodes (0 = never)")
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return strings.Split("mermaid json markdown dot html svg png", " "), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
