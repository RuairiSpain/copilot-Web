//go:build spike

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/prices"
	costreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/report"
)

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike -run TestCostFixedSampleEstimates ./cmd/foundry-doctor/
//
// It compiles samples/cost-fixed with the real Bicep CLI, prices it from a
// price cache with round, obviously illustrative prices (no network) and
// checks that the sample really produces an estimate: Search, APIM and Cosmos
// are priced, the PTU deployment is listed but not priced, and the token
// deployment is excluded.
func TestCostFixedSampleEstimates(t *testing.T) {
	exe := os.Getenv("BICEP_PATH")
	if exe == "" {
		t.Skip("BICEP_PATH not set")
	}
	dir := copyTree(t, sample("cost-fixed"))
	const region = "eastus2" // from infra/main.parameters.json
	rec := func(q prices.Query, price float64) prices.Record {
		return prices.Record{Query: q, CurrencyCode: "USD", Region: region, MeterName: q.MeterName, UnitOfMeasure: "1 Hour",
			UnitPrice: price, Type: "Consumption", PrimaryMeterRegion: true, RetrievedAt: time.Now().UTC()}
	}
	search, _ := catalog.Search(region, "USD", "standard2")
	apim, _ := catalog.APIM(region, "USD", "Premium")
	cosmos := catalog.CosmosProvisioned(region, "USD")
	cachePath := filepath.Join(dir, ".foundry-doctor", "cache", "prices.json")
	if err := prices.SaveCache(cachePath, prices.Cache{Records: []prices.Record{rec(search.Query, 1), rec(apim.Query, 2), rec(cosmos.Query, 0.01)}}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	svc := app.NewServices(&errb, app.Options{
		AzdVersion: func(context.Context) string { return "1.34.2" },
		Bicep:      bicep.DiscoverOptions{Path: exe},
	})
	if code := run(context.Background(), []string{"cost", "--dir", dir, "--offline", "--format", "json"}, &out, &errb, svc); code != 0 {
		t.Fatalf("exit=%d err=%q", code, errb.String())
	}
	var doc costreport.Document
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	env := doc.Environments[0]
	if len(env.Estimated) != 3 {
		t.Fatalf("want Search, APIM and Cosmos priced; got %d lines, unsupported=%+v\n%s", len(env.Estimated), env.Unsupported, errb.String())
	}
	// Search 2 units x 1 x 730, APIM 1 x 2 x 730, Cosmos 4000 RU/s = 40 x 0.01 x 730.
	if want := 1460.0 + 1460.0 + 292.0; env.TotalHigh != want {
		t.Fatalf("total = %.2f, want %.2f", env.TotalHigh, want)
	}
	var ptu, token bool
	for _, u := range env.Unsupported {
		ptu = ptu || strings.Contains(u.ResourceName, "ptu")
	}
	for _, x := range env.Excluded {
		token = token || strings.Contains(x.ResourceName, "chat")
	}
	if !ptu || !token {
		t.Fatalf("PTU deployment must be unsupported and the token deployment excluded: unsupported=%+v excluded=%+v", env.Unsupported, env.Excluded)
	}
	if !env.Incomplete {
		t.Fatal("an estimate with an unpriced PTU deployment is incomplete")
	}
}
