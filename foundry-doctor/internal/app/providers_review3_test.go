package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestSelectedByNameAndExpandedKeys(t *testing.T) {
	body := "AZURE_LOCATION=${UNDEFINED_REGION}\nPLAIN=v\nQUOTED='${NOT_EXPANDED}'\nX=$A$B\nX=plain\nAZURE_ENV_NAME=dev\n"
	mk := func(environment, def string) *envStore {
		dir := envProject(t, map[string]*string{"dev": str(body), "test": str("A=1\n")})
		return &envStore{src: Source{Dir: dir, Environment: environment, DefaultEnvironment: def, Environments: []string{"dev", "test"}}}
	}
	if !mk("dev", "test").SelectedByName() {
		t.Error("an environment chosen by name is selected by name")
	}
	if mk("dev", "dev").SelectedByName() {
		t.Error("the default environment is not counted as selected by name")
	}
	if mk("", "").SelectedByName() {
		t.Error("no environment is not selected by name")
	}
	if mk("missing", "").SelectedByName() {
		t.Error("an environment with no .env directory is not selected")
	}
	got := mk("dev", "").ExpandedKeys()
	if !reflect.DeepEqual(got, map[string]bool{"AZURE_LOCATION": true}) {
		t.Fatalf("ExpandedKeys = %v (last assignment wins, single quotes do not expand)", got)
	}
	if v := mk("dev", "").Values()["AZURE_LOCATION"]; v != "" {
		t.Fatalf("the parser should expand an undefined variable to empty, got %q", v)
	}
}

// ENV-002 end to end: a resource ID under a secret-looking key name is still
// compared by shape and never reaches the evidence as a value.
func TestENV002SecretNamedKeyHoldingResourceID(t *testing.T) {
	const id = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/kv"
	files := map[string]*string{
		"dev":  str("AZURE_ENV_NAME=dev\nAZURE_KEYVAULT_ID=" + id + "\n"),
		"prod": str("AZURE_ENV_NAME=prod\nAZURE_KEYVAULT_ID=" + id + "\n"),
	}
	s := &envStore{src: Source{Dir: envProject(t, files), Environments: []string{"dev", "prod"}}}
	for _, r := range env.RegisterWith(s) {
		if r.ID() != "FND-ENV-002" {
			continue
		}
		res, err := r.Evaluate(context.Background(), &sdk.Input{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Findings) != 1 || !strings.Contains(res.Findings[0].Evidence, "AZURE_KEYVAULT_ID") {
			t.Fatalf("want one AZURE_KEYVAULT_ID finding, got %+v skip=%+v", res.Findings, res.Skipped)
		}
		if strings.Contains(res.Findings[0].Evidence, "00000000") {
			t.Fatalf("evidence leaks the value: %s", res.Findings[0].Evidence)
		}
	}
}
