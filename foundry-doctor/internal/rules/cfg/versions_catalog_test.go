package cfg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

// testedMax must equal the upper bound of compatibility.extensions that the catalogue
// declares for FND-CFG-001 and FND-CFG-011, the two rules whose windows define it.
func TestTestedMaxMatchesCatalogueWindows(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	rules, err := catalog.Load(context.Background(), os.DirFS(root), "rules/catalog")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if r.ID != "FND-CFG-001" && r.ID != "FND-CFG-011" {
			continue
		}
		for ext, rng := range r.Compatibility.Extensions {
			want, ok := testedMax[ext]
			if !ok {
				continue
			}
			seen[ext] = true
			if !strings.HasSuffix(rng.String(), "<="+want) || !rng.Contains(want) {
				t.Errorf("%s %s window = %q, want an inclusive upper bound equal to testedMax %q", r.ID, ext, rng.String(), want)
			}
		}
	}
	for ext := range testedMax {
		if !seen[ext] {
			t.Errorf("testedMax[%s] has no FND-CFG-001 or FND-CFG-011 window to compare with", ext)
		}
	}
}
