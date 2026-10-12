package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// tree writes a repository tree below a temporary root.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func repoInput(t *testing.T, profile string, files map[string]string, azureYAML string) *model.Input {
	t.Helper()
	if azureYAML == "" {
		azureYAML = "name: demo\n"
	}
	in := yamlInput(t, azureYAML)
	in.Profile = profile
	in.Project.Root = tree(t, files)
	return in
}

const azdWorkflow = "jobs:\n  deploy:\n    steps:\n      - run: azd provision --no-prompt\n      - run: azd deploy --no-prompt\n"

func scenarios007() scenarios {
	r := ops007{}
	return scenarios{
		"positive/prod-profile-no-pipeline-files": func(t *testing.T) {
			got := eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{"README.md": "x"}, ""))
			wantKeys(t, got, sdk.StatusFail, "no-pipeline")
			if got[0].Finding.Confidence != sdk.ConfidenceLikely || got[0].Finding.Location.File != "azure.yaml" {
				t.Fatalf("finding = %+v", got[0].Finding)
			}
		},
		"positive/workflow-present-but-never-invokes-azd": func(t *testing.T) {
			got := eval(t, r, repoInput(t, rules.ProfileTest, map[string]string{".github/workflows/azure-dev.yml": "steps:\n  - run: echo hi\n"}, ""))
			wantKeys(t, got, sdk.StatusUncertain, "pipeline-without-azd")
			if got[0].Finding.Confidence != sdk.ConfidenceUncertain {
				t.Fatal("confidence")
			}
		},
		"negative/github-workflows-azure-dev-yml": func(t *testing.T) {
			wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".github/workflows/azure-dev.yml": azdWorkflow}, "")))
		},
		"negative/azdo-pipelines-azure-dev-yml": func(t *testing.T) {
			wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".azdo/pipelines/azure-dev.yml": azdWorkflow}, "")))
			wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".azuredevops/pipelines/azure-dev.yaml": azdWorkflow}, "")))
		},
		"negative/azure-yaml-pipeline-section-with-provider": func(t *testing.T) {
			wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, nil, "name: demo\npipeline:\n  provider: azdo\n")))
			wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, nil, "name: demo\npipeline:\n  variables: [A]\n")))
		},
		"skipped/dev-profile": func(t *testing.T) {
			got := eval(t, r, repoInput(t, rules.ProfileDev, nil, ""))
			wantSkip(t, got, "not-applicable")
			wantNoFindings(t, got)
		},
		"skipped/repository-root-not-available": func(t *testing.T) {
			in := repoInput(t, rules.ProfileProd, nil, "")
			in.Project.Root = ""
			wantSkip(t, eval(t, r, in), "unresolved-value:repository-root-not-available")
			in.Project.Root = filepath.Join(t.TempDir(), "missing")
			wantSkip(t, eval(t, r, in), "unresolved-value:repository-root-not-available")
		},
		"skipped/ci-system-other-than-github-or-azure-pipelines": func(t *testing.T) {
			got := eval(t, r, repoInput(t, rules.ProfileProd, nil, "name: demo\npipeline:\n  provider: jenkins\n"))
			wantSkip(t, got, "not-applicable")
			wantNoFindings(t, got)
		},
	}
}

func TestOPS007Extras(t *testing.T) {
	r := ops007{}
	t.Run("another workflow that invokes azd passes", func(t *testing.T) {
		wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".github/workflows/ci.yaml": "run: azd up\n", ".github/workflows/x.txt": ""}, "")))
	})
	t.Run("unrelated workflow does not", func(t *testing.T) {
		wantKeys(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".github/workflows/ci.yml": "run: go test\n"}, "")), sdk.StatusFail, "no-pipeline")
	})
	t.Run("azure-dev without azd invocation plus pipeline section passes", func(t *testing.T) {
		wantPass(t, eval(t, r, repoInput(t, rules.ProfileProd, map[string]string{".github/workflows/azure-dev.yml": "x: y\n"}, "name: demo\npipeline:\n  provider: github\n")))
	})
	t.Run("symlinked pipeline file is ignored", func(t *testing.T) {
		in := repoInput(t, rules.ProfileProd, map[string]string{"elsewhere.yml": azdWorkflow}, "")
		if err := os.MkdirAll(filepath.Join(in.Project.Root, ".github", "workflows"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(in.Project.Root, "elsewhere.yml"), filepath.Join(in.Project.Root, ".github", "workflows", "azure-dev.yml")); err != nil {
			t.Skip("symlinks unavailable")
		}
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "no-pipeline")
	})
	t.Run("azure.yaml absent", func(t *testing.T) {
		in := repoInput(t, rules.ProfileProd, nil, "")
		in.AzureYAML, in.YAMLReport = nil, nil
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "no-pipeline")
	})
	t.Run("unknown profile", func(t *testing.T) {
		wantSkip(t, eval(t, r, repoInput(t, "weird", nil, "")), "not-applicable")
	})
	t.Run("evidence has no absolute path", func(t *testing.T) {
		in := repoInput(t, rules.ProfileProd, nil, "")
		f := fails(eval(t, r, in))[0]
		if strings.Contains(f.Evidence, in.Project.Root) {
			t.Fatal("absolute path in evidence")
		}
	})
}
