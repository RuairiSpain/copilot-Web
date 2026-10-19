package app

import (
	"math"
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

// Phase 7 gate (black belt N1): --hours-per-month is finite and at most the
// hours of the longest month.
func TestCostRequestValidateHoursPerMonth(t *testing.T) {
	for _, h := range []float64{-1, 0.5, 744.5, 100000, math.NaN(), math.Inf(1)} {
		r := CostRequest{HoursPerMonth: h}
		if err := r.Validate(); err == nil {
			t.Errorf("hours-per-month %v must be rejected", h)
		}
	}
	for _, h := range []float64{0, 1, 730, 744} {
		r := CostRequest{HoursPerMonth: h}
		if err := r.Validate(); err != nil {
			t.Errorf("hours-per-month %v must be accepted: %v", h, err)
		}
	}
}
