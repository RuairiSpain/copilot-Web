package cost

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	costengine "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Estimate is the priced result of one compiled plan. The rule package defines
// it where it is consumed so the rule does no I/O itself: the estimator behind
// it decides where prices come from (the wiring passes CachedEstimator, which
// reads only the local price cache and never the network).
type Estimate struct {
	Currency      string
	HoursPerMonth float64
	MonthlyLow    float64
	MonthlyHigh   float64
	// PriceRetrieved is the retrieval timestamp of the newest price used, or "".
	PriceRetrieved string
	UsedCache      bool
	Stale          bool
	// Priced is the number of estimated components.
	Priced int
	// Assumptions are deterministic statements the figure depends on.
	Assumptions []string
	// Unpriced lists the components that could not be priced, with the reason.
	Unpriced []Unpriced
}

// Unpriced is one component the estimator could not price.
type Unpriced struct {
	ResourceType string
	ResourceName string
	Reason       string
	// NoPrice is true when the only obstacle is that no retail price is
	// available (for example an empty offline cache), not a modelling gap.
	NoPrice bool
}

// noPricePrefix is the internal/cost reason for a component whose retail price
// could not be resolved (cache missing offline, or no unambiguous price).
const noPricePrefix = "retail price unavailable"

// Estimator prices a compiled plan. It must not use the network.
type Estimator func(ctx context.Context, plan sdk.ARMModel, profile string) (Estimate, error)

// Providers carries the optional collaborators of the COST rules.
type Providers struct {
	// Estimate prices the plan for FND-COST-004. Nil leaves the rule reporting
	// only what makes the estimate incomplete.
	Estimate Estimator
}

// CachedEstimator prices the plan with internal/cost in offline mode from the
// price cache at cachePath ("" selects the default cache path). No price is
// invented: a component without a cached price is returned as Unpriced.
func CachedEstimator(cachePath string) Estimator {
	return func(ctx context.Context, plan sdk.ARMModel, profile string) (Estimate, error) {
		doc, err := costengine.Engine{}.Estimate(ctx, costengine.Request{
			Environments:   []costengine.Environment{{Name: "plan", Profile: profile, ARM: plan}},
			Offline:        true,
			PriceCachePath: cachePath,
		})
		if err != nil {
			return Estimate{}, fmt.Errorf("estimate fixed-capacity cost: %w", err)
		}
		if len(doc.Environments) != 1 {
			return Estimate{}, fmt.Errorf("estimate fixed-capacity cost: expected one environment, got %d", len(doc.Environments))
		}
		env := doc.Environments[0]
		est := Estimate{
			Currency:       env.Currency,
			HoursPerMonth:  env.HoursPerMonth,
			MonthlyLow:     env.TotalLow,
			MonthlyHigh:    env.TotalHigh,
			PriceRetrieved: env.PriceRetrieved,
			UsedCache:      env.UsedCache,
			Stale:          env.StalePricing,
			Priced:         len(env.Estimated),
		}
		est.Assumptions = append(est.Assumptions,
			fmt.Sprintf("%g hours per month", env.HoursPerMonth),
			"public retail list prices from the local price cache, without discounts, reservations, savings plans or taxes",
			"fixed-capacity components only; token consumption is not estimated",
		)
		est.Assumptions = append(est.Assumptions, env.Notes...)
		for _, x := range env.Unsupported {
			est.Unpriced = append(est.Unpriced, Unpriced{
				ResourceType: x.ResourceType, ResourceName: x.ResourceName, Reason: x.Reason,
				NoPrice: strings.HasPrefix(x.Reason, noPricePrefix),
			})
		}
		return est, nil
	}
}

// estimate adds the one plan-level estimate finding and an "incomplete" item
// for each component the estimator could not price and that the extraction
// above did not already report.
func (o *outcome) estimate(ctx context.Context, in *sdk.Input, est Estimator, live []sdk.ARMResource) {
	e, err := est(ctx, in.ARM, in.Profile)
	if err != nil {
		o.skip = sdk.SkipInputUnavailable + ": fixed-capacity price estimate unavailable: " + err.Error()
		return
	}
	reported := map[string]bool{}
	for _, f := range o.findings {
		reported[strings.ToLower(f.Resource.Type+"|"+f.Resource.Name)] = true
	}
	byName := map[string]sdk.ARMResource{}
	for _, r := range live {
		byName[strings.ToLower(r.Type+"|"+armexpr.StripScope(r.Name))] = r
	}
	if e.Priced == 0 {
		noPrice := 0
		for _, u := range e.Unpriced {
			if u.NoPrice {
				noPrice++
			}
		}
		if noPrice > 0 {
			// Nothing could be priced: one skip instead of one item per component.
			o.skip = fmt.Sprintf("price-cache-missing-and-offline: no cached retail price for %d component(s); run `foundry-doctor cost` once with network access to populate the price cache", noPrice)
		}
	}
	for _, u := range e.Unpriced {
		if e.Priced == 0 && u.NoPrice {
			continue
		}
		k := strings.ToLower(u.ResourceType + "|" + armexpr.StripScope(u.ResourceName))
		if reported[k] {
			continue
		}
		r, ok := byName[k]
		if !ok {
			r = sdk.ARMResource{Type: u.ResourceType, Name: u.ResourceName}
		}
		o.addKey(r, "incomplete:unpriced", "fixed-capacity estimate is incomplete because this component could not be priced: "+u.Reason)
		reported[k] = true
	}
	if e.Priced == 0 {
		return // nothing was priced, so there is no figure to report
	}
	o.addKey(planAnchor(live), "estimate", estimateEvidence(e))
}

// planAnchor picks the resource the plan-level estimate is attached to: the
// first Foundry project by name, else the first Foundry account, else the
// first live resource. The fixed-capacity components (Search, API Management,
// Cosmos DB) are shared by the plan, so the figure is per plan, not per project.
func planAnchor(live []sdk.ARMResource) sdk.ARMResource {
	pick := func(typ string) (sdk.ARMResource, bool) {
		var cand []sdk.ARMResource
		for _, r := range live {
			if strings.EqualFold(r.Type, typ) {
				cand = append(cand, r)
			}
		}
		if len(cand) == 0 {
			return sdk.ARMResource{}, false
		}
		slices.SortFunc(cand, func(a, b sdk.ARMResource) int { return strings.Compare(a.Name, b.Name) })
		return cand[0], true
	}
	if r, ok := pick("Microsoft.CognitiveServices/accounts/projects"); ok {
		return r
	}
	if r, ok := pick(typeFoundryAccount); ok {
		return r
	}
	return live[0]
}

const typeFoundryAccount = "Microsoft.CognitiveServices/accounts"

func estimateEvidence(e Estimate) string {
	var b strings.Builder
	if e.MonthlyLow == e.MonthlyHigh {
		fmt.Fprintf(&b, "estimated fixed-capacity cost about %.2f %s per month", e.MonthlyLow, e.Currency)
	} else {
		fmt.Fprintf(&b, "estimated fixed-capacity cost %.2f to %.2f %s per month", e.MonthlyLow, e.MonthlyHigh, e.Currency)
	}
	fmt.Fprintf(&b, " for %d priced component(s)", e.Priced)
	if e.PriceRetrieved != "" {
		date := e.PriceRetrieved
		if i := strings.IndexByte(date, 'T'); i > 0 {
			date = date[:i]
		}
		fmt.Fprintf(&b, "; retail prices retrieved %s", date)
	}
	if e.UsedCache {
		b.WriteString(" (local cache)")
	}
	if e.Stale {
		b.WriteString("; some cached prices are older than 30 days")
	}
	if n := len(e.Unpriced); n > 0 {
		fmt.Fprintf(&b, "; %d component(s) could not be priced and are not in the total", n)
	}
	if len(e.Assumptions) > 0 {
		b.WriteString("; assumptions: " + strings.Join(e.Assumptions, "; "))
	}
	return b.String()
}
