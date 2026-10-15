package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	acctName  = "[format('ai-{0}', uniqueString(resourceGroup().id))]"
	acctScope = "[resourceId('Microsoft.CognitiveServices/accounts', format('ai-{0}', uniqueString(resourceGroup().id)))]"
	lawRef    = "[resourceId('Microsoft.OperationalInsights/workspaces', parameters('logAnalyticsName'))]"
)

func acct() sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: acctName, Kind: "AIServices"}
}

func diag(scope string) sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.Insights/diagnosticSettings", Name: "to-law", Scope: scope, Properties: map[string]any{
		"workspaceId": lawRef,
		"logs":        []any{map[string]any{"categoryGroup": "audit", "enabled": true}},
	}}
}

func kind(res sdk.Result) string {
	switch {
	case len(res.Findings) > 0:
		return "finding"
	case res.Skipped != nil:
		return "skipped"
	}
	return "pass"
}

func TestOPS001CompiledBicepScopes(t *testing.T) {
	falseCond := func(r sdk.ARMResource) sdk.ARMResource {
		r.Conditional, r.Condition = true, "[false()]"
		return r
	}
	paramCond := func(r sdk.ARMResource) sdk.ARMResource {
		r.Conditional, r.Condition = true, "[parameters('enableDiagnostics')]"
		return r
	}
	proj := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/projects", Name: "[format('{0}/{1}', format('ai-{0}', uniqueString(resourceGroup().id)), 'proj')]"}
	projScope := "[resourceId('Microsoft.CognitiveServices/accounts/projects', format('ai-{0}', uniqueString(resourceGroup().id)), 'proj')]"
	cases := []struct {
		name string
		arm  model
		want string
	}{
		{"expression scope equals expression name", model{acct(), diag(acctScope)}, "pass"},
		{"whitespace and case differences are normalised", model{acct(), diag("[resourceId('Microsoft.CognitiveServices/accounts',format( 'ai-{0}',uniqueString( resourceGroup().id ) ) )]")}, "pass"},
		{"no diagnostic setting at all", model{acct()}, "finding"},
		{"scope of a different resource type", model{acct(), diag("[resourceId('Microsoft.Search/searchServices', 'x')]")}, "finding"},
		{"different expression cannot be correlated", model{acct(), diag("[resourceId('Microsoft.CognitiveServices/accounts', variables('accountName'))]")}, "skipped"},
		{"scope is a parameter", model{acct(), diag("[parameters('scopeId')]")}, "skipped"},
		{"literal false diagnostic setting is ignored", model{acct(), falseCond(diag(acctScope))}, "finding"},
		{"parameter-driven diagnostic setting is uncertain", model{acct(), paramCond(diag(acctScope))}, "skipped"},
		{"literal false account is not deployed", model{falseCond(acct())}, "pass"},
		{"parameter-driven account is uncertain", model{paramCond(acct())}, "skipped"},
		{"project with two name arguments", model{proj, diag(projScope)}, "pass"},
		{"workspaceId missing", model{acct(), func() sdk.ARMResource {
			d := diag(acctScope)
			d.Properties = map[string]any{"logs": []any{map[string]any{"categoryGroup": "audit", "enabled": true}}}
			return d
		}()}, "finding"},
		{"log category is an expression", model{acct(), func() sdk.ARMResource {
			d := diag(acctScope)
			d.Properties["logs"] = []any{map[string]any{"category": "[parameters('cat')]", "enabled": true}}
			return d
		}()}, "skipped"},
		{"unsupported category", model{acct(), func() sdk.ARMResource {
			d := diag(acctScope)
			d.Properties["logs"] = []any{map[string]any{"category": "Bogus", "enabled": true}}
			return d
		}()}, "finding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-OPS-001", Providers{}, &sdk.Input{ARM: tc.arm})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	if got := eval(t, "FND-OPS-001", Providers{}, &sdk.Input{}); kind(got) != "skipped" {
		t.Fatalf("missing ARM must skip, got %+v", got)
	}
}

func deployment(format, name, version any) sdk.ARMResource {
	return sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "[format('{0}/{1}', 'acct', parameters('deploymentName'))]",
		Properties: map[string]any{"model": map[string]any{"format": format, "name": name, "version": version}}}
}

func TestOPS010ExpressionsAreSkipped(t *testing.T) {
	policy := pol{"models.allow": []string{"OpenAI/gpt-4o"}}
	cases := []struct {
		name string
		r    sdk.ARMResource
		want string
	}{
		{"literal allowed", deployment("OpenAI", "gpt-4o", "2024-11-20"), "pass"},
		{"literal not allowed", deployment("OpenAI", "gpt-5", "1"), "finding"},
		{"name from parameters()", deployment("OpenAI", "[parameters('modelName')]", "2024-11-20"), "skipped"},
		{"version from parameters()", deployment("OpenAI", "gpt-4o", "[parameters('modelVersion')]"), "skipped"},
		{"format from variables()", deployment("[variables('publisher')]", "gpt-4o", "1"), "skipped"},
		{"missing version is not the text nil", deployment("OpenAI", "gpt-4o", nil), "pass"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{Policy: policy, ARM: model{tc.r}})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	cond := deployment("OpenAI", "gpt-5", "1")
	cond.Conditional, cond.Condition = true, "false"
	if got := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{Policy: policy, ARM: model{cond}}); kind(got) != "pass" {
		t.Fatalf("literal false deployment must be ignored: %+v", got)
	}
	if got := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{Policy: policy}); kind(got) != "pass" {
		t.Fatalf("no ARM, no lists overlap: %+v", got)
	}
	if got := eval(t, "FND-OPS-010", Providers{}, &sdk.Input{ARM: model{}}); kind(got) != "skipped" {
		t.Fatalf("missing policy must skip: %+v", got)
	}
}

func TestOPS006PurgeBoolean(t *testing.T) {
	ws := func(purge any) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.OperationalInsights/workspaces", Name: "law", Properties: map[string]any{
			"retentionInDays": float64(90),
			"features":        map[string]any{"immediatePurgeDataOn30Days": purge},
		}}
	}
	cases := []struct {
		name  string
		purge any
		want  string
	}{
		{"true with 90 day minimum", true, "finding"},
		{"false", false, "pass"},
		{"expression", "[parameters('purge')]", "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-OPS-006", Providers{}, &sdk.Input{Policy: pol{"logRetention.minimumDays": 90}, ARM: model{ws(tc.purge)}})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	off := ws(false)
	off.Conditional, off.Condition = true, "[parameters('create')]"
	if got := eval(t, "FND-OPS-006", Providers{}, &sdk.Input{Policy: pol{"logRetention.minimumDays": 90}, ARM: model{off}}); kind(got) != "skipped" {
		t.Fatalf("conditional workspace must be uncertain: %+v", got)
	}
}

func TestOPS009DeveloperTier(t *testing.T) {
	dep := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts/deployments", Name: "a/d", SKUName: "DeveloperTier"}
	cases := []struct {
		scope string
		want  string
	}{
		{"datazone-us", "finding"},
		{"regional", "finding"},
		{"global", "pass"},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			got := eval(t, "FND-OPS-009", Providers{}, &sdk.Input{Policy: pol{"dataResidency.scope": tc.scope}, ARM: model{dep}})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	if got := eval(t, "FND-OPS-009", Providers{}, &sdk.Input{Policy: pol{"dataResidency.scope": "global"}}); kind(got) != "skipped" {
		t.Fatalf("missing ARM must skip, not panic: %+v", got)
	}
	loc := sdk.ARMResource{Type: "Microsoft.Search/searchServices", Name: "s", Region: "[resourceGroup().location]"}
	if got := eval(t, "FND-OPS-009", Providers{}, &sdk.Input{Policy: pol{"dataResidency.scope": "global", "dataResidency.regions": []string{"westeurope"}}, ARM: model{loc}}); kind(got) != "skipped" {
		t.Fatalf("resourceGroup().location must be unresolved: %+v", got)
	}
}

func TestOPS005AndOPS004Conditional(t *testing.T) {
	st := sdk.ARMResource{Type: "Microsoft.Storage/storageAccounts", Name: "bad-name", Conditional: true, Condition: "[false()]"}
	if got := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{st}}); kind(got) != "pass" {
		t.Fatalf("literal false must be ignored: %+v", got)
	}
	st.Condition = "[parameters('deployStorage')]"
	if got := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{st}}); kind(got) != "skipped" {
		t.Fatalf("parameter condition must be uncertain: %+v", got)
	}
	st.Conditional, st.Condition = false, ""
	st.Name = "[format('st{0}', uniqueString(resourceGroup().id))]"
	if got := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{st}}); kind(got) != "skipped" {
		t.Fatalf("expression name must skip: %+v", got)
	}
	// An unrelated expression-named resource must not turn a pass into a skip.
	other := sdk.ARMResource{Type: "Microsoft.Network/virtualNetworks", Name: "[parameters('vnet')]"}
	if got := eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{other}}); kind(got) != "pass" {
		t.Fatalf("unchecked type must not skip: %+v", got)
	}
}

func TestOPS003ConditionalAlerts(t *testing.T) {
	alert := sdk.ARMResource{Type: "Microsoft.Insights/activityLogAlerts", Name: "sh", Conditional: true, Condition: "[parameters('alerts')]",
		Properties: map[string]any{"condition": "ServiceHealth"}}
	got := eval(t, "FND-OPS-003", Providers{}, &sdk.Input{ARM: model{acct(), alert}})
	for _, f := range got.Findings {
		if strings.Contains(f.Evidence, "Service Health") {
			t.Fatalf("uncertain alert must not yield a Service Health finding: %+v", got)
		}
	}
	alert.Condition = "false"
	got = eval(t, "FND-OPS-003", Providers{}, &sdk.Input{ARM: model{acct(), alert}})
	found := false
	for _, f := range got.Findings {
		found = found || strings.Contains(f.Evidence, "Service Health")
	}
	if !found {
		t.Fatalf("literal false alert is not deployed: %+v", got)
	}
}

// writeFile creates dir/name below root with body.
func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func prodInput(t *testing.T, azureYAML string) *sdk.Input {
	t.Helper()
	return &sdk.Input{Profile: "foundry-prod", AzureYAML: doc(t, azureYAML)}
}

const plainYAML = "name: d\nservices:\n  p:\n    host: azure.ai.project\n"

func TestOPS007Pipelines(t *testing.T) {
	gitRepo := func(t *testing.T) (repo, project string) {
		repo = t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		project = filepath.Join(repo, "apps", "agent")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		return repo, project
	}
	cases := []struct {
		name  string
		setup func(t *testing.T) (root string)
		yaml  string
		want  string
	}{
		{"workflow at git root, project in subdirectory", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/azure-dev.yml", "jobs:\n  d:\n    steps:\n    - run: azd provision --no-prompt\n")
			return project
		}, plainYAML, "pass"},
		{"azd up in a multi-line run step", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/ci.yaml", "jobs:\n  d:\n    steps:\n    - run: |\n        azd auth login\n        azd up\n")
			return project
		}, plainYAML, "pass"},
		{"azure.yaml pipeline section alone is not evidence", func(t *testing.T) string {
			_, project := gitRepo(t)
			return project
		}, plainYAML + "pipeline:\n  provider: github\n", "finding"},
		{"azd only in a YAML comment", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/azure-dev.yml", "# azd up\njobs:\n  d:\n    steps:\n    - run: echo hi\n")
			return project
		}, plainYAML, "skipped"},
		{"azd only in a shell comment line", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/azure-dev.yml", "jobs:\n  d:\n    steps:\n    - run: |\n        # azd up\n        echo hi\n")
			return project
		}, plainYAML, "skipped"},
		{"azd text outside a run step", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/azure-dev.yml", "name: azd up\njobs:\n  d:\n    steps:\n    - uses: actions/checkout@v4\n      with:\n        note: azd up\n")
			return project
		}, plainYAML, "skipped"},
		{"non-YAML file is ignored", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/azure-dev.txt", "run: azd up\n")
			return project
		}, plainYAML, "finding"},
		{"azd auth login only", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/x.yml", "jobs:\n  d:\n    steps:\n    - run: azd auth login\n")
			return project
		}, plainYAML, "skipped"},
		{"azure pipelines script step", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".azdo/pipelines/azure-dev.yml", "steps:\n- script: azd deploy\n")
			return project
		}, plainYAML, "pass"},
		{"no git directory and no files is uncertain", func(t *testing.T) string { return t.TempDir() }, plainYAML, "skipped"},
		{"no git directory but pipeline in project passes", func(t *testing.T) string {
			d := t.TempDir()
			writeFile(t, d, ".github/workflows/azure-dev.yml", "steps:\n- run: azd up\n")
			return d
		}, plainYAML, "pass"},
		{"invalid YAML is skipped, not a finding", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/bad.yml", "a: [unclosed\n")
			return project
		}, plainYAML, "skipped"},
		{"oversize workflow is skipped, not a finding", func(t *testing.T) string {
			repo, project := gitRepo(t)
			writeFile(t, repo, ".github/workflows/big.yml", "# "+strings.Repeat("x", maxRepoFileBytes+10)+"\nsteps:\n- run: azd up\n")
			return project
		}, plainYAML, "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-OPS-007", Providers{Root: tc.setup(t)}, prodInput(t, tc.yaml))
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
			if tc.want == "finding" && got.Findings[0].Location.File != "azure.yaml" {
				t.Fatalf("finding must carry the azure.yaml location: %+v", got.Findings[0])
			}
		})
	}
	if got := eval(t, "FND-OPS-007", Providers{Root: t.TempDir()}, &sdk.Input{Profile: "foundry-dev"}); got.Skipped == nil {
		t.Fatalf("dev profile must skip: %+v", got)
	}
	if got := eval(t, "FND-OPS-007", Providers{}, prodInput(t, plainYAML)); got.Skipped == nil {
		t.Fatalf("missing root must skip: %+v", got)
	}
}

func TestRepoFilesSymlinkAndLimits(t *testing.T) {
	t.Run("symlinked workflow file pointing outside the repository", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		target := filepath.Join(outside, "azure-dev.yml")
		if err := os.WriteFile(target, []byte("steps:\n- run: azd up\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repo, ".github", "workflows"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(repo, ".github", "workflows", "azure-dev.yml")); err != nil {
			t.Skip("symlinks unavailable: ", err)
		}
		got := eval(t, "FND-OPS-007", Providers{Root: repo}, prodInput(t, plainYAML))
		if kind(got) != "skipped" {
			t.Fatalf("symlink must be skipped, not read or passed: %+v", got)
		}
	})
	t.Run("symlink to an unbounded device is never read", func(t *testing.T) {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repo, ".github", "workflows"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/zero", filepath.Join(repo, ".github", "workflows", "zero.yml")); err != nil {
			t.Skip("symlinks unavailable: ", err)
		}
		scan := scanPipelines(repo)
		if len(scan.Files) != 0 || scan.Skipped != 1 {
			t.Fatalf("scan=%+v", scan)
		}
	})
	t.Run("symlinked workflows directory", func(t *testing.T) {
		repo := t.TempDir()
		outside := t.TempDir()
		writeFile(t, outside, "azure-dev.yml", "steps:\n- run: azd up\n")
		if err := os.MkdirAll(filepath.Join(repo, ".github"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(repo, ".github", "workflows")); err != nil {
			t.Skip("symlinks unavailable: ", err)
		}
		scan := scanPipelines(repo)
		if len(scan.Files) != 0 || scan.Skipped != 1 {
			t.Fatalf("scan=%+v", scan)
		}
	})
	t.Run("oversize file", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".github/workflows/big.yml", strings.Repeat("a", maxRepoFileBytes+1))
		scan := scanPipelines(repo)
		if len(scan.Files) != 0 || scan.Skipped != 1 {
			t.Fatalf("scan=%+v", scan)
		}
	})
	t.Run("file exactly at the cap is read", func(t *testing.T) {
		repo := t.TempDir()
		body := "steps:\n- run: azd up\n# " + strings.Repeat("a", maxRepoFileBytes-len("steps:\n- run: azd up\n# ")-1) + "\n"
		writeFile(t, repo, ".github/workflows/ok.yml", body)
		scan := scanPipelines(repo)
		if len(scan.Files) != 1 || scan.Skipped != 0 {
			t.Fatalf("scan=%+v", scan)
		}
	})
	t.Run("file count cap", func(t *testing.T) {
		repo := t.TempDir()
		for i := 0; i < maxRepoFiles+5; i++ {
			writeFile(t, repo, fmt.Sprintf(".github/workflows/w%03d.yml", i), "a: 1\n")
		}
		scan := scanPipelines(repo)
		if len(scan.Files) != maxRepoFiles || scan.Skipped != 5 {
			t.Fatalf("files=%d skipped=%d", len(scan.Files), scan.Skipped)
		}
	})
	t.Run("eval.yaml symlink is not read", func(t *testing.T) {
		d := t.TempDir()
		if err := os.Symlink("/dev/zero", filepath.Join(d, "eval.yaml")); err != nil {
			t.Skip("symlinks unavailable: ", err)
		}
		if _, ok, skipped := readRootFile(d, "eval.yaml"); ok || !skipped {
			t.Fatalf("ok=%v skipped=%v", ok, skipped)
		}
	})
}

func TestGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := gitRoot(sub)
	if !ok || got != repo {
		t.Fatalf("got %q ok=%v want %q", got, ok, repo)
	}
}

func TestOPS008EvalPipeline(t *testing.T) {
	agentYAML := "name: d\nservices:\n  a:\n    host: azure.ai.agent\n"
	setup := func(t *testing.T, evalYAML, workflow string) string {
		repo := t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if evalYAML != "" {
			writeFile(t, repo, "eval.yaml", evalYAML)
		}
		if workflow != "" {
			writeFile(t, repo, ".github/workflows/azure-dev.yml", workflow)
		}
		return repo
	}
	goodEval := "agent: a\ndataset_file: d.jsonl\nevaluators:\n- name: x\n"
	cases := []struct {
		name     string
		evalYAML string
		workflow string
		want     string
	}{
		{"complete", goodEval, "jobs:\n  e:\n    steps:\n    - run: azd ai agent eval run\n  production:\n    steps:\n    - run: azd deploy\n", "pass"},
		{"eval runs only after production", goodEval, "jobs:\n  production:\n    steps:\n    - run: azd deploy\n  e:\n    steps:\n    - run: azd ai agent eval run\n", "finding"},
		{"eval command only in a comment", goodEval, "jobs:\n  e:\n    steps:\n    - run: |\n        # azd ai agent eval run\n        echo\n", "finding"},
		{"missing eval.yaml", "", "jobs:\n  e:\n    steps:\n    - run: azd ai agent eval run\n", "finding"},
		{"evaluators only in a comment", "# evaluators\ndataset: d\n", "jobs:\n  e:\n    steps:\n    - run: azd ai agent eval run\n", "finding"},
		{"invalid eval.yaml is uncertain", "a: [x\n", "jobs:\n  e:\n    steps:\n    - run: azd ai agent eval run\n", "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval(t, "FND-OPS-008", Providers{Root: setup(t, tc.evalYAML, tc.workflow)}, prodInput(t, agentYAML))
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	if got := eval(t, "FND-OPS-008", Providers{Root: t.TempDir()}, prodInput(t, plainYAML)); got.Skipped == nil {
		t.Fatalf("no agent services must skip: %+v", got)
	}
}

func TestExprHelpers(t *testing.T) {
	for s, want := range map[string]bool{"[parameters('a')]": true, "[[literal": false, "plain": false, "": false, " [format('x')]": true} {
		if isExpr(s) != want {
			t.Errorf("isExpr(%q) != %v", s, want)
		}
	}
	fn, args, ok := splitCall("resourceId('A/b', format('{0},{1}', x(1, 2), 'y'))")
	if !ok || fn != "resourceId" || len(args) != 2 || args[1] != "format('{0},{1}', x(1, 2), 'y')" {
		t.Fatalf("fn=%q args=%q ok=%v", fn, args, ok)
	}
	if _, _, ok := splitCall("not a call"); ok {
		t.Fatal("expected not ok")
	}
	if _, _, ok := splitCall("f('unterminated)"); ok {
		t.Fatal("expected not ok")
	}
}
