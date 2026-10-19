package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goldenDoc() Document {
	return Document{
		Advisory: "A.",
		Environments: []EnvironmentReport{{
			Environment:    "dev",
			Profile:        "foundry-dev",
			Currency:       "USD",
			HoursPerMonth:  730,
			PriceRetrieved: "2026-10-18T00:00:00Z",
			Estimated: []LineItem{{
				ResourceName: "s", ResourceType: "Microsoft.Search/searchServices", Kind: "search-units", Region: "eastus",
				MeterName: "Standard S1 Unit", SKUName: "standard", Quantity: 1, Unit: "unit", UnitPrice: 0.336,
				MonthlyLow: 245.28, MonthlyHigh: 245.28, PriceDate: "2015-05-01T00:00:00Z", Notes: []string{"n1"},
			}},
			Excluded:    []NoteItem{{ResourceName: "chat", ResourceType: "T", Reason: "token-based"}},
			Unsupported: []NoteItem{{ResourceName: "ptu", ResourceType: "T", Reason: "PTU"}},
			TotalLow:    245.28,
			TotalHigh:   245.28,
			Incomplete:  true,
		}},
		Comparisons: []Comparison{{Left: "dev", Right: "prod", Reason: "x"}},
	}
}

func TestRenderGolden(t *testing.T) {
	for _, format := range []string{"console", "markdown"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Render(&buf, format, goldenDoc()); err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", format+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if buf.String() != string(want) {
				t.Fatalf("%s output differs from golden:\n--- got ---\n%s\n--- want ---\n%s", format, buf.String(), want)
			}
		})
	}
}

func TestRenderJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, "json", goldenDoc()); err != nil {
		t.Fatal(err)
	}
	var got Document
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	env := got.Environments[0]
	if env.TotalHigh != 245.28 || !env.Incomplete || len(env.Excluded) != 1 || len(env.Unsupported) != 1 || env.Estimated[0].Notes[0] != "n1" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestRenderRejectsUnknownFormat(t *testing.T) {
	if err := Render(&bytes.Buffer{}, "xml", goldenDoc()); err == nil {
		t.Fatal("want an error for an unknown format")
	}
}

// Phase 7 gate: the console total says when stale prices are in it.
func TestConsoleTotalFlagsStalePricing(t *testing.T) {
	doc := goldenDoc()
	doc.Environments[0].StalePricing = true
	var buf bytes.Buffer
	if err := Render(&buf, "console", doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "USD/month (includes stale prices) (estimate incomplete)") {
		t.Fatalf("total line lacks the stale flag:\n%s", buf.String())
	}
}
