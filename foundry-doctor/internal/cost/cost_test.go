package cost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/prices"
	costreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type model []sdk.ARMResource

func (m model) Resources() []sdk.ARMResource { return []sdk.ARMResource(m) }

var fixedNow = func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) }

func priceServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"Items":[]}`
		q := r.URL.RawQuery
		cur := "USD"
		if strings.Contains(q, "currencyCode=%27EUR%27") {
			cur = "EUR"
		}
		region := "eastus"
		if strings.Contains(q, "westeurope") {
			region = "westeurope"
		}
		item := func(service, product, meter, unit string, price string) string {
			return `{"Items":[{"currencyCode":"` + cur + `","tierMinimumUnits":0,"unitPrice":` + price + `,"armRegionName":"` + region + `","productName":"` + product +
				`","meterName":"` + meter + `","serviceName":"` + service + `","unitOfMeasure":"` + unit + `","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption","isPrimaryMeterRegion":true}]}`
		}
		switch {
		case strings.Contains(q, "Standard+S2+Unit"):
			body = item("Azure Cognitive Search", "Azure AI Search", "Standard S2 Unit", "1 Hour", "1.344")
		case strings.Contains(q, "Standard+S1+Unit"):
			body = item("Azure Cognitive Search", "Azure AI Search", "Standard S1 Unit", "1 Hour", "0.336")
		case strings.Contains(q, "Premium+Unit"):
			body = item("API Management", "API Management", "Premium Unit", "1 Hour", "2.795")
		case strings.Contains(q, "100+RU%2Fs"):
			body = item("Azure Cosmos DB", "Azure Cosmos DB", "100 RU/s", "1/Hour", "0.008")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func estimate(t *testing.T, srv *httptest.Server, currency string, envs ...Environment) costreport.Document {
	t.Helper()
	req := Request{Environments: envs, Currency: currency, PriceCachePath: t.TempDir() + "/prices.json", Now: fixedNow}
	if srv != nil {
		req.BaseURL, req.HTTPClient = srv.URL, srv.Client()
	} else {
		req.Offline = true
	}
	doc, err := Engine{}.Estimate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func cosmosAccount(name string, locations ...string) sdk.ARMResource {
	var locs []any
	for i, l := range locations {
		locs = append(locs, map[string]any{"locationName": l, "failoverPriority": float64(i), "isZoneRedundant": false})
	}
	return sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts", Name: name, Region: locations[0], Properties: map[string]any{"locations": locs}}
}

func reasons(items []costreport.NoteItem, name string) string {
	var out []string
	for _, x := range items {
		if x.ResourceName == name {
			out = append(out, x.Reason)
		}
	}
	return strings.Join(out, " | ")
}

func TestEstimateIncludesSupportedAndExcludedResources(t *testing.T) {
	srv := priceServer(t)
	doc := estimate(t, srv, "", Environment{Name: "prod", Profile: "foundry-prod", ARM: model{
		{Type: "Microsoft.Search/searchServices", Name: "search", Region: "eastus", SKUName: "standard2", Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}},
		{Type: "Microsoft.ApiManagement/service", Name: "apim", Region: "eastus", SKUName: "Premium", SKU: map[string]any{"capacity": float64(1)}},
		cosmosAccount("acct", "eastus"),
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", Name: "acct/db/default", Properties: map[string]any{"resource": map[string]any{"throughput": float64(4000)}}},
		{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/chat", SKUName: "GlobalStandard"},
		{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/ptu", SKUName: "ProvisionedManaged", SKU: map[string]any{"name": "ProvisionedManaged", "capacity": float64(50)}},
	}})
	env := doc.Environments[0]
	if len(env.Estimated) != 3 {
		t.Fatalf("estimated lines = %d, want 3: %+v", len(env.Estimated), env)
	}
	for _, l := range env.Estimated {
		if l.Region != "eastus" {
			t.Errorf("%s region %q; throughputSettings without location must inherit the account region", l.ResourceName, l.Region)
		}
	}
	if !strings.Contains(reasons(env.Excluded, "acct/chat"), "token-based") {
		t.Fatalf("excluded = %+v", env.Excluded)
	}
	if r := reasons(env.Unsupported, "acct/ptu"); !strings.Contains(r, "50 PTU") || !strings.Contains(r, "ProvisionedManaged (Regional)") {
		t.Fatalf("unsupported PTU note = %q", r)
	}
	// 2 units x 1.344 x 730 + 1 x 2.795 x 730 + 40 x 0.008 x 730
	want := round2(2*1.344*730) + round2(2.795*730) + round2(40*0.008*730)
	if env.TotalHigh != round2(want) || env.TotalLow != env.TotalHigh {
		t.Fatalf("total = %v/%v, want %v", env.TotalLow, env.TotalHigh, round2(want))
	}
	if !env.Incomplete {
		t.Fatal("a PTU deployment leaves the estimate incomplete")
	}
}

func TestDeploymentSKUsAreClassifiedCaseInsensitively(t *testing.T) {
	excluded := []string{"GlobalStandard", "globalstandard", "DataZoneStandard", "STANDARD", "GlobalBatch", "datazonebatch", "DeveloperTier"}
	unsupported := []string{"GlobalProvisionedManaged", "datazoneprovisionedmanaged", "ProvisionedManaged"}
	var m model
	for _, s := range append(append([]string{}, excluded...), unsupported...) {
		m = append(m, sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/" + s, SKUName: s})
	}
	m = append(m,
		sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/nosku"},
		sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/expr", SKU: map[string]any{"name": "[parameters('sku')]"}},
		sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "acct/odd", SKUName: "SuperFast"},
	)
	env := estimate(t, nil, "", Environment{Name: "x", ARM: m}).Environments[0]
	for _, s := range excluded {
		if !strings.Contains(reasons(env.Excluded, "acct/"+s), "token-based") {
			t.Errorf("%s: want excluded as token-based, got excluded=%q unsupported=%q", s, reasons(env.Excluded, "acct/"+s), reasons(env.Unsupported, "acct/"+s))
		}
	}
	for _, s := range unsupported {
		if !strings.Contains(reasons(env.Unsupported, "acct/"+s), "provisioned throughput") {
			t.Errorf("%s: want unsupported PTU, got %q", s, reasons(env.Unsupported, "acct/"+s))
		}
	}
	for _, n := range []string{"acct/nosku", "acct/expr", "acct/odd"} {
		if !strings.Contains(reasons(env.Unsupported, n), "unknown or unresolved deployment SKU") {
			t.Errorf("%s: want unknown/unresolved SKU, got %q", n, reasons(env.Unsupported, n))
		}
	}
}

func TestConditionsAndLoops(t *testing.T) {
	srv := priceServer(t)
	search := func(name string) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.Search/searchServices", Name: name, Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}}
	}
	never, maybe, looped, always := search("never"), search("maybe"), search("looped"), search("always")
	never.Conditional, never.Condition = true, "[false()]"
	maybe.Conditional, maybe.Condition = true, "[parameters('deploy')]"
	looped.Looped = true
	always.Conditional, always.Condition = true, "true"
	env := estimate(t, srv, "", Environment{Name: "x", ARM: model{never, maybe, looped, always}}).Environments[0]
	if len(env.Estimated) != 1 || env.Estimated[0].ResourceName != "always" {
		t.Fatalf("estimated = %+v", env.Estimated)
	}
	if reasons(env.Unsupported, "never") != "" || reasons(env.Excluded, "never") != "" {
		t.Error("a literal-false condition is not deployed and must not be listed")
	}
	for _, n := range []string{"maybe", "looped"} {
		if !strings.Contains(reasons(env.Unsupported, n), "conditional or looped") {
			t.Errorf("%s: %q", n, reasons(env.Unsupported, n))
		}
	}
	if !env.Incomplete {
		t.Error("uncertain resources make the estimate incomplete")
	}
	if env.Estimated[0].MeterName != "Standard S1 Unit" {
		t.Errorf("S1 meter = %q", env.Estimated[0].MeterName)
	}
}

func TestCosmosDatabaseContainerAndRegions(t *testing.T) {
	srv := priceServer(t)
	env := estimate(t, srv, "", Environment{Name: "x", ARM: model{
		cosmosAccount("acct", "eastus", "westeurope"),
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "acct/db", Properties: map[string]any{"options": map[string]any{"throughput": float64(1000)}}},
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", Name: "acct/db/auto", Properties: map[string]any{"options": map[string]any{"autoscaleSettings": map[string]any{"maxThroughput": float64(4000)}}}},
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", Name: "acct/db/shared", Properties: map[string]any{}},
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", Name: "acct/db/expr", Properties: map[string]any{"options": map[string]any{"throughput": "[parameters('ru')]"}}},
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "orphan/db", Properties: map[string]any{"options": map[string]any{"throughput": float64(400)}}},
	}}).Environments[0]
	var regions []string
	for _, l := range env.Estimated {
		if l.ResourceName == "acct/db" {
			regions = append(regions, l.Region)
			if l.Quantity != 10 {
				t.Errorf("quantity = %v, want 10 x100 RU/s", l.Quantity)
			}
		}
	}
	if strings.Join(regions, ",") != "eastus,westeurope" {
		t.Fatalf("database lines per account region = %v", regions)
	}
	if !strings.Contains(reasons(env.Unsupported, "acct/db/auto"), "autoscale") {
		t.Errorf("autoscale: %q", reasons(env.Unsupported, "acct/db/auto"))
	}
	if !strings.Contains(reasons(env.Excluded, "acct/db/shared"), "no dedicated provisioned throughput") {
		t.Errorf("shared container: %q", reasons(env.Excluded, "acct/db/shared"))
	}
	if !strings.Contains(reasons(env.Unsupported, "acct/db/expr"), "unresolved") {
		t.Errorf("expression throughput: %q", reasons(env.Unsupported, "acct/db/expr"))
	}
	if !strings.Contains(reasons(env.Unsupported, "orphan/db"), "parent Cosmos DB account") {
		t.Errorf("orphan: %q", reasons(env.Unsupported, "orphan/db"))
	}
}

func TestCosmosAccountBillingModesAreUnsupported(t *testing.T) {
	for name, props := range map[string]map[string]any{
		"multi-region write": {"enableMultipleWriteLocations": true},
		"free-tier":          {"enableFreeTier": true},
		"zone-redundant":     {"zone": true},
		"locations":          {"locations": "[variables('locs')]"},
	} {
		t.Run(name, func(t *testing.T) {
			acct := cosmosAccount("acct", "eastus")
			for k, v := range props {
				if k == "zone" {
					acct.Properties["locations"] = []any{map[string]any{"locationName": "eastus", "isZoneRedundant": true}}
					continue
				}
				acct.Properties[k] = v
			}
			env := estimate(t, nil, "", Environment{Name: "x", ARM: model{acct,
				{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "acct/db", Properties: map[string]any{"options": map[string]any{"throughput": float64(400)}}},
			}}).Environments[0]
			if len(env.Estimated) != 0 || reasons(env.Unsupported, "acct/db") == "" {
				t.Fatalf("want unsupported, got %+v", env)
			}
		})
	}
}

func TestSearchAndAPIMEdgeCases(t *testing.T) {
	srv := priceServer(t)
	env := estimate(t, srv, "", Environment{Name: "x", ARM: model{
		{Type: "Microsoft.Search/searchServices", Name: "cc", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1), "computeType": "Confidential"}},
		{Type: "Microsoft.Search/searchServices", Name: "hd", Region: "eastus", SKUName: "standard3", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1), "hostingMode": "HighDensity"}},
		{Type: "Microsoft.Search/searchServices", Name: "nocount", Region: "eastus", SKUName: "standard", Properties: map[string]any{}},
		{Type: "Microsoft.Search/searchServices", Name: "free", Region: "eastus", SKUName: "free"},
		{Type: "Microsoft.ApiManagement/service", Name: "consumption", Region: "eastus", SKUName: "Consumption"},
		{Type: "Microsoft.ApiManagement/service", Name: "multi", Region: "eastus", SKUName: "Premium", SKU: map[string]any{"capacity": float64(1)},
			Properties: map[string]any{"additionalLocations": []any{map[string]any{"location": "westeurope", "sku": map[string]any{"name": "Premium", "capacity": float64(2)}}}}},
		{Type: "Microsoft.ApiManagement/service", Name: "v2", Region: "eastus", SKUName: "PremiumV2", SKU: map[string]any{"capacity": float64(1)}},
		{Type: "Microsoft.Search/searchServices", Name: "gov", Region: "usgovvirginia", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}},
		{Type: "Microsoft.Storage/storageAccounts", Name: "st", Region: "eastus", SKUName: "Standard_LRS"},
	}}).Environments[0]
	for name, want := range map[string]string{
		"cc":      "confidential",
		"hd":      "HighDensity",
		"nocount": "not set",
		"multi":   "2 units in westeurope",
		"v2":      "no mapping",
		"gov":     "sovereign",
	} {
		if !strings.Contains(reasons(env.Unsupported, name), want) {
			t.Errorf("%s: unsupported %q, want %q", name, reasons(env.Unsupported, name), want)
		}
	}
	for name, want := range map[string]string{
		"free":        "free or serverless",
		"consumption": "Consumption is request-based",
		"st":          "not modelled",
	} {
		if !strings.Contains(reasons(env.Excluded, name), want) {
			t.Errorf("%s: excluded %q, want %q", name, reasons(env.Excluded, name), want)
		}
	}
	// The primary APIM region is still estimated next to the unpriced additional region.
	if len(env.Estimated) != 1 || env.Estimated[0].ResourceName != "multi" {
		t.Fatalf("estimated = %+v", env.Estimated)
	}
}

func TestCurrencyAndRegionHandling(t *testing.T) {
	srv := priceServer(t)
	arm := model{{Type: "Microsoft.Search/searchServices", Name: "s", Region: "WestEurope", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}}}
	env := estimate(t, srv, "eur", Environment{Name: "x", ARM: arm}).Environments[0]
	if env.Currency != "EUR" || len(env.Estimated) != 1 || env.Estimated[0].Region != "westeurope" {
		t.Fatalf("env = %+v", env)
	}
	found := false
	for _, n := range env.Notes {
		found = found || strings.Contains(n, "reference values")
	}
	if !found {
		t.Errorf("non-USD reports must say prices are reference values: %v", env.Notes)
	}
	unres := model{{Type: "Microsoft.Search/searchServices", Name: "s", Region: "[parameters('location')]", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}}}
	env = estimate(t, srv, "", Environment{Name: "x", ARM: unres}).Environments[0]
	if !strings.Contains(reasons(env.Unsupported, "s"), "region is unresolved") {
		t.Fatalf("unresolved region: %+v", env.Unsupported)
	}
}

// A server that answers USD to an EUR request never yields an EUR figure.
func TestCurrencyMismatchIsUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Items":[{"currencyCode":"USD","unitPrice":0.336,"armRegionName":"eastus","productName":"Azure AI Search","meterName":"Standard S1 Unit","serviceName":"Azure Cognitive Search","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true}]}`))
	}))
	defer srv.Close()
	arm := model{{Type: "Microsoft.Search/searchServices", Name: "s", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}}}
	env := estimate(t, srv, "EUR", Environment{Name: "x", ARM: arm}).Environments[0]
	if len(env.Estimated) != 0 || !strings.Contains(reasons(env.Unsupported, "s"), "currency mismatch") || !env.Incomplete {
		t.Fatalf("env = %+v", env)
	}
}

func TestRoundingPerLine(t *testing.T) {
	if got := round2(0.336 * 730); got != 245.28 {
		t.Errorf("round2 = %v", got)
	}
	if got := round2(2.005); got != 2.0 && got != 2.01 {
		t.Errorf("round2(2.005) = %v", got)
	}
	if got := round2(-1.234); got != -1.23 {
		t.Errorf("round2(-1.234) = %v", got)
	}
}

func TestCompareEnvironmentsWithDifferentSKUs(t *testing.T) {
	srv := priceServer(t)
	mk := func(sku string, replicas float64) model {
		return model{{Type: "Microsoft.Search/searchServices", Name: "s", Region: "eastus", SKUName: sku, Properties: map[string]any{"replicaCount": replicas, "partitionCount": float64(1)}}}
	}
	doc := estimate(t, srv, "", Environment{Name: "dev", ARM: mk("standard", 1)}, Environment{Name: "prod", ARM: mk("standard2", 3)})
	if len(doc.Comparisons) != 1 || !doc.Comparisons[0].Comparable {
		t.Fatalf("comparisons = %+v", doc.Comparisons)
	}
	want := round2(round2(3*1.344*730) - round2(0.336*730))
	if doc.Comparisons[0].DeltaHigh != want {
		t.Fatalf("delta = %v, want %v", doc.Comparisons[0].DeltaHigh, want)
	}
	if !strings.Contains(doc.Comparisons[0].Reason, "covered resources only") {
		t.Errorf("comparison must say it covers estimated lines only: %q", doc.Comparisons[0].Reason)
	}
}

func TestEstimateOfflineMissingCacheMarksIncomplete(t *testing.T) {
	doc := estimate(t, nil, "", Environment{Name: "prod", Profile: "foundry-prod", ARM: model{
		{Type: "Microsoft.Search/searchServices", Name: "search", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}},
	}})
	if !doc.Environments[0].Incomplete || len(doc.Environments[0].Unsupported) == 0 {
		t.Fatalf("expected incomplete offline report: %+v", doc.Environments[0])
	}
}

// cachedEstimate fills the price cache from the fake live API, lets mutate
// change the cached records, then estimates offline from that cache.
func cachedEstimate(t *testing.T, resources model, mutate func([]prices.Record)) costreport.EnvironmentReport {
	t.Helper()
	srv := priceServer(t)
	cachePath := t.TempDir() + "/prices.json"
	env := Environment{Name: "prod", Profile: "foundry-prod", ARM: resources}
	live := Request{Environments: []Environment{env}, PriceCachePath: cachePath, Now: fixedNow, BaseURL: srv.URL, HTTPClient: srv.Client()}
	if _, err := (Engine{}).Estimate(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	c, err := prices.LoadCache(cachePath)
	if err != nil || len(c.Records) == 0 {
		t.Fatalf("cache not written: %v %+v", err, c)
	}
	mutate(c.Records)
	if err := prices.SaveCache(cachePath, c); err != nil {
		t.Fatal(err)
	}
	doc, err := Engine{}.Estimate(context.Background(), Request{Environments: []Environment{env}, PriceCachePath: cachePath, Now: fixedNow, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	return doc.Environments[0]
}

func searchAndCosmos() model {
	return model{
		{Type: "Microsoft.Search/searchServices", Name: "search", Region: "eastus", SKUName: "standard2", Properties: map[string]any{"replicaCount": float64(1), "partitionCount": float64(1)}},
		cosmosAccount("acct", "eastus"),
		{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "acct/db", Region: "eastus", Properties: map[string]any{"options": map[string]any{"throughput": float64(400)}}},
	}
}

// Phase 7 gate (both reviewers): stale pricing is reported at engine level.
func TestStaleCachedPricesAreFlaggedNotHidden(t *testing.T) {
	old := fixedNow().AddDate(0, 0, -60)
	env := cachedEstimate(t, searchAndCosmos(), func(rs []prices.Record) {
		for i := range rs {
			rs[i].RetrievedAt = old
		}
	})
	if !env.StalePricing || !env.Incomplete || len(env.Estimated) == 0 {
		t.Fatalf("stale=%v incomplete=%v estimated=%d", env.StalePricing, env.Incomplete, len(env.Estimated))
	}
	if env.PriceRetrieved != old.Format(time.RFC3339) {
		t.Fatalf("PriceRetrieved = %q, want the stale date %q", env.PriceRetrieved, old.Format(time.RFC3339))
	}
	for _, l := range env.Estimated {
		if !strings.Contains(strings.Join(l.Notes, " "), "STALE") {
			t.Errorf("line %s lacks the stale marker: %v", l.ResourceName, l.Notes)
		}
	}
}

func TestMixedAgeCacheShowsOldestRetrievalDate(t *testing.T) {
	old := fixedNow().AddDate(0, 0, -60)
	env := cachedEstimate(t, searchAndCosmos(), func(rs []prices.Record) {
		rs[0].RetrievedAt = old // one stale record, the others stay fresh
	})
	if env.PriceRetrieved != old.Format(time.RFC3339) || !env.StalePricing {
		t.Fatalf("PriceRetrieved=%q stale=%v, want the oldest date %q", env.PriceRetrieved, env.StalePricing, old.Format(time.RFC3339))
	}
}

func TestTamperedCacheIsRejectedNotPriced(t *testing.T) {
	env := cachedEstimate(t, searchAndCosmos(), func(rs []prices.Record) {
		for i := range rs {
			rs[i].UnitPrice = -5
		}
	})
	if len(env.Estimated) != 0 || env.TotalHigh != 0 {
		t.Fatalf("a negative cached price was priced: %+v", env.Estimated)
	}
	if !strings.Contains(reasons(env.Unsupported, "search"), "rejected") {
		t.Fatalf("unsupported reasons = %q", reasons(env.Unsupported, "search"))
	}
}

// Phase 7 gate (black belt B1): Premium units after the first cost less, so
// the capacity is not priced at the first-unit rate.
func TestApimPremiumWithMoreThanOneUnitIsNotPriced(t *testing.T) {
	doc := estimate(t, priceServer(t), "", Environment{Name: "prod", Profile: "foundry-prod", ARM: model{
		{Type: "Microsoft.ApiManagement/service", Name: "apim3", Region: "eastus", SKUName: "Premium", SKU: map[string]any{"capacity": float64(3)}},
	}})
	env := doc.Environments[0]
	if len(env.Estimated) != 0 || !strings.Contains(reasons(env.Unsupported, "apim3"), "units after the first") {
		t.Fatalf("estimated=%+v unsupported=%q", env.Estimated, reasons(env.Unsupported, "apim3"))
	}
}
