package graphreport

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	coregraph "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "update golden files and samples/graph")

// sampleInput is a recorded graph input: azure.yaml text, template resources
// as the flattened ARM model produces them, a Resources - List inventory and
// a Foundry runtime overlay.
type sampleInput struct {
	Mode      view.Mode            `json:"mode"`
	Scope     string               `json:"scope"`
	AzureYAML string               `json:"azureYaml"`
	Resources []coregraph.Resource `json:"resources"`
	Inventory []azure.Resource     `json:"inventory"`
	Runtime   view.RuntimeInput    `json:"runtime"`
	Findings  []sdk.Finding        `json:"findings"`
}

type sampleFixture struct {
	Description string `json:"description"`
	sampleInput
	DiffBase   *sampleInput `json:"diffBase"`
	DiffTarget *sampleInput `json:"diffTarget"`
}

func buildSample(t *testing.T, in sampleInput) view.Graph {
	t.Helper()
	var src view.SourceInput
	if in.AzureYAML != "" {
		d, err := azureyaml.Parse([]byte(in.AzureYAML), "azure.yaml")
		if err != nil {
			t.Fatal(err)
		}
		src.Document = d
		for _, name := range d.ServiceNames() {
			host, _, _ := d.Lookup("services", name, "host")
			var uses []string
			for _, ref := range d.Uses(name) {
				uses = append(uses, ref.Name)
			}
			src.Services = append(src.Services, coregraph.Service{Name: name, Host: host, Uses: uses})
		}
	}
	src.Resources = in.Resources
	g, err := view.Build(view.Input{Source: src, Inventory: in.Inventory, Runtime: in.Runtime, Findings: in.Findings},
		view.Options{Mode: in.Mode, Scope: in.Scope, CollapseAfter: 80})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestSamples regenerates every samples/graph/<name>.md from
// samples/graph/fixtures/<name>.json and fails if a committed sample drifts.
func TestSamples(t *testing.T) {
	root := filepath.Join("..", "..", "..", "samples", "graph")
	fixtures, err := filepath.Glob(filepath.Join(root, "fixtures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) < 4 {
		t.Fatalf("expected at least 4 graph sample fixtures, found %d", len(fixtures))
	}
	for _, path := range fixtures {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fx sampleFixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			var g view.Graph
			if fx.DiffBase != nil || fx.DiffTarget != nil {
				if fx.DiffBase == nil || fx.DiffTarget == nil {
					t.Fatal("diff fixture needs diffBase and diffTarget")
				}
				g, err = view.Diff(buildSample(t, *fx.DiffBase), buildSample(t, *fx.DiffTarget), view.Options{})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				g = buildSample(t, fx.sampleInput)
			}
			got := "<!-- Generated from samples/graph/fixtures/" + name + ".json by TestSamples in internal/report/graph; regenerate with `go test ./internal/report/graph -run TestSamples -update`. -->\n\n" + string(Markdown(g))
			out := filepath.Join(root, name+".md")
			if *update {
				if err := os.WriteFile(out, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("missing sample %s: %v", out, err)
			}
			if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
				t.Fatalf("sample %s drifted from its fixture\n--- got ---\n%s\n--- want ---\n%s", out, got, want)
			}
		})
	}
}
