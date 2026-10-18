package prices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testNow = func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }

func serveBody(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serveFile(t *testing.T, name string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return serveBody(t, string(data))
}

func client(srv *httptest.Server) Client {
	return Client{BaseURL: srv.URL, HTTP: srv.Client(), Now: testNow}
}

func TestRequestURLPinsVersionCurrencyAndPriceType(t *testing.T) {
	u, err := RequestURL("", Query{ServiceName: "API Management", ProductName: "API Management", Region: "eastus", CurrencyCode: "eur", MeterName: "Basic v2 Unit"})
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != RetailHost {
		t.Errorf("url = %s", u)
	}
	if q.Get("api-version") != "2023-01-01-preview" {
		t.Errorf("api-version = %q", q.Get("api-version"))
	}
	if q.Get("currencyCode") != "'EUR'" {
		t.Errorf("currencyCode = %q, want the documented quoted form 'EUR'", q.Get("currencyCode"))
	}
	f := q.Get("$filter")
	for _, want := range []string{"serviceName eq 'API Management'", "armRegionName eq 'eastus'", "meterName eq 'Basic v2 Unit'", "priceType eq 'Consumption'", "productName eq 'API Management'"} {
		if !strings.Contains(f, want) {
			t.Errorf("$filter %q lacks %q", f, want)
		}
	}
}

func TestRecordedAPIMBasicV2(t *testing.T) {
	srv := serveFile(t, "apim-basicv2-eastus.json")
	rec, err := client(srv).Lookup(context.Background(), Query{ServiceName: "API Management", ProductName: "API Management", Region: "eastus", MeterName: "Basic v2 Unit", RequireHourly: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.UnitPrice != 0.20548 || rec.CurrencyCode != "USD" || !rec.PrimaryMeterRegion || rec.MeterID != "a7b658f5-ab15-5542-9af5-d4da0c151b57" {
		t.Fatalf("record %+v", rec)
	}
}

func TestRecordedSearchS1NonPrimary(t *testing.T) {
	srv := serveFile(t, "search-mixed.json")
	rec, err := client(srv).Lookup(context.Background(), Query{ServiceName: "Azure Cognitive Search", ProductName: "Azure AI Search", Region: "koreacentral", MeterName: "Standard S1 Unit", RequireHourly: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.UnitPrice != 0.336 || rec.PrimaryMeterRegion {
		t.Fatalf("record %+v", rec)
	}
	// Region filtering is applied client side as well: no S1 row in eastus.
	if _, err := client(srv).Lookup(context.Background(), Query{ServiceName: "Azure Cognitive Search", ProductName: "Azure AI Search", Region: "eastus", MeterName: "Standard S1 Unit", RequireHourly: true}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

const mixedTypes = `{"Items":[
 {"currencyCode":"USD","tierMinimumUnits":0,"unitPrice":0.5,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"DevTestConsumption","isPrimaryMeterRegion":true},
 {"currencyCode":"USD","tierMinimumUnits":0,"reservationTerm":"1 Year","unitPrice":2500,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Reservation","isPrimaryMeterRegion":true},
 {"currencyCode":"USD","tierMinimumUnits":100,"unitPrice":0.7,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true},
 {"currencyCode":"USD","tierMinimumUnits":0,"unitPrice":0.9,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":false},
 {"currencyCode":"USD","tierMinimumUnits":0,"unitPrice":0.8,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true,"effectiveStartDate":"2026-01-01T00:00:00Z"},
 {"currencyCode":"USD","tierMinimumUnits":0,"unitPrice":0.6,"armRegionName":"eastus","productName":"P","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true,"effectiveStartDate":"2025-01-01T00:00:00Z","effectiveEndDate":"2025-12-31T23:59:00Z"}
]}`

// DevTest, Reservation, tiered, non-primary and expired rows are never
// chosen, even when cheaper.
func TestOnlyCurrentPrimaryConsumptionRowIsUsed(t *testing.T) {
	srv := serveBody(t, mixedTypes)
	rec, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", ProductName: "P", Region: "eastus", MeterName: "M", RequireHourly: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.UnitPrice != 0.8 || rec.Type != "Consumption" {
		t.Fatalf("record %+v, want the current primary Consumption price 0.8", rec)
	}
}

func TestAmbiguousPricesAreRefused(t *testing.T) {
	srv := serveBody(t, `{"Items":[
 {"currencyCode":"USD","unitPrice":1,"armRegionName":"eastus","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true},
 {"currencyCode":"USD","unitPrice":2,"armRegionName":"eastus","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true}]}`)
	_, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", Region: "eastus", MeterName: "M"})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("err = %v, want ErrAmbiguous", err)
	}
	// Resolve never replaces an ambiguous answer with a cached price.
	cachePath := filepath.Join(t.TempDir(), "prices.json")
	q := Query{ServiceName: "S", Region: "eastus", MeterName: "M"}
	_ = SaveCache(cachePath, Cache{Records: []Record{{Query: q, CurrencyCode: "USD", UnitPrice: 1, RetrievedAt: testNow()}}})
	if _, _, err := Resolve(context.Background(), client(srv), cachePath, false, q); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Resolve err = %v, want ErrAmbiguous", err)
	}
}

func TestCurrencyMismatchIsRefused(t *testing.T) {
	srv := serveBody(t, `{"Items":[{"currencyCode":"USD","unitPrice":1,"armRegionName":"westeurope","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true}]}`)
	_, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", Region: "westeurope", MeterName: "M", CurrencyCode: "EUR"})
	if !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestPagingFollowsSameOriginOnly(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"Items":[{"currencyCode":"USD","unitPrice":3,"armRegionName":"eastus","meterName":"M","serviceName":"S","unitOfMeasure":"1 Hour","type":"Consumption","isPrimaryMeterRegion":true}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"Items":[],"NextPageLink":"` + srv.URL + `/?page=2"}`))
	}))
	defer srv.Close()
	rec, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", Region: "eastus", MeterName: "M"})
	if err != nil || rec.UnitPrice != 3 {
		t.Fatalf("two-page lookup: %+v err=%v", rec, err)
	}

	foreign := serveBody(t, `{"Items":[],"NextPageLink":"https://attacker.example/api/retail/prices?$skip=1000"}`)
	if _, err := client(foreign).Lookup(context.Background(), Query{ServiceName: "S", Region: "eastus", MeterName: "M"}); !errors.Is(err, ErrPaging) {
		t.Fatalf("foreign NextPageLink err = %v, want ErrPaging", err)
	}
}

func TestPageCap(t *testing.T) {
	var hits int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"Items":[],"NextPageLink":"` + srv.URL + `/?loop=1"}`))
	}))
	defer srv.Close()
	_, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", Region: "eastus", MeterName: "M"})
	if !errors.Is(err, ErrPaging) {
		t.Fatalf("err = %v, want ErrPaging", err)
	}
	if got := atomic.LoadInt32(&hits); got != MaxPages {
		t.Fatalf("pages fetched = %d, want %d", got, MaxPages)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://attacker.example/", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := client(srv).Lookup(context.Background(), Query{ServiceName: "S", Region: "eastus", MeterName: "M"}); err == nil {
		t.Fatal("a redirect must fail the lookup")
	}
}

func TestCacheWriteFailureKeepsLivePrice(t *testing.T) {
	srv := serveFile(t, "apim-basicv2-eastus.json")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	q := Query{ServiceName: "API Management", ProductName: "API Management", Region: "eastus", MeterName: "Basic v2 Unit", RequireHourly: true}
	rec, usedCache, err := Resolve(context.Background(), client(srv), filepath.Join(blocker, "prices.json"), false, q)
	if err != nil || usedCache || rec.UnitPrice != 0.20548 {
		t.Fatalf("rec=%+v usedCache=%v err=%v", rec, usedCache, err)
	}
	if !strings.Contains(rec.Notice, "price cache not written") {
		t.Fatalf("notice = %q", rec.Notice)
	}
}

func TestLiveFailureFallsBackToCacheWithNotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	q := Query{ServiceName: "S", Region: "eastus", MeterName: "M"}
	cachePath := filepath.Join(t.TempDir(), "prices.json")
	if err := SaveCache(cachePath, Cache{Records: []Record{{Query: q, CurrencyCode: "USD", UnitPrice: 1.5, RetrievedAt: testNow()}}}); err != nil {
		t.Fatal(err)
	}
	rec, usedCache, err := Resolve(context.Background(), client(srv), cachePath, false, q)
	if err != nil || !usedCache || rec.UnitPrice != 1.5 || !strings.Contains(rec.Notice, "503") {
		t.Fatalf("rec=%+v usedCache=%v err=%v", rec, usedCache, err)
	}
	// Without a cache the original cause is kept.
	_, _, err = Resolve(context.Background(), client(srv), filepath.Join(t.TempDir(), "none.json"), false, q)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want the live cause", err)
	}
}

func TestCachedRecordInOtherCurrencyIsIgnored(t *testing.T) {
	q := Query{ServiceName: "S", Region: "eastus", MeterName: "M", CurrencyCode: "EUR"}
	cachePath := filepath.Join(t.TempDir(), "prices.json")
	if err := SaveCache(cachePath, Cache{Records: []Record{{Query: q, CurrencyCode: "USD", UnitPrice: 1, RetrievedAt: testNow()}}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(context.Background(), Client{Now: testNow}, cachePath, true, q); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

func TestOldCacheSchemaIsIgnored(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "prices.json")
	if err := os.WriteFile(cachePath, []byte(`{"version":1,"records":[{"query":{"serviceName":"S","region":"eastus","meterName":"M"},"currencyCode":"USD","unitPrice":1}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Resolve(context.Background(), Client{Now: testNow}, cachePath, true, Query{ServiceName: "S", Region: "eastus", MeterName: "M"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist for a version-1 cache", err)
	}
}
