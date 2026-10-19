package cost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	costengine "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func searchRes(name string) sdk.ARMResource {
	return sdk.ARMResource{Type: typeSearch, Name: name, Region: "eastus", SKUName: "standard",
		Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}}
}

func projectRes(name string) sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/projects", Name: name}
}

func fakeEstimator(e Estimate, err error, calls *int) Estimator {
	return func(context.Context, sdk.ARMModel, string) (Estimate, error) {
		if calls != nil {
			*calls++
		}
		return e, err
	}
}

func run004(t *testing.T, p Providers, in *sdk.Input) sdk.Result {
	t.Helper()
	got, err := find(RegisterWith(p), "FND-COST-004").Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func keyed(res sdk.Result, key string) []sdk.Finding {
	var out []sdk.Finding
	for _, f := range res.Findings {
		if f.Key == key {
			out = append(out, f)
		}
	}
	return out
}

var pricedEstimate = Estimate{
	Currency: "USD", HoursPerMonth: 730, MonthlyLow: 490.56, MonthlyHigh: 490.56,
	PriceRetrieved: "2026-10-04T00:00:00Z", UsedCache: true, Priced: 1,
	Assumptions: []string{"730 hours per month", "public retail list prices"},
}

func TestCost004EstimateFinding(t *testing.T) {
	arm := model{projectRes("acct/zeta"), projectRes("acct/alpha"), searchRes("search")}
	calls := 0
	got := run004(t, Providers{Estimate: fakeEstimator(pricedEstimate, nil, &calls)}, &sdk.Input{Profile: "foundry-prod", ARM: arm})
	if calls != 1 {
		t.Fatalf("estimator calls = %d", calls)
	}
	est := keyed(got, "estimate")
	if len(est) != 1 {
		t.Fatalf("want exactly one estimate finding, got %+v", got.Findings)
	}
	f := est[0]
	if f.Resource.Name != "acct/alpha" || f.Resource.Type != "Microsoft.CognitiveServices/accounts/projects" {
		t.Fatalf("anchor = %+v, want the first project by name", f.Resource)
	}
	for _, want := range []string{"490.56 USD per month", "2026-10-04", "730 hours per month", "public retail list prices", "local cache"} {
		if !strings.Contains(f.Evidence, want) {
			t.Errorf("evidence %q lacks %q", f.Evidence, want)
		}
	}
	if len(got.Findings) != 1 || got.Skipped != nil {
		t.Fatalf("a fully priced plan has only the estimate finding: %+v", got)
	}
}

func TestCost004EstimateRangeAndStale(t *testing.T) {
	e := pricedEstimate
	e.MonthlyHigh, e.Stale = 600, true
	got := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: model{searchRes("search")}})
	est := keyed(got, "estimate")
	if len(est) != 1 || !strings.Contains(est[0].Evidence, "490.56 to 600.00 USD") || !strings.Contains(est[0].Evidence, "older than 30 days") {
		t.Fatalf("got %+v", got.Findings)
	}
	if est[0].Resource.Name != "search" {
		t.Fatalf("without a project or account the first live resource anchors the estimate: %+v", est[0].Resource)
	}
}

func TestCost004EstimateAnchorsOnAccountWithoutProject(t *testing.T) {
	arm := model{searchRes("search"), {Type: "Microsoft.CognitiveServices/accounts", Name: "acct"}}
	got := run004(t, Providers{Estimate: fakeEstimator(pricedEstimate, nil, nil)}, &sdk.Input{ARM: arm})
	if est := keyed(got, "estimate"); len(est) != 1 || est[0].Resource.Name != "acct" {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestCost004UnpricedComponents(t *testing.T) {
	e := pricedEstimate
	e.Unpriced = []Unpriced{
		{ResourceType: typeSearch, ResourceName: "other", Reason: "retail price unavailable (no price guessed)", NoPrice: true},
	}
	arm := model{projectRes("acct/p"), searchRes("search"), searchRes("other")}
	got := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: arm})
	un := keyed(got, "incomplete:unpriced")
	if len(un) != 1 || un[0].Resource.Name != "other" || !strings.Contains(un[0].Evidence, "could not be priced") {
		t.Fatalf("got %+v", got.Findings)
	}
	if est := keyed(got, "estimate"); len(est) != 1 || !strings.Contains(est[0].Evidence, "1 component(s) could not be priced") {
		t.Fatalf("estimate finding: %+v", est)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("priced components must not get incomplete items: %+v", got.Findings)
	}
}

func TestCost004UnpricedNotDuplicatedWithExtractionItem(t *testing.T) {
	// replicaCount missing: the extraction already reports this resource.
	broken := searchRes("search")
	broken.Properties = map[string]any{"partitionCount": float64(1)}
	e := pricedEstimate
	e.Unpriced = []Unpriced{{ResourceType: typeSearch, ResourceName: "search", Reason: "x"}}
	got := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: model{broken}})
	if len(keyed(got, "incomplete:unpriced")) != 0 || len(keyed(got, "incomplete:search-replicas")) != 1 {
		t.Fatalf("got %+v", got.Findings)
	}
}

func TestCost004NothingPricedHasNoEstimateFinding(t *testing.T) {
	e := Estimate{Currency: "USD", HoursPerMonth: 730, Unpriced: []Unpriced{{ResourceType: typeSearch, ResourceName: "search", Reason: "retail price unavailable (no price guessed)", NoPrice: true}}}
	got := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: model{searchRes("search")}})
	if len(got.Findings) != 0 || got.Skipped == nil || !strings.HasPrefix(got.Skipped.Reason, "price-cache-missing-and-offline") {
		t.Fatalf("an empty price source is a skip, never an invented figure: %+v", got)
	}
}

func TestCost004NothingPricedForModellingGapKeepsIncompleteItem(t *testing.T) {
	e := Estimate{Currency: "USD", HoursPerMonth: 730, Unpriced: []Unpriced{{ResourceType: typeSearch, ResourceName: "search", Reason: "sku has no mapping in the supported-meter catalogue"}}}
	got := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: model{searchRes("search")}})
	if len(keyed(got, "estimate")) != 0 || len(keyed(got, "incomplete:unpriced")) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestCost004EstimatorFailureSkips(t *testing.T) {
	got := run004(t, Providers{Estimate: fakeEstimator(Estimate{}, errors.New("cache unreadable"), nil)}, &sdk.Input{ARM: model{searchRes("search")}})
	if got.Skipped == nil || len(got.Findings) != 0 || !strings.HasPrefix(got.Skipped.Reason, sdk.SkipInputUnavailable) {
		t.Fatalf("an estimator failure is a skip, never a pass: %+v", got)
	}
}

func TestCost004NoEstimatorKeepsIncompleteOnly(t *testing.T) {
	// Register() has no estimator: a complete plan is a clean pass as before.
	got := run004(t, Providers{}, &sdk.Input{ARM: model{searchRes("search")}})
	if got.Skipped != nil || len(got.Findings) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestCost004NoFixedCapacityDoesNotCallEstimator(t *testing.T) {
	calls := 0
	got := run004(t, Providers{Estimate: fakeEstimator(pricedEstimate, nil, &calls)}, &sdk.Input{ARM: model{projectRes("acct/p")}})
	if calls != 0 || got.Skipped == nil || got.Skipped.Reason != "no-supported-fixed-capacity-resources" {
		t.Fatalf("calls=%d got %+v", calls, got)
	}
}

func TestCost004EstimateIsDeterministicAndKeyStable(t *testing.T) {
	arm := model{projectRes("acct/p"), searchRes("search")}
	p := Providers{Estimate: fakeEstimator(pricedEstimate, nil, nil)}
	a := run004(t, p, &sdk.Input{ARM: arm})
	b := run004(t, p, &sdk.Input{ARM: arm})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("not deterministic:\n%+v\n%+v", a, b)
	}
	// A price change alters evidence only, never the Key that feeds the fingerprint.
	e := pricedEstimate
	e.MonthlyLow, e.MonthlyHigh = 1, 1
	c := run004(t, Providers{Estimate: fakeEstimator(e, nil, nil)}, &sdk.Input{ARM: arm})
	if a.Findings[0].Key != c.Findings[0].Key || a.Findings[0].Evidence == c.Findings[0].Evidence {
		t.Fatalf("key must be stable and evidence must change: %+v %+v", a.Findings[0], c.Findings[0])
	}
}

func TestCost004CachedEstimatorOfflineFromPopulatedCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"currencyCode":"USD","tierMinimumUnits":0,"unitPrice":0.336,"armRegionName":"eastus","productName":"Azure AI Search","meterName":"Standard S1 Unit","serviceName":"Azure Cognitive Search","unitOfMeasure":"1 Hour","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption","isPrimaryMeterRegion":true}]}`))
	}))
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "prices.json")
	arm := model{projectRes("acct/p"), {Type: typeSearch, Name: "search", Region: "eastus", SKUName: "standard", Properties: map[string]any{"replicaCount": float64(2), "partitionCount": float64(1)}}}
	_, err := costengine.Engine{}.Estimate(context.Background(), costengine.Request{
		Environments:   []costengine.Environment{{Name: "plan", Profile: "foundry-prod", ARM: arm}},
		PriceCachePath: cache, BaseURL: srv.URL, HTTPClient: srv.Client(),
		Now: func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Close() // the rule must not need the network

	got := run004(t, Providers{Estimate: CachedEstimator(cache)}, &sdk.Input{Profile: "foundry-prod", ARM: arm})
	est := keyed(got, "estimate")
	if len(est) != 1 {
		t.Fatalf("got %+v", got)
	}
	// 2 units x 0.336 x 730
	if !strings.Contains(est[0].Evidence, "490.56 USD per month") || !strings.Contains(est[0].Evidence, "local cache") {
		t.Fatalf("evidence %q", est[0].Evidence)
	}
}

func TestCost004CachedEstimatorMissingCacheReportsUnpriced(t *testing.T) {
	arm := model{projectRes("acct/p"), searchRes("search")}
	got := run004(t, Providers{Estimate: CachedEstimator(filepath.Join(t.TempDir(), "absent.json"))}, &sdk.Input{ARM: arm})
	if len(keyed(got, "estimate")) != 0 {
		t.Fatalf("no price may be invented: %+v", got.Findings)
	}
	if len(got.Findings) != 0 || got.Skipped == nil || !strings.HasPrefix(got.Skipped.Reason, "price-cache-missing-and-offline") {
		t.Fatalf("got %+v", got)
	}
}
