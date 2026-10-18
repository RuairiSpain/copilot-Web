package app

import (
	"path/filepath"
	"testing"
)

func TestCostRequestValidateCurrencyAndCachePath(t *testing.T) {
	r := CostRequest{Dir: "proj", Currency: " eur "}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", r.Currency)
	}
	if want := filepath.Join("proj", ".foundry-doctor", "cache", "prices.json"); r.PriceCache != want {
		t.Errorf("price cache = %q, want %q (under --dir)", r.PriceCache, want)
	}
	for _, bad := range []string{"EURO", "E1R", "$$$"} {
		r := CostRequest{Currency: bad}
		if err := r.Validate(); err == nil {
			t.Errorf("currency %q must be rejected", bad)
		}
	}
	keep := CostRequest{PriceCache: "custom.json"}
	if err := keep.Validate(); err != nil || keep.PriceCache != "custom.json" || keep.Currency != "USD" {
		t.Errorf("explicit cache path and default currency: %+v err=%v", keep, err)
	}
}
