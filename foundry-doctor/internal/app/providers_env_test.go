package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// envProject writes .azure/<name>/.env files (nil body = directory only).
func envProject(t *testing.T, files map[string]*string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		d := filepath.Join(dir, ".azure", name)
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
		if body != nil {
			if err := os.WriteFile(filepath.Join(d, ".env"), []byte(*body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func str(s string) *string { return &s }

func TestReadEnvFileStatuses(t *testing.T) {
	var many strings.Builder
	for i := 0; i <= maxEnvKeys; i++ {
		fmt.Fprintf(&many, "K%d=v\n", i)
	}
	tests := []struct {
		name string
		body *string
		want env.Status
	}{
		{"ok", str("A=1\n"), env.StatusOK},
		{"missing file", nil, env.StatusMissing},
		{"over-size", str(strings.Repeat("A=1\n", maxEnvFileBytes/4+10)), env.StatusOversize},
		{"too many keys", str(many.String()), env.StatusTruncated},
		{"unparseable line", str("A=1\nNOT A LINE\nB=2\n"), env.StatusUnparseable},
		{"unterminated quote", str("A=\"x\n"), env.StatusUnparseable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := envProject(t, map[string]*string{"dev": tc.body})
			s := &envStore{src: Source{Dir: dir, Environments: []string{"dev"}}}
			envs := s.Environments()
			if len(envs) != 1 || envs[0].Status != tc.want {
				t.Fatalf("envs=%+v want status %q", envs, tc.want)
			}
			if tc.want != env.StatusOK && len(envs[0].Values) != 0 {
				t.Fatalf("unusable environment must carry no values, got %d", len(envs[0].Values))
			}
		})
	}
}

func TestReadEnvFileNotRegularIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".azure", "dev", ".env"), 0o750); err != nil {
		t.Fatal(err)
	}
	s := &envStore{src: Source{Dir: dir, Environments: []string{"dev"}}}
	if st := s.Environments()[0].Status; st != env.StatusUnreadable {
		t.Fatalf("status=%q", st)
	}
}

func TestEnvStoreUnusableEnvironmentIsNotSelected(t *testing.T) {
	dir := envProject(t, map[string]*string{"dev": str("A=\"x\n")})
	s := &envStore{src: Source{Dir: dir, Environment: "dev", Environments: []string{"dev"}}}
	if _, ok := s.Selected(); ok {
		t.Fatal("an environment with an unparseable .env must not be selected")
	}
}

func TestEnvFileGrammarAndRedaction(t *testing.T) {
	const expanded = "expanded-value-1"
	body := "BASE=" + expanded + "\nMY_API_KEY=\"$BASE\"\nNAME: 'single $BASE'\nexport AZURE_LOCATION=eastus # c\n"
	dir := envProject(t, map[string]*string{"dev": str(body)})
	s := &envStore{src: Source{Dir: dir, Environment: "dev", Environments: []string{"dev"}}}
	v := s.Values()
	if v["AZURE_LOCATION"] != "eastus" || v["NAME"] != "single $BASE" {
		t.Fatalf("values=%v", v)
	}
	if got := v["MY_API_KEY"]; !strings.HasPrefix(got, "sha256:") || got != digest(expanded) {
		t.Fatalf("expansion must happen before the digest: %q", got)
	}
}

func TestLiteralResourceGroups(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want []string
	}{
		{"top-level literal", "name: x\nresourceGroup: rg-fixed\n", []string{"resourceGroup"}},
		{"top-level variable", "name: x\nresourceGroup: rg-${AZURE_ENV_NAME}\n", nil},
		{"service literal", "name: x\nservices:\n  web:\n    host: containerapp\n    resourceGroup: rg-shared\n  api:\n    host: containerapp\n    resourceGroup: rg-${AZURE_ENV_NAME}\n", []string{"services.web.resourceGroup"}},
		{"top-level and service", "name: x\nresourceGroup: rg-a\nservices:\n  web:\n    host: containerapp\n    resourceGroup: rg-b\n", []string{"resourceGroup", "services.web.resourceGroup"}},
		{"none", "name: x\nservices:\n  web:\n    host: containerapp\n", nil},
		{"blank value", "name: x\nresourceGroup: \"\"\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := azureyaml.Parse([]byte(tc.yaml), "azure.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if got := literalResourceGroups(doc); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	if got := literalResourceGroups(nil); got != nil {
		t.Fatalf("nil view: %v", got)
	}
}

func evalEnvRules(t *testing.T, dir string, names []string, view sdk.AzureYAMLView) map[string]sdk.Result {
	t.Helper()
	s := &envStore{src: Source{Dir: dir, Environments: names}, view: view}
	out := map[string]sdk.Result{}
	for _, r := range env.RegisterWith(s) {
		res, err := r.Evaluate(context.Background(), &sdk.Input{})
		if err != nil {
			t.Fatal(err)
		}
		out[r.ID()] = res
	}
	return out
}

func TestEnvRulesNeverPassOnUnreadableEnvironments(t *testing.T) {
	good := "AZURE_ENV_NAME=dev\nAZURE_SUBSCRIPTION_ID=s\nAZURE_RESOURCE_GROUP=rg-dev\n"
	tests := []struct {
		name   string
		files  map[string]*string
		reason string
	}{
		{"missing .env", map[string]*string{"dev": str(good), "prod": nil}, "environment prod: .env missing"},
		{"unparseable .env", map[string]*string{"dev": str(good), "prod": str("A=\"x\n")}, "environment prod: .env unparseable"},
		{"over-size .env", map[string]*string{"dev": str(good), "prod": str(strings.Repeat("A=1\n", maxEnvFileBytes/4+10))}, "environment prod: .env over-size"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := envProject(t, tc.files)
			res := evalEnvRules(t, dir, []string{"dev", "prod"}, nil)
			for _, id := range []string{"FND-ENV-001", "FND-ENV-002", "FND-ENV-004"} {
				r := res[id]
				if r.Skipped == nil || len(r.Findings) != 0 {
					t.Fatalf("%s must skip, got %+v", id, r)
				}
				if !strings.HasPrefix(r.Skipped.Reason, report.UncertainPrefix) || !strings.Contains(r.Skipped.Reason, tc.reason) {
					t.Fatalf("%s reason %q lacks %q", id, r.Skipped.Reason, tc.reason)
				}
			}
		})
	}
}

func TestEnvRulesEndToEndFindings(t *testing.T) {
	a := "AZURE_ENV_NAME=dev\nAZURE_SUBSCRIPTION_ID=s\nAZURE_RESOURCE_GROUP=rg-shared\nAZURE_AI_PROJECT_ENDPOINT=https://a.services.ai.azure.com/api/projects/p\nAZURE_TENANT_ID=t\n"
	b := strings.Replace(a, "AZURE_ENV_NAME=dev", "AZURE_ENV_NAME=prod", 1)
	dir := envProject(t, map[string]*string{"dev": str(a), "prod": str(b)})
	doc, err := azureyaml.Parse([]byte("name: x\nservices:\n  web:\n    host: containerapp\n    resourceGroup: rg-fixed\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	res := evalEnvRules(t, dir, []string{"dev", "prod"}, doc)
	if n := len(res["FND-ENV-001"].Findings); n != 1 {
		t.Fatalf("ENV-001 findings=%d: %+v", n, res["FND-ENV-001"])
	}
	var keys []string
	for _, f := range res["FND-ENV-002"].Findings {
		keys = append(keys, f.Resource.Name)
	}
	want := []string{"AZURE_AI_PROJECT_ENDPOINT", "AZURE_RESOURCE_GROUP", "AZURE_SUBSCRIPTION_ID"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("ENV-002 keys=%v want %v (AZURE_TENANT_ID must not be reported)", keys, want)
	}
}

type tierPolicy map[string]any

func (p tierPolicy) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

// A Key Vault resource ID shared by prod and non-prod is reported whatever the
// key is called; the secret value under the same key stays out of evidence.
func TestEnvRulesClassifyBeforeDigest(t *testing.T) {
	const kv = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/kv1"
	for _, key := range []string{"AZURE_KEYVAULT_ID", "AZURE_VAULT_ID"} {
		t.Run(key, func(t *testing.T) {
			mk := func(n string) *string {
				return str("AZURE_ENV_NAME=" + n + "\n" + key + "=" + kv + "\n")
			}
			dir := envProject(t, map[string]*string{"dev": mk("dev"), "prod": mk("prod")})
			s := &envStore{src: Source{Dir: dir, Environments: []string{"dev", "prod"}}}
			pol := tierPolicy{env.TierPolicyKey: map[string]any{"dev": "dev", "prod": "prod"}}
			for _, r := range env.RegisterWith(s) {
				if r.ID() != "FND-ENV-003" {
					continue
				}
				res, err := r.Evaluate(context.Background(), &sdk.Input{Policy: pol})
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Findings) != 1 {
					t.Fatalf("ENV-003 findings=%d skip=%+v", len(res.Findings), res.Skipped)
				}
				if strings.Contains(res.Findings[0].Evidence, "00000000") {
					t.Fatalf("evidence leaks the value: %s", res.Findings[0].Evidence)
				}
			}
		})
	}
}

func TestReadEnvFileShapes(t *testing.T) {
	dir := envProject(t, map[string]*string{"dev": str("AZURE_KEYVAULT_ID=/subscriptions/1/x\nAPI_KEY=" + "sk" + "-abcdef123456\nSAS_URL=https://x.blob.core.windows.net/c?" + "sig" + "=abc\nPLAIN=v\nEP_TOKEN_URL=https://a.services.ai.azure.com\n")})
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	vals, shapes, _, st := readEnvFile(root, ".azure/dev/.env")
	if st != env.StatusOK {
		t.Fatal(st)
	}
	want := map[string]env.Shape{"AZURE_KEYVAULT_ID": env.ShapeResourceID, "EP_TOKEN_URL": env.ShapeFoundryEndpoint}
	if !reflect.DeepEqual(shapes, want) {
		t.Fatalf("shapes=%v want %v", shapes, want)
	}
	if !strings.HasPrefix(vals["API_KEY"], "sha256:") || !strings.HasPrefix(vals["AZURE_KEYVAULT_ID"], "sha256:") || vals["PLAIN"] != "v" {
		t.Fatalf("vals=%v", vals)
	}
}

func TestSelectionReasonDistinguishesCauses(t *testing.T) {
	mk := func(src Source, envs map[string]*string) *envStore {
		dir := envProject(t, envs)
		src.Dir = dir
		for name := range envs {
			src.Environments = append(src.Environments, name)
		}
		return &envStore{src: src}
	}
	cases := []struct {
		name string
		src  Source
		envs map[string]*string
		want string
	}{
		{"none", Source{}, map[string]*string{"dev": str("A=1\n")}, "no-environment-selected"},
		{"default names no directory", Source{DefaultEnvironment: "nope"}, map[string]*string{"dev": str("A=1\n")}, "default-environment-not-found"},
		{"selected env file missing", Source{Environment: "dev"}, map[string]*string{"dev": nil}, "env-file-missing"},
		{"selected env file unparseable", Source{Environment: "dev"}, map[string]*string{"dev": str("A=\"unterminated\n")}, "env-file-unparseable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := mk(c.src, c.envs)
			if _, ok := s.Selected(); ok {
				t.Fatal("Selected() = true, want false")
			}
			if got := s.SelectionReason(); got != c.want {
				t.Errorf("SelectionReason() = %q, want %q", got, c.want)
			}
		})
	}
}
