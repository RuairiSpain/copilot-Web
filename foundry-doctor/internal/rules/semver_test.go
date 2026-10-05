package rules_test

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

func TestInRange(t *testing.T) {
	const rng = ">=1.34.2 <=1.36.0-beta.1"
	tests := []struct {
		v, rng  string
		want    bool
		wantErr bool
	}{
		{"1.34.2", rng, true, false},
		{"1.35.0", rng, true, false},
		{"v1.35.0+build5", rng, true, false},
		{"1.36.0-beta.1", rng, true, false},
		{"1.36.0-alpha.9", rng, true, false},
		{"1.36.0-beta.2", rng, false, false},
		{"1.36.0", rng, false, false},
		{"1.34.1", rng, false, false},
		{"2.0.0", rng, false, false},
		{"1.0.0-beta.13", ">=1.0.0-beta.13 <=1.0.0-beta.13", true, false},
		{"1.0.0-beta.2", ">=1.0.0-beta.11 <=1.0.0-beta.13", false, false}, // numeric ids compare as numbers
		{"1.0.0-1", ">=1.0.0-alpha", false, false},                        // numeric sorts below alphanumeric
		{"1.0.0", "1.0.0", true, false},
		{"1.0.1", "=1.0.0", false, false},
		{"1.0.1", ">1.0.0 <1.0.2", true, false},
		{"1.0.0-rc", "<1.0.0", true, false},
		{"1.0.0-rc.1", ">1.0.0-rc", true, false},
		{"abc", rng, false, true},
		{"1.2", rng, false, true},
		{"01.2.3", rng, false, true},
		{"1.2.3-", rng, false, true},
		{"1.2.3", "", false, true},
		{"1.2.3", ">=x", false, true},
	}
	for _, tc := range tests {
		got, err := rules.InRange(tc.v, tc.rng)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("InRange(%q, %q) = %v, %v; want %v, err %v", tc.v, tc.rng, got, err, tc.want, tc.wantErr)
		}
	}
}
