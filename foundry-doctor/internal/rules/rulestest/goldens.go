package rulestest

import (
	"embed"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// goldenFS holds the small single-purpose goldens in goldens/. Each X.json is
// compiled from the X.bicep next to it with Bicep CLI 0.48.1.
//
//go:embed goldens/*.json
var goldenFS embed.FS

// Golden loads goldens/<name>.json (for example "netinj", "idn", "tags") into
// the rule input model.
func Golden(t testing.TB, name string) sdk.ARMModel {
	t.Helper()
	b, err := goldenFS.ReadFile("goldens/" + name + ".json")
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	m, err := armmodel.FromARM(b, armmodel.Options{Entry: name + ".bicep"})
	if err != nil {
		t.Fatalf("load golden %s: %v", name, err)
	}
	return m
}
