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

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/prices"
	costreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	DefaultHoursPerMonth = 730
	defaultPriceMaxAge   = 30 * 24 * time.Hour
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
		cachePath = filepath.Join(".foundry-doctor", "cache", "prices.json")
	}
	client := prices.Client{BaseURL: req.BaseURL, HTTP: req.HTTPClient, Now: now}
	doc := costreport.Document{
		Advisory: "Retail-price estimates are planning aids only. They exclude token consumption, discounts, taxes, negotiated pricing, and unsupported meters; invoices and Azure meter records remain the source of truth.",
	}
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
	var stale bool
	for _, d := range drivers {
		spec, ok := meterFor(d, currency)
		if !ok {
			out.Unsupported = append(out.Unsupported, costreport.NoteItem{
				ResourceName: d.Name,
				ResourceType: d.Type,
				Reason:       d.UnsupportedReason,
			})
			continue
		}
		rec, usedCache, err := prices.Resolve(ctx, client, cachePath, offline, spec.Query)
		if err != nil {
			out.Incomplete = true
			out.Unsupported = append(out.Unsupported, costreport.NoteItem{
				ResourceName: d.Name,
				ResourceType: d.Type,
				Reason:       "UNVERIFIED retail price unavailable: " + err.Error(),
			})
			continue
		}
		out.UsedCache = out.UsedCache || usedCache
		if out.PriceRetrieved == "" || rec.RetrievedAt.Format(time.RFC3339) > out.PriceRetrieved {
			out.PriceRetrieved = rec.RetrievedAt.Format(time.RFC3339)
		}
		if prices.Stale(rec, now, defaultPriceMaxAge) {
			stale = true
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
			Notes:        slices.Clone(d.Notes),
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
	if len(out.Estimated) == 0 && len(out.Unsupported) == 0 {
		out.Notes = append(out.Notes, "no supported fixed-capacity resources were found in the compiled plan")
	}
	return out
}

type driver struct {
	Kind              catalog.DriverKind
	Name              string
	Type              string
	Region            string
	SKUName           string
	Quantity          float64
	UnsupportedReason string
	Notes             []string
}

func extract(m sdk.ARMModel) (drivers []driver, excluded []costreport.NoteItem, unsupported []costreport.NoteItem, notes []string) {
	if m == nil {
		notes = append(notes, "compiled ARM template unavailable; estimate is incomplete")
		return nil, nil, nil, notes
	}
	for _, r := range m.Resources() {
		switch {
		case strings.EqualFold(r.Type, "Microsoft.Search/searchServices"):
			region, ok := literalRegion(r.Region)
			if !ok {
				unsupported = append(unsupported, note(r, "search region is unresolved"))
				continue
			}
			replicas, rok := intProp(r.Properties, "replicaCount")
			partitions, pok := intProp(r.Properties, "partitionCount")
			if !rok || !pok {
				unsupported = append(unsupported, note(r, "search replicaCount or partitionCount is unresolved"))
				continue
			}
			if replicas <= 0 || partitions <= 0 {
				unsupported = append(unsupported, note(r, "search replicaCount or partitionCount is missing or zero"))
				continue
			}
			if strings.EqualFold(r.SKUName, "free") || strings.EqualFold(r.SKUName, "serverless") {
				excluded = append(excluded, note(r, "free or serverless Search tiers are excluded from fixed-capacity estimation"))
				continue
			}
			drivers = append(drivers, driver{
				Kind:     catalog.DriverSearchUnits,
				Name:     r.Name,
				Type:     r.Type,
				Region:   region,
				SKUName:  r.SKUName,
				Quantity: float64(replicas * partitions),
				Notes:    []string{"quantity = replicaCount x partitionCount"},
			})
		case strings.EqualFold(r.Type, "Microsoft.ApiManagement/service"):
			region, ok := literalRegion(r.Region)
			if !ok {
				unsupported = append(unsupported, note(r, "API Management region is unresolved"))
				continue
			}
			capacity, ok := skuCapacity(r)
			if !ok {
				unsupported = append(unsupported, note(r, "API Management sku.capacity is unresolved"))
				continue
			}
			if strings.EqualFold(r.SKUName, "Consumption") {
				excluded = append(excluded, note(r, "API Management Consumption is request-based and excluded"))
				continue
			}
			drivers = append(drivers, driver{
				Kind:     catalog.DriverAPIMUnits,
				Name:     r.Name,
				Type:     r.Type,
				Region:   region,
				SKUName:  r.SKUName,
				Quantity: capacity,
			})
		case strings.HasSuffix(strings.ToLower(r.Type), "/throughputsettings"):
			region, ok := literalRegion(r.Region)
			if !ok {
				region = "global"
			}
			if throughput, ok := cosmosThroughput(r); ok {
				drivers = append(drivers, driver{
					Kind:     catalog.DriverCosmosProvisioned,
					Name:     r.Name,
					Type:     r.Type,
					Region:   region,
					SKUName:  "RUs",
					Quantity: throughput,
					Notes:    []string{"quantity is provisioned RU/s divided by the public 100 RU/s retail meter"},
				})
				continue
			}
			if max, ok := cosmosAutoscale(r); ok {
				unsupported = append(unsupported, note(r, "Cosmos autoscale pricing is UNVERIFIED for this estimator (maxThroughput="+strconv.Itoa(max)+")"))
				continue
			}
			unsupported = append(unsupported, note(r, "Cosmos throughput is unresolved"))
		case strings.EqualFold(r.Type, "Microsoft.CognitiveServices/accounts/deployments"):
			switch strings.TrimSpace(r.SKUName) {
			case "GlobalStandard", "Standard":
				excluded = append(excluded, note(r, "token-based model deployment is excluded from fixed-capacity estimation"))
			case "GlobalProvisionedManaged", "DataZoneProvisionedManaged", "ProvisionedManaged":
				unsupported = append(unsupported, note(r, "PTU retail meter mapping remains UNVERIFIED for generic deployment SKUs"))
			}
		}
	}
	return drivers, excluded, unsupported, notes
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

func literalRegion(region string) (string, bool) {
	region = strings.TrimSpace(region)
	if region == "" || strings.HasPrefix(region, "[") {
		return "", false
	}
	return strings.ToLower(region), true
}

func intProp(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	}
	return 0, false
}

func skuCapacity(r sdk.ARMResource) (float64, bool) {
	if r.SKU == nil {
		return 0, false
	}
	switch v := r.SKU["capacity"].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

func cosmosThroughput(r sdk.ARMResource) (float64, bool) {
	resource, ok := mapValue(r.Properties, "resource")
	if !ok {
		return 0, false
	}
	switch v := resource["throughput"].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

func cosmosAutoscale(r sdk.ARMResource) (int, bool) {
	resource, ok := mapValue(r.Properties, "resource")
	if !ok {
		return 0, false
	}
	auto, ok := mapValue(resource, "autoscaleSettings")
	if !ok {
		return 0, false
	}
	switch v := auto["maxThroughput"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func mapValue(m map[string]any, key string) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key].(map[string]any)
	return v, ok
}

func round2(v float64) float64 {
	s := fmt.Sprintf("%.2f", v)
	out, _ := strconv.ParseFloat(s, 64)
	return out
}

// DefaultCachePath returns the built-in cache path.
func DefaultCachePath() string { return filepath.Join(".foundry-doctor", "cache", "prices.json") }

// EnsureCacheDir creates the built-in cache directory when needed.
func EnsureCacheDir(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
