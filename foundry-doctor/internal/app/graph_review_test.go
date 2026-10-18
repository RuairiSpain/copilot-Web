package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	graphviz "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/graphviz"
	graphview "github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

// A permission failure on one runtime overlay skips that overlay with a note;
// the graph is still rendered (rendering must not block on optional evidence).
func TestGraphOverlayPermissionFailureDegrades(t *testing.T) {
	old := graphClient
	ff := &fakeFoundry{projectHostsErr: fmt.Errorf("%w: 403 on capabilityHosts/read", azure.ErrUnavailable)}
	graphClient = func() (azure.Client, error) {
		return azure.Client{
			Inventory: fakeInventory{list: azure.ResourceList{Resources: []azure.Resource{
				{ID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/acct", Name: "acct", Type: "Microsoft.CognitiveServices/accounts"},
			}}},
			Foundry: ff,
		}, nil
	}
	defer func() { graphClient = old }()

	svc, _ := newSvc(&fakeEngine{})
	var stderr, out bytes.Buffer
	svc.Stderr = &stderr
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeCombined,
		Format: "json",
		Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct", Project: "proj"},
	}, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	var g graphview.Graph
	if err := json.Unmarshal(out.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Summary.Notes) != 1 || !strings.Contains(g.Summary.Notes[0], "project capability hosts overlay skipped") {
		t.Fatalf("want a skipped-overlay note, got %+v", g.Summary)
	}
	for _, n := range g.Nodes {
		if n.Kind == "capability-host" {
			t.Fatalf("skipped overlay must contribute no nodes: %+v", g.Nodes)
		}
	}
	if !strings.Contains(stderr.String(), "overlay skipped") {
		t.Fatalf("want a stderr warning, got %q", stderr.String())
	}
}

func TestGraphWarnsWhenAccountNotInInventory(t *testing.T) {
	old := graphClient
	graphClient = func() (azure.Client, error) {
		return azure.Client{Inventory: fakeInventory{}, Foundry: &fakeFoundry{}}, nil
	}
	defer func() { graphClient = old }()
	svc, _ := newSvc(&fakeEngine{})
	var stderr bytes.Buffer
	svc.Stderr = &stderr
	code, err := Graph(context.Background(), svc, GraphRequest{
		Mode:   graphview.ModeDeployed,
		Format: "json",
		Target: GraphTarget{SubscriptionID: "sub", ResourceGroup: "rg", Account: "acct"},
	}, io.Discard)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !strings.Contains(stderr.String(), `Foundry account "acct" is not in the Azure inventory`) {
		t.Fatalf("missing warning: %q", stderr.String())
	}
}

// The optional Graphviz adapter is absent: svg/png report the optional
// feature unavailable instead of failing internally.
func TestGraphSVGWithoutGraphvizIsUnavailable(t *testing.T) {
	old := graphvizRenderer
	graphvizRenderer = graphviz.Binary{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	defer func() { graphvizRenderer = old }()
	svc, _ := newSvc(&fakeEngine{})
	for _, format := range []string{"svg", "png"} {
		code, err := Graph(context.Background(), svc, GraphRequest{Format: format}, io.Discard)
		if code != ExitUnavailable || err == nil || !strings.Contains(err.Error(), "Graphviz") {
			t.Fatalf("%s: code=%d err=%v", format, code, err)
		}
	}
}

func writeGraphJSON(t *testing.T, dir, name string, g graphview.Graph) string {
	t.Helper()
	data, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGraphEnvironmentDiff(t *testing.T) {
	dir := t.TempDir()
	base := writeGraphJSON(t, dir, "dev.json", graphview.Graph{SchemaVersion: graphview.SchemaVersion, Mode: graphview.ModeDeployed,
		Nodes: []graphview.Node{{ID: "n1", Label: "st", Kind: "storage", Origin: graphview.OriginAzure, Type: "Microsoft.Storage/storageAccounts", Name: "st"}}})
	target := writeGraphJSON(t, dir, "prod.json", graphview.Graph{SchemaVersion: graphview.SchemaVersion, Mode: graphview.ModeDeployed,
		Nodes: []graphview.Node{
			{ID: "n1", Label: "st", Kind: "storage", Origin: graphview.OriginAzure, Type: "Microsoft.Storage/storageAccounts", Name: "st"},
			{ID: "n2", Label: "srch", Kind: "search", Origin: graphview.OriginAzure, Type: "Microsoft.Search/searchServices", Name: "srch"},
		}})
	svc, _ := newSvc(&fakeEngine{})
	svc.Project = fakeProject{err: errors.New("diff mode must not load source")}
	var out bytes.Buffer
	code, err := Graph(context.Background(), svc, GraphRequest{Format: "markdown", DiffBase: base, DiffTarget: target}, &out)
	if err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	for _, want := range []string{"# Foundry Doctor graph (diff)", "- Added nodes: 1", "class n1 added"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
}

func TestGraphEnvironmentDiffValidation(t *testing.T) {
	svc, _ := newSvc(&fakeEngine{})
	cases := []GraphRequest{
		{Format: "json", DiffBase: "a.json"},
		{Format: "json", DiffBase: "a.json", DiffTarget: "b.json", Target: GraphTarget{SubscriptionID: "sub"}},
		{Mode: graphview.ModeSource, Format: "json", DiffBase: "a.json", DiffTarget: "b.json"},
		{Format: "json", DiffBase: "missing-a.json", DiffTarget: "missing-b.json"},
	}
	for i, req := range cases {
		code, err := Graph(context.Background(), svc, req, io.Discard)
		if err == nil || code != ExitUnavailable {
			t.Errorf("case %d: code=%d err=%v", i, code, err)
		}
	}
}

// Connection credentials and secret-bearing URL query strings never reach any
// output format.
func TestGraphNeverEmitsConnectionSecrets(t *testing.T) {
	sig := "FAKESIGVALUE"
	cred := "FAKECREDVALUE"
	apiKey := "FAKEAPIKEYVALUE"
	yaml := "name: s\nservices:\n  docs-conn:\n    host: azure.ai.connection\n" +
		"    target: https://contoso.blob.core.windows.net/docs?sv=2024&sig=" + sig + "\n" +
		"    credentials:\n      key: " + cred + "\n"
	d, err := azureyaml.Parse([]byte(yaml), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	g, err := graphview.Build(graphview.Input{
		Source: graphview.SourceInput{Document: d, Services: sourceServices(d)},
		Runtime: graphview.RuntimeInput{Connections: []azure.FoundryConnection{{
			Name: "rt-conn", Error: "401 for key " + apiKey,
			Target: azure.ConnectionTarget{Endpoint: "https://x.openai.azure.com/?api" + "-key=" + apiKey},
		}}},
	}, graphview.Options{Mode: graphview.ModeCombined})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"mermaid", "json", "markdown", "dot", "html"} {
		out, err := renderGraph(context.Background(), g, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{sig, cred, apiKey} {
			if strings.Contains(string(out), secret) {
				t.Errorf("%s output leaks %q:\n%s", format, secret, out)
			}
		}
	}
}
