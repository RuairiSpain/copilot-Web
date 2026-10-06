package main

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
)

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
		Long: "Build an advisory fixed-capacity estimate from the compiled ARM plan using public Azure retail prices.\n" +
			"It never estimates token consumption, discounts, taxes, or negotiated pricing.",
		Example: "  foundry-doctor cost --environment prod --compare dev\n" +
			"  foundry-doctor cost --offline --price-cache .foundry-doctor/cache/prices.json --format json",
		Args: cobra.NoArgs,
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
	_ = cmd.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return strings.Split("console,json,markdown", ","), cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}
