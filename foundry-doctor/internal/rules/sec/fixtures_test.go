package sec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func fixture(t testing.TB, supplied obj, parts ...string) *model.Input {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "..", "test", "fixtures"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return normalise(t, raw, supplied)
}

func TestFixture_FoundryData(t *testing.T) {
	in := fixture(t, nil, "rules", "sec", "foundry-data.arm.json")
	tests := []struct {
		id   string
		want [4]int
	}{
		{"FND-SEC-001", [4]int{1, 0, 0, 0}},
		{"FND-SEC-002", [4]int{1, 0, 0, 1}}, // fdsearch passes (default true); the unrelated service is out of scope
		{"FND-SEC-003", [4]int{0, 0, 0, 1}}, // cosmosKeysOff has no default: skipped
		{"FND-SEC-004", [4]int{4, 0, 0, 0}},
		{"FND-SEC-006", [4]int{1, 1, 0, 0}}, // storage clean, cosmos 0.0.0.0 open
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			count(eval(t, tt.id, in)).want(t, tt.want[0], tt.want[1], tt.want[2], tt.want[3])
		})
	}
	t.Run("supplied parameter values change the outcome", func(t *testing.T) {
		in := fixture(t, obj{"cosmosKeysOff": false, "searchKeysOff": false}, "rules", "sec", "foundry-data.arm.json")
		count(eval(t, "FND-SEC-003", in)).want(t, 0, 1, 0, 0)
		count(eval(t, "FND-SEC-002", in)).want(t, 0, 1, 0, 1)
	})
}

func TestFixture_BicepFoundry(t *testing.T) {
	// Committed compiled Bicep fixtures: accounts with literal, default-parameter, no-default and
	// absent disableLocalAuth. Only the literal account owns a project.
	for _, form := range []string{"foundry", "foundry-v2"} {
		t.Run(form, func(t *testing.T) {
			c := count(eval(t, "FND-SEC-001", fixture(t, nil, "bicep", form, "foundry.arm.json")))
			if c.pass < 1 {
				t.Fatalf("expected the literal-true hosting account to pass: %+v", c)
			}
		})
	}
	t.Run("synthetic template has no storage, search or cosmos in scope", func(t *testing.T) {
		in := fixture(t, nil, "bicep", "synthetic", "main.arm.json")
		for _, id := range []string{"FND-SEC-002", "FND-SEC-003", "FND-SEC-004"} {
			count(eval(t, id, in)).want(t, 0, 0, 0, 0)
		}
	})
}
