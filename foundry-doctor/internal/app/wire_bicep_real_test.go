//go:build spike

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike -run TestRealBicepDiagnosticsBecomeFindings ./internal/app/
//
// It compiles a Bicep file that trips the no-unused-params and
// no-hardcoded-env-urls linter rules with the real CLI and checks the adapter
// findings. Without BICEP_PATH it skips; REQUIRE_SPIKE_DEPS=1 turns that into a
// failure.
func TestRealBicepDiagnosticsBecomeFindings(t *testing.T) {
	exe := os.Getenv("BICEP_PATH")
	if exe == "" {
		if os.Getenv("REQUIRE_SPIKE_DEPS") == "1" {
			t.Fatal("BICEP_PATH is required when REQUIRE_SPIKE_DEPS=1")
		}
		t.Skip("BICEP_PATH not set")
	}
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, "infra/main.bicep", "param unusedParam string = 'x'\n"+
		"var url = 'https://management.azure.com/foo'\n"+
		"resource sa 'Microsoft.Storage/storageAccounts@2023-01-01' = {\n"+
		"  name: 'st${uniqueString(resourceGroup().id)}'\n"+
		"  location: resourceGroup().location\n"+
		"  sku: { name: 'Standard_LRS' }\n"+
		"  kind: 'StorageV2'\n"+
		"}\n"+
		"output u string = url\n")
	store := &adapterStore{}
	l := armLoader{adapter: store}
	l.disc.Path = exe
	src := Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}
	if _, err := l.Load(context.Background(), src); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := map[string]int{}
	for _, f := range store.take(dir) {
		got[f.RuleID] = f.Location.Line
		if f.Location.File != filepath.ToSlash("infra/main.bicep") || f.Adapter != "bicep" || f.Fingerprint == "" {
			t.Errorf("unexpected finding %+v", f)
		}
	}
	if got["bicep/no-unused-params"] != 1 || got["bicep/no-hardcoded-env-urls"] != 2 {
		t.Fatalf("findings by rule and line = %v", got)
	}
}
