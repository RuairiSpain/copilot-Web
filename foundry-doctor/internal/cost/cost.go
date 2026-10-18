package cost

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/armcost"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/prices"
	costreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	DefaultHoursPerMonth = 730
	defaultPriceMaxAge   = 30 * 24 * time.Hour

	typeSearch         = "Microsoft.Search/searchServices"
	typeAPIM           = "Microsoft.ApiManagement/service"
	typeDeployment     = "Microsoft.CognitiveServices/accounts/deployments"
	typeFoundryAccount = "Microsoft.CognitiveServices/accounts"

	// Advisory is the document-level disclaimer.
	Advisory = "Retail-price estimates are planning aids only. Totals cover the estimated lines only (covered resources only); " +
		"they exclude token consumption, discounts, reservations, savings plans, taxes, negotiated pricing, and every excluded or " +
		"unsupported resource listed below. Invoices and Azure meter records remain the source of truth."
	nonUSDNote = "non-USD retail prices are reference values; Microsoft prices Azure services in USD (Azure Retail Prices API overview)"
	rangeNote  = "low and high are equal: every estimated line is a fixed hourly meter; autoscale and PTU, where a range would apply, are listed as unsupported"
)

type Environment struct {
	Name    string
	Profile string
	ARM     sdk.ARMModel
}

type Request struct {
	Environments   []Environment
	Currency       string
	HoursPerMonth  float64
	Offline        bool
	PriceCachePath string
	BaseURL        string
	HTTPClient     *http.Client
	Now            func() time.Time
}

type Engine struct{}

func (Engine) Estimate(ctx context.Context, req Request) (costreport.Document, error) {
	if len(req.Environments) == 0 {
		return costreport.Document{}, fmt.Errorf("no environments to estimate")
	}
	now := time.Now().UTC
	if req.Now != nil {
		now = req.Now
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "USD"
	}
	hours := req.HoursPerMonth
	if hours <= 0 {
		hours = DefaultHoursPerMonth
	}
	cachePath := req.PriceCachePath
	if cachePath == "" {
		cachePath = DefaultCachePath()
	}
	client := prices.Client{BaseURL: req.BaseURL, HTTP: req.HTTPClient, Now: now}
	doc := costreport.Document{Advisory: Advisory}
	for _, env := range req.Environments {
		rep := estimateEnv(ctx, client, cachePath, currency, hours, req.Offline, now(), env)
		doc.Environments = append(doc.Environments, rep)
	}
	if len(doc.Environments) > 1 {
		base := doc.Environments[0]
		for _, other := range doc.Environments[1:] {
			cmp := costreport.Comparison{Left: base.Environment, Right: other.Environment}
			if base.Incomplete || other.Incomplete {
				cmp.Reason = "comparison is incomplete because one or both environments have unsupported or unavailable pricing"
			} else {
				cmp.Comparable = true
				cmp.DeltaLow = round2(other.TotalLow - base.TotalLow)
				cmp.DeltaHigh = round2(other.TotalHigh - base.TotalHigh)
				cmp.Reason = "covered resources only; excluded resources are not compared"
			}
			doc.Comparisons = append(doc.Comparisons, cmp)
		}
	}
	return doc, nil
}

func estimateEnv(ctx context.Context, client prices.Client, cachePath, currency string, hours float64, offline bool, now time.Time, env Environment) costreport.EnvironmentReport {
	out := costreport.EnvironmentReport{
		Environment:   env.Name,
		Profile:       env.Profile,
		Currency:      currency,
		HoursPerMonth: hours,
	}
	drivers, excluded, unsupported, notes := extract(env.ARM)
	out.Excluded = excluded
	out.Unsupported = unsupported
	out.Notes = append(out.Notes, notes...)
	if currency != "USD" {
		out.Notes = append(out.Notes, nonUSDNote)
	}
	var stale bool
	for _, d := range drivers {
		if armcost.SovereignRegion(d.Region) {
			out.Unsupported = append(out.Unsupported, driverNote(d, "region "+d.Region+" is in a sovereign cloud; the Retail Prices API covers the Commercial cloud only"))
			continue
		}
		spec, ok := meterFor(d, currency)
		if !ok {
			out.Unsupported = append(out.Unsupported, driverNote(d, fmt.Sprintf("sku %q has no mapping in the supported-meter catalogue", d.SKUName)))
			continue
		}
		rec, usedCache, err := prices.Resolve(ctx, client, cachePath, offline, spec.Query)
		if err != nil {
			out.Unsupported = append(out.Unsupported, driverNote(d, "retail price unavailable (no price guessed): "+err.Error()))
			continue
		}
		out.UsedCache = out.UsedCache || usedCache
		if rec.Notice != "" {
			out.Notes = append(out.Notes, d.Name+": "+rec.Notice)
		}
		if out.PriceRetrieved == "" || rec.RetrievedAt.Format(time.RFC3339) > out.PriceRetrieved {
			out.PriceRetrieved = rec.RetrievedAt.Format(time.RFC3339)
		}
		if prices.Stale(rec, now, defaultPriceMaxAge) {
			stale = true
		}
		lineNotes := slices.Clone(d.Notes)
		if spec.Verified == "" {
			lineNotes = append(lineNotes, "meter name follows the retail naming pattern only (UNVERIFIED)")
		}
		if !rec.PrimaryMeterRegion {
			lineNotes = append(lineNotes, "the retail row is not marked as the primary meter region; its list price is shown")
		}
		quantity := d.Quantity / spec.QuantityDivisor
		monthly := round2(quantity * rec.UnitPrice * hours)
		out.Estimated = append(out.Estimated, costreport.LineItem{
			ResourceName: d.Name,
			ResourceType: d.Type,
			Kind:         string(d.Kind),
			Region:       d.Region,
			MeterName:    rec.MeterName,
			SKUName:      d.SKUName,
			Quantity:     quantity,
			Unit:         unitLabel(d, spec),
			UnitPrice:    rec.UnitPrice,
			MonthlyLow:   monthly,
			MonthlyHigh:  monthly,
			PriceDate:    rec.EffectiveStartDate,
			Notes:        lineNotes,
		})
		out.TotalLow = round2(out.TotalLow + monthly)
		out.TotalHigh = round2(out.TotalHigh + monthly)
	}
	out.StalePricing = stale
	if stale {
		out.Incomplete = true
		out.Notes = append(out.Notes, "one or more cached retail prices are older than 30 days")
	}
	if len(out.Unsupported) > 0 {
		out.Incomplete = true
	}
	if len(out.Estimated) > 0 {
		out.Notes = append(out.Notes, rangeNote)
	}
	if len(out.Estimated) == 0 && len(out.Unsupported) == 0 {
		out.Notes = append(out.Notes, "no supported fixed-capacity resources were found in the compiled plan")
	}
	return out
}

type driver struct {
	Kind     catalog.DriverKind
	Name     string
	Type     string
	Region   string
	SKUName  string
	Quantity float64
	Notes    []string
}

func driverNote(d driver, reason string) costreport.NoteItem {
	return costreport.NoteItem{ResourceName: d.Name, ResourceType: d.Type, Reason: reason}
}

// extraction collects the classification of every resource in one plan.
type extraction struct {
	all         []sdk.ARMResource
	drivers     []driver
	excluded    []costreport.NoteItem
	unsupported []costreport.NoteItem
}

func (x *extraction) exclude(r sdk.ARMResource, reason string) {
	x.excluded = append(x.excluded, note(r, reason))
}

func (x *extraction) unsupport(r sdk.ARMResource, reason string) {
	x.unsupported = append(x.unsupported, note(r, reason))
}

// extract classifies every resource of the plan: an estimated driver, an
// excluded resource (consumption-based or not modelled), or an unsupported
// resource (fixed cost that cannot be priced). A resource whose condition is
// the literal false is not deployed and is skipped.
func extract(m sdk.ARMModel) (drivers []driver, excluded []costreport.NoteItem, unsupported []costreport.NoteItem, notes []string) {
	if m == nil {
		notes = append(notes, "compiled ARM template unavailable; estimate is incomplete")
		return nil, nil, []costreport.NoteItem{{ResourceName: "(compiled plan)", Reason: "compiled ARM template unavailable"}}, notes
	}
	x := &extraction{all: m.Resources()}
	for _, r := range x.all {
		switch armcost.PresenceOf(r) {
		case armcost.NotDeployed:
			continue
		case armcost.Uncertain:
			if costRelevant(r) {
				x.unsupport(r, "conditional or looped deployment unresolved: a non-literal condition or a copy loop's instance count is not evaluated")
			} else {
				x.exclude(r, "resource type not modelled by the fixed-capacity estimator (conditional or looped); any cost it has is not included")
			}
			continue
		}
		switch {
		case strings.EqualFold(r.Type, typeSearch):
			x.search(r)
		case strings.EqualFold(r.Type, typeAPIM):
			x.apim(r)
		case armcost.IsCosmosThroughputType(r.Type):
			x.cosmos(r)
		case armcost.IsCosmosChildWithOptions(r):
			x.unsupport(r, "Cosmos DB API type not modelled; any provisioned throughput in properties.options is not priced")
		case strings.EqualFold(r.Type, armcost.TypeCosmosAccount):
			if a := armcost.ReadCosmosAccount(r); a.Serverless {
				x.exclude(r, "serverless Cosmos DB account is request-based and excluded")
			} else {
				x.exclude(r, "account-level cost (storage, backup, bandwidth) is not modelled; provisioned throughput is estimated on its databases, containers and throughputSettings")
			}
		case strings.EqualFold(r.Type, typeDeployment):
			x.deployment(r)
		default:
			x.exclude(r, "resource type not modelled by the fixed-capacity estimator; any cost it has is not included")
		}
	}
	return x.drivers, x.excluded, x.unsupported, notes
}

// costRelevant reports whether the estimator models the resource type.
func costRelevant(r sdk.ARMResource) bool {
	return strings.EqualFold(r.Type, typeSearch) || strings.EqualFold(r.Type, typeAPIM) ||
		strings.EqualFold(r.Type, typeDeployment) || armcost.IsCosmosThroughputType(r.Type) ||
		armcost.IsCosmosChildWithOptions(r)
}

func (x *extraction) search(r sdk.ARMResource) {
	sku, unresolved := armcost.SKUName(r)
	switch {
	case unresolved:
		x.unsupport(r, "Search sku.name is unresolved")
		return
	case sku == "":
		x.unsupport(r, "Search sku.name is missing")
		return
	case strings.EqualFold(sku, "free") || strings.EqualFold(sku, "serverless"):
		x.exclude(r, "free or serverless Search tiers are excluded from fixed-capacity estimation")
		return
	}
	region, ok := armcost.LiteralRegion(r.Region)
	if !ok {
		x.unsupport(r, "Search region is unresolved")
		return
	}
	switch v := r.Properties["computeType"].(type) {
	case nil:
	case string:
		if armcost.IsExpr(v) {
			x.unsupport(r, "Search computeType is unresolved")
			return
		}
		if strings.EqualFold(v, "Confidential") {
			x.unsupport(r, "Search confidential compute bills on separate CC meters, which are not mapped")
			return
		}
	default:
		x.unsupport(r, "Search computeType is unresolved")
		return
	}
	switch v := r.Properties["hostingMode"].(type) {
	case nil:
	case string:
		if armcost.IsExpr(v) {
			x.unsupport(r, "Search hostingMode is unresolved")
			return
		}
		if strings.EqualFold(v, "HighDensity") {
			x.unsupport(r, "Search HighDensity hosting mode pricing is not mapped")
			return
		}
	default:
		x.unsupport(r, "Search hostingMode is unresolved")
		return
	}
	replicas, why := searchCount(r, "replicaCount")
	if why != "" {
		x.unsupport(r, why)
		return
	}
	partitions, why := searchCount(r, "partitionCount")
	if why != "" {
		x.unsupport(r, why)
		return
	}
	x.drivers = append(x.drivers, driver{
		Kind:     catalog.DriverSearchUnits,
		Name:     r.Name,
		Type:     r.Type,
		Region:   region,
		SKUName:  sku,
		Quantity: replicas * partitions,
		Notes:    []string{"quantity = replicaCount x partitionCount (search units)"},
	})
}

// searchCount reads a positive count. The specification gives no default
// for an absent count, so absence is reported, never assumed to be 1.
func searchCount(r sdk.ARMResource, key string) (float64, string) {
	v, present := r.Properties[key]
	if !present || v == nil {
		return 0, "Search " + key + " is not set in the compiled plan; the service default is not verified, so the unit count is unknown"
	}
	n, ok := armcost.Number(v)
	if !ok {
		return 0, "Search " + key + " is unresolved"
	}
	if n <= 0 {
		return 0, "Search " + key + " is zero or negative"
	}
	return n, ""
}

func (x *extraction) apim(r sdk.ARMResource) {
	sku, unresolved := armcost.SKUName(r)
	switch {
	case unresolved:
		x.unsupport(r, "API Management sku.name is unresolved")
		return
	case sku == "":
		x.unsupport(r, "API Management sku.name is missing")
		return
	case strings.EqualFold(sku, "Consumption"):
		x.exclude(r, "API Management Consumption is request-based and excluded")
		return
	}
	region, ok := armcost.LiteralRegion(r.Region)
	if !ok {
		x.unsupport(r, "API Management region is unresolved")
		return
	}
	capacity, ok, _ := armcost.SKUCapacity(r)
	if !ok {
		x.unsupport(r, "API Management sku.capacity is missing or unresolved")
		return
	}
	switch locs := r.Properties["additionalLocations"].(type) {
	case nil:
	case []any:
		for _, l := range locs {
			m, _ := l.(map[string]any)
			loc, _ := m["location"].(string)
			if loc == "" || armcost.IsExpr(loc) {
				loc = "(unresolved region)"
			}
			units := "an unresolved number of"
			if sku, _ := m["sku"].(map[string]any); sku != nil {
				if n, ok := armcost.Number(sku["capacity"]); ok {
					units = strconv.FormatFloat(n, 'f', -1, 64)
				}
			}
			x.unsupport(r, fmt.Sprintf("additionalLocations: %s units in %s are billed in that region and are not priced", units, loc))
		}
	default:
		x.unsupport(r, "API Management additionalLocations is unresolved; units in additional regions are not priced")
	}
	x.drivers = append(x.drivers, driver{
		Kind:     catalog.DriverAPIMUnits,
		Name:     r.Name,
		Type:     r.Type,
		Region:   region,
		SKUName:  sku,
		Quantity: capacity,
		Notes:    []string{"quantity = sku.capacity (primary region units only)"},
	})
}

func (x *extraction) cosmos(r sdk.ARMResource) {
	t := armcost.CosmosThroughput(r)
	if t.None() {
		x.exclude(r, "no dedicated provisioned throughput is set on this resource")
		return
	}
	if t.Unresolved {
		x.unsupport(r, "Cosmos DB throughput is unresolved")
		return
	}
	acct, ok := armcost.FindParent(x.all, r, armcost.TypeCosmosAccount)
	if !ok {
		x.unsupport(r, "parent Cosmos DB account is not in the compiled plan; region and region count are unresolved")
		return
	}
	a := armcost.ReadCosmosAccount(acct)
	switch {
	case a.UnresolvedReason != "":
		x.unsupport(r, "Cosmos DB account "+a.UnresolvedReason+"; region and region count are unresolved")
		return
	case a.Serverless:
		x.exclude(r, "serverless Cosmos DB account is request-based and excluded")
		return
	case a.MultiRegionWrite:
		x.unsupport(r, "multi-region write account: RU/s bill at the multi-region-write rate, which is not mapped")
		return
	case a.FreeTier:
		x.unsupport(r, "free-tier account: the free-tier RU/s discount is not modelled")
		return
	case t.HasAutoscale:
		x.unsupport(r, "autoscale throughput (maxThroughput="+strconv.FormatFloat(t.AutoscaleMax, 'f', -1, 64)+
			") bills 10-100% of the maximum each hour at the autoscale rate, which is not mapped")
		return
	}
	for _, reg := range a.Regions {
		if reg.ZoneRedundant {
			x.unsupport(r, "zone-redundant region "+reg.Name+": the availability-zone multiplier is not modelled")
			return
		}
	}
	for _, reg := range a.Regions {
		x.drivers = append(x.drivers, driver{
			Kind:     catalog.DriverCosmosProvisioned,
			Name:     r.Name,
			Type:     r.Type,
			Region:   reg.Name,
			SKUName:  "RUs",
			Quantity: t.Manual,
			Notes: []string{
				"quantity = provisioned RU/s / 100 (per-100 RU/s hourly meter)",
				fmt.Sprintf("region taken from parent account %s, which has %d region(s); RU/s are billed in every account region (one line per region)", acct.Name, len(a.Regions)),
			},
		})
	}
}

func (x *extraction) deployment(r sdk.ARMResource) {
	sku, unresolved := armcost.SKUName(r)
	if unresolved || sku == "" {
		x.unsupport(r, "unknown or unresolved deployment SKU: sku.name is missing or unresolved, so the billing model is unknown")
		return
	}
	d, ok := armcost.ClassifyDeploymentSKU(sku)
	if !ok {
		x.unsupport(r, fmt.Sprintf("unknown or unresolved deployment SKU: %q is not a documented deployment type", sku))
		return
	}
	switch d.Class {
	case armcost.DeploymentPayPerToken:
		x.exclude(r, "token-based deployment type "+d.Code+" (pay-per-token) is excluded; sku.capacity is a throughput allocation, not a billed quantity")
	case armcost.DeploymentBatch:
		x.exclude(r, "token-based deployment type "+d.Code+" (batch, billed per token at a discount) is excluded")
	case armcost.DeploymentDeveloper:
		x.exclude(r, "token-based deployment type "+d.Code+" (fine-tuned model evaluation) is excluded")
	case armcost.DeploymentProvisioned:
		ptu := "PTU count unresolved"
		if n, ok, unres := armcost.SKUCapacity(r); ok {
			ptu = strconv.FormatFloat(n, 'f', -1, 64) + " PTU"
		} else if !unres {
			ptu = "PTU count not set"
		}
		region := "region unresolved"
		if acct, ok := armcost.FindParent(x.all, r, typeFoundryAccount); ok {
			if reg, ok := armcost.LiteralRegion(acct.Region); ok {
				region = "region " + reg
			}
		}
		x.unsupport(r, fmt.Sprintf("provisioned throughput %s, deployment type %s (%s), %s: PTUs bill hourly per deployed PTU, but the PTU retail meter mapping is UNVERIFIED; reservations are not modelled", ptu, d.Code, d.Scope, region))
	}
}

func note(r sdk.ARMResource, reason string) costreport.NoteItem {
	return costreport.NoteItem{ResourceName: r.Name, ResourceType: r.Type, Reason: reason}
}

func unitLabel(d driver, spec catalog.MeterSpec) string {
	if d.Kind == catalog.DriverCosmosProvisioned && spec.QuantityDivisor == 100 {
		return "x100 RU/s"
	}
	return "unit"
}

func meterFor(d driver, currency string) (catalog.MeterSpec, bool) {
	switch d.Kind {
	case catalog.DriverSearchUnits:
		return catalog.Search(d.Region, currency, d.SKUName)
	case catalog.DriverAPIMUnits:
		return catalog.APIM(d.Region, currency, d.SKUName)
	case catalog.DriverCosmosProvisioned:
		return catalog.CosmosProvisioned(d.Region, currency), true
	default:
		return catalog.MeterSpec{}, false
	}
}

// round2 rounds each line to two decimals; totals are the sum of rounded
// lines (documented in docs/cost.md).
func round2(v float64) float64 {
	s := fmt.Sprintf("%.2f", v)
	out, _ := strconv.ParseFloat(s, 64)
	return out
}

// DefaultCachePath returns the built-in cache path, relative to the working
// directory. The CLI passes a path under --dir instead.
func DefaultCachePath() string { return filepath.Join(".foundry-doctor", "cache", "prices.json") }

// EnsureCacheDir creates the built-in cache directory when needed.
func EnsureCacheDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
