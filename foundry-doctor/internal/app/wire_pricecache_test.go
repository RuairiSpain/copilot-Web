package app

import (
	"path/filepath"
	"testing"
)

// The COST-004 estimate must read the cache the `cost` command writes for the same project directory,
// not a cache relative to the process working directory.
func TestPriceCachePathFollowsProjectDir(t *testing.T) {
	dir := filepath.Join("some", "project")
	want := filepath.Join(dir, ".foundry-doctor", "cache", "prices.json")
	if got := priceCachePath(dir); got != want {
		t.Fatalf("priceCachePath(%q) = %q, want %q", dir, got, want)
	}
	req := CostRequest{Dir: dir}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	if req.PriceCache != priceCachePath(dir) {
		t.Fatalf("cost command cache %q differs from the COST-004 cache %q", req.PriceCache, priceCachePath(dir))
	}
	if got, want := priceCachePath(""), filepath.Join(".", ".foundry-doctor", "cache", "prices.json"); got != want {
		t.Fatalf("empty dir: %q, want %q", got, want)
	}
}
