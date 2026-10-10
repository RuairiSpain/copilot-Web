package prices

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientLookupAndCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[{"currencyCode":"EUR","unitPrice":1.25,"armRegionName":"eastus","productName":"Azure AI Search","skuName":"Standard S2","meterName":"Standard S2 Unit","serviceName":"Azure Cognitive Search","unitOfMeasure":"1 Hour","effectiveStartDate":"2025-10-01T00:00:00Z","type":"Consumption"}]}`))
	}))
	defer srv.Close()

	c := Client{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC) }}
	q := Query{ServiceName: "Azure Cognitive Search", ProductName: "Azure AI Search", Region: "eastus", CurrencyCode: "EUR", MeterName: "Standard S2 Unit", RequireHourly: true}
	rec, err := c.Lookup(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if rec.CurrencyCode != "EUR" || rec.UnitPrice != 1.25 {
		t.Fatalf("unexpected record %+v", rec)
	}

	cachePath := filepath.Join(t.TempDir(), "prices.json")
	got, usedCache, err := Resolve(context.Background(), c, cachePath, false, q)
	if err != nil {
		t.Fatal(err)
	}
	if usedCache {
		t.Fatal("first resolve should be live")
	}
	if got.UnitPrice != 1.25 {
		t.Fatalf("unit price = %v", got.UnitPrice)
	}
	got2, usedCache, err := Resolve(context.Background(), Client{Now: c.Now}, cachePath, true, q)
	if err != nil {
		t.Fatal(err)
	}
	if !usedCache || got2.UnitPrice != 1.25 {
		t.Fatalf("offline cache fallback failed: %+v usedCache=%v", got2, usedCache)
	}
}

func TestOfflineMissingCacheIsUnavailable(t *testing.T) {
	q := Query{ServiceName: "Azure Cognitive Search", Region: "eastus", MeterName: "Standard Unit"}
	if _, _, err := Resolve(context.Background(), Client{}, filepath.Join(t.TempDir(), "missing.json"), true, q); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

func TestStale(t *testing.T) {
	rec := Record{RetrievedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if !Stale(rec, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC), 30*24*time.Hour) {
		t.Fatal("expected stale record")
	}
}
