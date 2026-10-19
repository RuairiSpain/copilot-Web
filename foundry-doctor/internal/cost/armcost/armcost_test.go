package armcost

import "testing"

func TestLiteralRegionNormalisesDisplayNames(t *testing.T) {
	for in, want := range map[string]string{"eastus": "eastus", "East US": "eastus", " West Europe ": "westeurope"} {
		if got, ok := LiteralRegion(in); !ok || got != want {
			t.Errorf("LiteralRegion(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := LiteralRegion("[parameters('location')]"); ok {
		t.Error("an expression is not a literal region")
	}
}

func TestSovereignRegionIgnoresSpaces(t *testing.T) {
	for in, want := range map[string]bool{"usgovvirginia": true, "US Gov Virginia": true, "China North 3": true, "East US": false} {
		if got := SovereignRegion(in); got != want {
			t.Errorf("SovereignRegion(%q) = %v, want %v", in, got, want)
		}
	}
}
