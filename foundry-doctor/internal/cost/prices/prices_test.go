package prices

import (
	"bytes"
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// Phase 7 gate (both reviewers): a unit that only contains "hour" is not one
// hour, and the cache is untrusted input.
func TestHourlyUnitIsExact(t *testing.T) {
	for unit, want := range map[string]bool{
		"1 Hour": true, "1 hour": true, " 1  Hour ": true, "1/Hour": true,
		"100 Hours": false, "1 Month": false, "1 Hour per 100": false, "Hours": false, "": false, "10/Hour": false,
	} {
		if got := hourlyUnit(unit); got != want {
			t.Errorf("hourlyUnit(%q) = %v, want %v", unit, got, want)
		}
	}
}

func TestResolveRejectsUntrustedCacheRecords(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	q := Query{ServiceName: "S", Region: "eastus", MeterName: "M", RequireHourly: true}
	good := Record{Query: q, CurrencyCode: "USD", UnitPrice: 1, UnitOfMeasure: "1 Hour", Type: "Consumption", RetrievedAt: now.Add(-time.Hour)}
	for name, mutate := range map[string]func(*Record){
		"negative price":    func(r *Record) { r.UnitPrice = -5 },
		"NaN price":         func(r *Record) { r.UnitPrice = math.NaN() },
		"monthly unit":      func(r *Record) { r.UnitOfMeasure = "1 Month" },
		"scaled unit":       func(r *Record) { r.UnitOfMeasure = "100 Hours" },
		"reservation price": func(r *Record) { r.Type = "Reservation" },
		"future retrieval":  func(r *Record) { r.RetrievedAt = now.AddDate(1, 0, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			rec := good
			mutate(&rec)
			path := filepath.Join(t.TempDir(), "prices.json")
			// SaveCache cannot encode NaN, so write the file by hand for it.
			data := Cache{Records: []Record{rec}}
			if math.IsNaN(rec.UnitPrice) {
				data.Records[0].UnitPrice = 0
				if err := SaveCache(path, data); err != nil {
					t.Fatal(err)
				}
				raw, _ := os.ReadFile(path)
				raw = bytes.Replace(raw, []byte(`"unitPrice": 0`), []byte(`"unitPrice": "NaN"`), 1)
				if err := os.WriteFile(path, raw, 0o600); err != nil {
					t.Fatal(err)
				}
				if _, _, err := Resolve(context.Background(), Client{Now: func() time.Time { return now }}, path, true, q); err == nil {
					t.Fatal("a cache with a non-numeric price must be unusable")
				}
				return
			}
			if err := SaveCache(path, data); err != nil {
				t.Fatal(err)
			}
			_, _, err := Resolve(context.Background(), Client{Now: func() time.Time { return now }}, path, true, q)
			if err == nil || !strings.Contains(err.Error(), "rejected") {
				t.Fatalf("err = %v, want the record rejected", err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "prices.json")
	if err := SaveCache(path, Cache{Records: []Record{good}}); err != nil {
		t.Fatal(err)
	}
	if rec, used, err := Resolve(context.Background(), Client{Now: func() time.Time { return now }}, path, true, q); err != nil || !used || rec.UnitPrice != 1 {
		t.Fatalf("a valid record must be used: rec=%+v used=%v err=%v", rec, used, err)
	}
}
