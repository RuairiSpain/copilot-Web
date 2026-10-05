package project

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

const fixtures = "../../test/fixtures/project"

func hasWarning(d *Discovery, code string) bool {
	return slices.ContainsFunc(d.Warnings, func(w Warning) bool { return w.Code == code })
}

func TestDiscoverFixtures(t *testing.T) {
	tests := []struct {
		name      string
		dir       string
		start     string // relative to the fixture
		wantName  string
		wantIaC   model.IaCMode
		wantInfra string
		bicep     []string
		bicepPar  []string
		arm       []string
		pipelines []string
		envs      []string
		current   string
		gitignore GitignoreInfo
	}{
		{
			name: "bicep", dir: "bicep-basic", wantName: "bicep-basic", wantIaC: model.IaCBicep, wantInfra: "infra",
			bicep: []string{"infra/main.bicep"}, bicepPar: []string{"infra/main.bicepparam"},
			pipelines: []string{".github/workflows/azure-dev.yml"},
			envs:      []string{"dev", "prod"}, current: "dev",
			gitignore: GitignoreInfo{Present: true, IgnoresAzure: true, Negations: true},
		},
		{
			name: "bicep from subdirectory", dir: "bicep-basic", start: "infra", wantName: "bicep-basic", wantIaC: model.IaCBicep, wantInfra: "infra",
			bicep: []string{"infra/main.bicep"}, bicepPar: []string{"infra/main.bicepparam"},
			pipelines: []string{".github/workflows/azure-dev.yml"},
			envs:      []string{"dev", "prod"}, current: "dev",
			gitignore: GitignoreInfo{Present: true, IgnoresAzure: true, Negations: true},
		},
		{
			name: "bicep from a file", dir: "bicep-basic", start: "infra/main.bicep", wantName: "bicep-basic", wantIaC: model.IaCBicep, wantInfra: "infra",
			bicep: []string{"infra/main.bicep"}, bicepPar: []string{"infra/main.bicepparam"},
			pipelines: []string{".github/workflows/azure-dev.yml"},
			envs:      []string{"dev", "prod"}, current: "dev",
			gitignore: GitignoreInfo{Present: true, IgnoresAzure: true, Negations: true},
		},
		{
			name: "synthetic foundry", dir: "synthetic-foundry", wantName: "synthetic-foundry", wantIaC: model.IaCSynthetic, wantInfra: "infra",
		},
		{
			name: "arm json", dir: "arm-json", wantName: "arm-json", wantIaC: model.IaCARMJSON, wantInfra: "infra",
			arm: []string{"infra/main.json"},
		},
		{
			name: "none", dir: "none", wantName: "none", wantIaC: model.IaCNone, wantInfra: "infra",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start := filepath.Join(fixtures, tc.dir, filepath.FromSlash(tc.start))
			d, err := Discover(context.Background(), start)
			if err != nil {
				t.Fatal(err)
			}
			p := d.Project
			if filepath.Base(p.Root) != tc.dir || !filepath.IsAbs(p.Root) {
				t.Errorf("root %q", p.Root)
			}
			if p.Name != tc.wantName || p.IaC != tc.wantIaC || p.InfraPath != tc.wantInfra || p.InfraModule != "main" || p.AzureYAMLPath != "azure.yaml" {
				t.Errorf("project %+v", p)
			}
			eq := func(what string, got, want []string) {
				if !slices.Equal(got, want) {
					t.Errorf("%s = %v, want %v", what, got, want)
				}
			}
			eq("bicep", d.Infra.Bicep, tc.bicep)
			eq("bicepparam", d.Infra.BicepParam, tc.bicepPar)
			eq("arm", d.Infra.ARMJSON, tc.arm)
			eq("pipelines", d.Repo.PipelineFiles, tc.pipelines)
			eq("envs", p.Environments, tc.envs)
			if p.CurrentEnvironment != tc.current {
				t.Errorf("current %q", p.CurrentEnvironment)
			}
			if d.Repo.Gitignore != tc.gitignore {
				t.Errorf("gitignore %+v", d.Repo.Gitignore)
			}
			if !slices.ContainsFunc(d.Unavailable, func(c Capability) bool { return c.Name == CapabilityTrackedFiles }) {
				t.Error("tracked-files capability gap not reported")
			}
			if p.HasARM() != (tc.wantIaC == model.IaCBicep || tc.wantIaC == model.IaCARMJSON) {
				t.Error("HasARM mismatch")
			}
		})
	}
}

// tree builds a temp project and returns its root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, data := range files {
		write(t, root, rel, data)
	}
	return root
}

func TestDiscoverIaCRules(t *testing.T) {
	foundryYAML := "name: f\nservices:\n  p:\n    host: azure.ai.project\n"
	tests := []struct {
		name  string
		files map[string]string
		want  model.IaCMode
		warn  string
		infra string
	}{
		{"custom infra path", map[string]string{
			"azure.yaml": "name: a\ninfra:\n  path: deploy/iac\n", "deploy/iac/main.bicep": "x",
		}, model.IaCBicep, "", "deploy/iac"},
		{"windows style infra path", map[string]string{
			"azure.yaml": "name: a\ninfra:\n  path: 'deploy\\iac'\n", "deploy/iac/main.bicep": "x",
		}, model.IaCBicep, "", "deploy/iac"},
		{"custom module", map[string]string{
			"azure.yaml": "name: a\ninfra:\n  module: core\n", "infra/core.bicep": "x",
		}, model.IaCBicep, "", "infra"},
		{"bicepparam only", map[string]string{
			"azure.yaml": "name: a\n", "infra/main.bicepparam": "using 'x'",
		}, model.IaCBicep, "", "infra"},
		{"other bicep without module and foundry host", map[string]string{
			"azure.yaml": foundryYAML, "infra/other.bicep": "x",
		}, model.IaCSynthetic, WarnInfraModuleAbsent, "infra"},
		{"on disk bicep beats foundry host", map[string]string{
			"azure.yaml": foundryYAML, "infra/main.bicep": "x",
		}, model.IaCBicep, "", "infra"},
		{"uppercase extension listed but module must match", map[string]string{
			"azure.yaml": "name: a\n", "infra/MAIN.BICEP": "x",
		}, model.IaCNone, WarnInfraModuleAbsent, "infra"},
		{"no infra, foundry host, no provider", map[string]string{"azure.yaml": foundryYAML}, model.IaCSynthetic, "", "infra"},
		{"legacy agent host", map[string]string{"azure.yaml": "name: a\nservices:\n  s:\n    host: azure.ai.agent\n"}, model.IaCSynthetic, "", "infra"},
		{"microsoft.foundry host", map[string]string{"azure.yaml": "name: a\nservices:\n  s:\n    host: microsoft.foundry\n"}, model.IaCSynthetic, "", "infra"},
		{"foundry provider without services", map[string]string{"azure.yaml": "name: a\ninfra:\n  provider: microsoft.foundry\n"}, model.IaCNone, "", "infra"},
		{"terraform provider", map[string]string{"azure.yaml": "name: a\ninfra:\n  provider: terraform\nservices:\n  s:\n    host: azure.ai.project\n"}, model.IaCNone, WarnProviderOther, "infra"},
		{"non foundry host", map[string]string{"azure.yaml": "name: a\nservices:\n  s:\n    host: containerapp\n"}, model.IaCNone, "", "infra"},
		{"empty azure.yaml", map[string]string{"azure.yaml": ""}, model.IaCNone, "", "infra"},
		{"azure.yml", map[string]string{"azure.yml": foundryYAML}, model.IaCSynthetic, "", "infra"},
		{"unparseable yaml falls back", map[string]string{"azure.yaml": "name: [unclosed\n", "infra/main.bicep": "x"}, model.IaCBicep, WarnAzureYAMLParse, "infra"},
		{"duplicate keys fall back", map[string]string{"azure.yaml": "name: a\nname: b\n"}, model.IaCNone, WarnAzureYAMLParse, "infra"},
		{"unsafe infra path", map[string]string{"azure.yaml": "name: a\ninfra:\n  path: ../outside\n"}, model.IaCNone, WarnInfraPathInvalid, ""},
		{"absolute infra path", map[string]string{"azure.yaml": "name: a\ninfra:\n  path: /etc\n"}, model.IaCNone, WarnInfraPathInvalid, ""},
		{"infra path is dot", map[string]string{"azure.yaml": "name: a\ninfra:\n  path: .\n"}, model.IaCNone, WarnInfraPathInvalid, ""},
		{"infra is a file", map[string]string{"azure.yaml": "name: a\n", "infra": "file"}, model.IaCNone, WarnInfraNotDir, "infra"},
		{"layers noted", map[string]string{"azure.yaml": "name: a\ninfra:\n  layers:\n    - name: x\n"}, model.IaCNone, WarnInfraLayers, "infra"},
		{"arm json beats foundry host", map[string]string{
			"azure.yaml": foundryYAML, "infra/t.json": `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"}`,
		}, model.IaCARMJSON, "", "infra"},
		{"json that is not arm", map[string]string{"azure.yaml": "name: a\n", "infra/p.json": `{"a":1}`, "infra/bad.json": "{nope"}, model.IaCNone, "", "infra"},
		{"both azure.yaml and yml", map[string]string{"azure.yaml": "name: y\n", "azure.yml": "name: m\n"}, model.IaCNone, WarnBothAzureYAML, "infra"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := tree(t, tc.files)
			d, err := Discover(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if d.Project.IaC != tc.want {
				t.Errorf("IaC = %q, want %q (warnings %v)", d.Project.IaC, tc.want, d.Warnings)
			}
			if d.Project.InfraPath != tc.infra {
				t.Errorf("InfraPath = %q, want %q", d.Project.InfraPath, tc.infra)
			}
			if tc.warn != "" && !hasWarning(d, tc.warn) {
				t.Errorf("want warning %s, got %v", tc.warn, d.Warnings)
			}
			if tc.warn == "" && len(d.Warnings) != 0 {
				t.Errorf("unexpected warnings %v", d.Warnings)
			}
		})
	}
}

func TestDiscoverNameFallsBackToDirectory(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "services: {}\n"})
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if d.Project.Name != filepath.Base(root) {
		t.Fatalf("name %q", d.Project.Name)
	}
}

func TestDiscoverNoProject(t *testing.T) {
	_, err := Discover(context.Background(), t.TempDir())
	// A stray azure.yaml in a parent of the temp dir would make this succeed; tolerate only that.
	if err == nil {
		t.Skip("an azure.yaml exists above the temp directory")
	}
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("want ErrNoProject, got %v", err)
	}
	_, err = Discover(context.Background(), filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing start: %v", err)
	}
}

func TestDiscoverNearestWinsAndShadowed(t *testing.T) {
	root := tree(t, map[string]string{
		"azure.yaml":             "name: outer\n",
		"mid/azure.yml":          "name: mid\n",
		"mid/inner/azure.yaml":   "name: inner\n",
		"mid/inner/src/app/keep": "",
	})
	d, err := Discover(context.Background(), filepath.Join(root, "mid/inner/src/app"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Project.Name != "inner" || d.Project.Root != filepath.Join(root, "mid", "inner") {
		t.Fatalf("project %+v", d.Project)
	}
	want := []string{"../../../azure.yml", "../../../../azure.yaml"}
	if !slices.Equal(d.ShadowedAzureYAML, want) {
		t.Fatalf("shadowed %v, want %v", d.ShadowedAzureYAML, want)
	}
	if !hasWarning(d, WarnShadowedAzureYAML) {
		t.Error("no shadow warning")
	}
}

func TestDiscoverAncestorLimit(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "name: a\n", "a/b/c/d/keep": ""})
	_, err := Discover(context.Background(), filepath.Join(root, "a/b/c/d"), WithLimits(Limits{MaxAncestors: 2}))
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("want ErrNoProject when the limit stops the walk, got %v", err)
	}
}

func TestDiscoverCaseVariant(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "name: a\n", "Azure.YAML": "name: b\n"})
	entries, _ := os.ReadDir(root)
	if len(entries) < 2 {
		t.Skip("case-insensitive file system")
	}
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(d, WarnCaseVariant) {
		t.Fatalf("warnings %v", d.Warnings)
	}
	if d.Project.Name != "a" {
		t.Fatalf("exact-case file must win, got %q", d.Project.Name)
	}
}

func TestDiscoverAzureDirProblems(t *testing.T) {
	t.Run("azure is a file", func(t *testing.T) {
		root := tree(t, map[string]string{"azure.yaml": "name: a\n", ".azure": "file"})
		d, err := Discover(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Project.Environments) != 0 || !hasWarning(d, WarnAzureDirNotDir) {
			t.Fatalf("%+v %v", d.Project.Environments, d.Warnings)
		}
	})
	t.Run("azure escapes", func(t *testing.T) {
		outside := t.TempDir()
		write(t, outside, "evil/.env", "A=1")
		root := tree(t, map[string]string{"azure.yaml": "name: a\n"})
		symlink(t, outside, filepath.Join(root, ".azure"))
		d, err := Discover(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Project.Environments) != 0 || !hasWarning(d, WarnAzureDirUnread) {
			t.Fatalf("%+v %v", d.Project.Environments, d.Warnings)
		}
	})
	t.Run("default env missing config", func(t *testing.T) {
		root := tree(t, map[string]string{"azure.yaml": "name: a\n", ".azure/dev/.env": "A=1\n"})
		d, err := Discover(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		if d.Project.CurrentEnvironment != "" || len(d.Project.Environments) != 1 {
			t.Fatalf("%+v", d.Project)
		}
	})
}

func TestDiscoverSymlinkedAzureYAMLEscapeRefused(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "azure.yaml", "name: evil\n")
	root := t.TempDir()
	symlink(t, filepath.Join(outside, "azure.yaml"), filepath.Join(root, "azure.yaml"))
	_, err := Discover(context.Background(), root)
	if !errors.Is(err, ErrSymlinkEscape) {
		t.Fatalf("want ErrSymlinkEscape, got %v", err)
	}
}

func TestDiscoverInfraSymlinks(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "evil.bicep", "x")
	write(t, outside, "dir/more.bicep", "x")
	root := tree(t, map[string]string{"azure.yaml": "name: a\n", "infra/main.bicep": "x"})
	symlink(t, "main.bicep", filepath.Join(root, "infra", "alias.bicep"))
	symlink(t, filepath.Join(outside, "evil.bicep"), filepath.Join(root, "infra", "evil.bicep"))
	symlink(t, outside, filepath.Join(root, "infra", "outdir"))
	symlink(t, "nowhere.bicep", filepath.Join(root, "infra", "dangling.bicep"))
	symlink(t, ".", filepath.Join(root, "infra", "loop"))
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"infra/alias.bicep", "infra/main.bicep"}
	if !slices.Equal(d.Infra.Bicep, want) {
		t.Fatalf("bicep %v, want %v", d.Infra.Bicep, want)
	}
	if !hasWarning(d, WarnBrokenLink) {
		t.Errorf("no broken link warning: %v", d.Warnings)
	}
}

func TestDiscoverInfraSymlinkedDirEscape(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "main.bicep", "x")
	root := tree(t, map[string]string{"azure.yaml": "name: a\n"})
	symlink(t, outside, filepath.Join(root, "infra"))
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Infra.Bicep) != 0 || d.Project.IaC != model.IaCNone {
		t.Fatalf("escaped infra must not be listed: %+v", d.Infra)
	}
}

func TestDiscoverLimits(t *testing.T) {
	files := map[string]string{"azure.yaml": "name: a\n"}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		files["infra/"+n+".bicep"] = "x"
		files["infra/"+n+".json"] = "{}"
	}
	root := tree(t, files)
	d, err := Discover(context.Background(), root, WithLimits(Limits{MaxListedFiles: 3}))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Infra.Truncated || !hasWarning(d, WarnListTruncated) {
		t.Fatalf("expected truncation: %+v %v", d.Infra, d.Warnings)
	}
	d, err = Discover(context.Background(), root, WithLimits(Limits{MaxARMSniffs: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Infra.Truncated {
		t.Fatal("expected truncation from the sniff limit")
	}
	big := tree(t, map[string]string{"azure.yaml": strings.Repeat("# c\n", 100) + "name: a\n"})
	if _, err := Discover(context.Background(), big, WithLimits(Limits{MaxFileBytes: 16})); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge for azure.yaml, got %v", err)
	}
}

func TestDiscoverCancelled(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "name: a\n"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Discover(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestDiscoverRepoInfo(t *testing.T) {
	root := tree(t, map[string]string{
		"azure.yaml":                           "name: a\n",
		".github/workflows/azure-dev.yaml":     "x",
		".azdo/pipelines/azure-dev.yml":        "x",
		".azuredevops/pipelines/azure-dev.yml": "x",
		".github/workflows/other.yml":          "x",
		".git/HEAD":                            "ref: refs/heads/main\n",
		".gitignore":                           "# c\n\nnode_modules\n/.azure/\n",
	})
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".azdo/pipelines/azure-dev.yml", ".azuredevops/pipelines/azure-dev.yml", ".github/workflows/azure-dev.yaml"}
	if !slices.Equal(d.Repo.PipelineFiles, want) {
		t.Errorf("pipelines %v", d.Repo.PipelineFiles)
	}
	if !d.Repo.HasGit || d.Repo.Gitignore != (GitignoreInfo{Present: true, IgnoresAzure: true}) {
		t.Errorf("repo %+v", d.Repo)
	}
}

func TestDiscoverRepoInfoAbsent(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "name: a\n", ".gitignore": "*.log\r\n.azurite\r\n"})
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if d.Repo.HasGit || len(d.Repo.PipelineFiles) != 0 || d.Repo.Gitignore != (GitignoreInfo{Present: true}) {
		t.Errorf("repo %+v", d.Repo)
	}
	root = tree(t, map[string]string{"azure.yaml": "name: a\n"})
	d, _ = Discover(context.Background(), root)
	if d.Repo.Gitignore.Present {
		t.Error("gitignore reported present")
	}
}

func TestDiscoverEnvironmentCaseCollision(t *testing.T) {
	root := tree(t, map[string]string{"azure.yaml": "name: a\n", ".azure/dev/.env": "A=1\n", ".azure/Dev/.env": "A=1\n"})
	if entries, _ := os.ReadDir(filepath.Join(root, ".azure")); len(entries) < 2 {
		t.Skip("case-insensitive file system")
	}
	d, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(d, WarnEnvCaseCollision) {
		t.Fatalf("warnings %v", d.Warnings)
	}
}

func TestDiscoverDeterministic(t *testing.T) {
	a, err := Discover(context.Background(), filepath.Join(fixtures, "bicep-basic"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Discover(context.Background(), filepath.Join(fixtures, "bicep-basic"))
	if !slices.Equal(a.Warnings, b.Warnings) || !slices.Equal(a.Infra.Bicep, b.Infra.Bicep) {
		t.Fatal("non-deterministic")
	}
}

func TestParseGitignore(t *testing.T) {
	tests := []struct {
		in   string
		want GitignoreInfo
	}{
		{"", GitignoreInfo{Present: true}},
		{".azure\n", GitignoreInfo{Present: true, IgnoresAzure: true}},
		{"  \n.azure/*   \n", GitignoreInfo{Present: true, IgnoresAzure: true}},
		{"**/.azure\n!.azure/config.json\n", GitignoreInfo{Present: true, IgnoresAzure: true, Negations: true}},
		{"# .azure\n", GitignoreInfo{Present: true}},
		{"!.azure\n", GitignoreInfo{Present: true, Negations: true}},
	}
	for _, tc := range tests {
		if got := parseGitignore([]byte(tc.in)); got != tc.want {
			t.Errorf("%q: got %+v want %+v", tc.in, got, tc.want)
		}
	}
}

func TestIsARMTemplate(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#"}`, true},
		{`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/DEPLOYMENTTEMPLATE.json"}`, true},
		{`{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentParameters.json#"}`, false},
		{`{"$schema":5}`, false},
		{`{}`, false},
		{`[]`, false},
		{``, false},
		{"\x00\x01", false},
	}
	for _, tc := range tests {
		if got := isARMTemplate([]byte(tc.in)); got != tc.want {
			t.Errorf("%q: got %v", tc.in, got)
		}
	}
}
