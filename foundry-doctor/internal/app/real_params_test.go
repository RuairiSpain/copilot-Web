//go:build spike

package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike -run TestRealBicep ./internal/app/
//
// It compiles a real project (the rulestest twice and params sources) with the
// Bicep CLI: module file locations by templateHash, bicepparam through
// `bicep build-params` with readEnvironmentVariable fed from the selected azd
// environment. Without BICEP_PATH it skips.
func TestRealBicepParamsAndModules(t *testing.T) {
	exe := os.Getenv("BICEP_PATH")
	if exe == "" {
		if os.Getenv("REQUIRE_SPIKE_DEPS") == "1" {
			t.Fatal("BICEP_PATH is required when REQUIRE_SPIKE_DEPS=1")
		}
		t.Skip("BICEP_PATH not set")
	}
	dir := t.TempDir()
	copyTree := func(src, dst string) {
		err := filepath.WalkDir(filepath.Join(testdataRulestest, src), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".bicep") {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join(testdataRulestest, src), p)
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			writeFile(t, dir, filepath.ToSlash(filepath.Join(dst, rel)), string(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	copyTree("params", "infra")
	writeFile(t, dir, "azure.yaml", "name: demo\n")
	writeFile(t, dir, ".azure/dev/.env", "AZURE_ENV_NAME=\"envdev\"\nAZURE_LOCATION=\"swedencentral\"\n")
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\n"+
		"param prefix = readEnvironmentVariable('AZURE_ENV_NAME')\n"+
		"param location = readEnvironmentVariable('AZURE_LOCATION')\n"+
		"param deploySearch = true\n")
	var log bytes.Buffer
	l := armLoader{stderr: &log}
	l.disc.Path = exe
	m, err := l.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra", Environment: "dev"})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	ai := resource(t, m, tAI, "envdev-ai")
	if ai.Region != "swedencentral" {
		t.Errorf("region %q", ai.Region)
	}
	mod := resource(t, m, tSearch, "envdev-msrch")
	if mod.Location.File != "infra/modules/search.bicep" {
		t.Errorf("module resource located at %q", mod.Location.File)
	}
	if strings.Contains(log.String(), "not used") {
		t.Errorf("log %q", log.String())
	}
}
