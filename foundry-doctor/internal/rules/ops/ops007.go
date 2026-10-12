package ops

import (
	"context"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// pipelineFiles are the azd pipeline definition locations (phase 1 fact check, item 5).
var pipelineFiles = []string{
	".github/workflows/azure-dev.yml", ".github/workflows/azure-dev.yaml",
	".azdo/pipelines/azure-dev.yml", ".azdo/pipelines/azure-dev.yaml",
	".azuredevops/pipelines/azure-dev.yml", ".azuredevops/pipelines/azure-dev.yaml",
}

const (
	workflowDir      = ".github/workflows"
	maxPipelineBytes = 1 << 20
	maxWorkflowFiles = 200
)

var (
	azdDeployRe = regexp.MustCompile(`\bazd\s+(provision|deploy|up)\b`)
	azdAnyRe    = regexp.MustCompile(`\bazd\s+[a-z]`)
)

// repoFS is how the rule reads the repository: Open and ReadDir below the project root. The default
// opens the root with os.Root, which refuses paths and symlinks that leave it.
type repoFS interface {
	Lstat(name string) (fs.FileInfo, error)
	Open(name string) (fs.File, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Close() error
}

type osRoot struct{ r *os.Root }

func (o osRoot) Lstat(n string) (fs.FileInfo, error) { return o.r.Lstat(n) }
func (o osRoot) Open(n string) (fs.File, error)      { return o.r.Open(n) }
func (o osRoot) Close() error                        { return o.r.Close() }
func (o osRoot) ReadDir(n string) ([]fs.DirEntry, error) {
	return fs.ReadDir(o.r.FS(), n)
}

type ops007 struct {
	open func(root string) (repoFS, error) // nil means the operating system
}

func (ops007) ID() string   { return "FND-OPS-007" }
func (ops007) Version() int { return 1 }

func (r ops007) opener(root string) (repoFS, error) {
	if r.open != nil {
		return r.open(root)
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return osRoot{rt}, nil
}

func readBounded(fsys repoFS, name string) (string, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxPipelineBytes))
	return string(b), err
}

func (r ops007) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	ref := projectRef(in)
	if in.Profile != rules.ProfileTest && in.Profile != rules.ProfileProd {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"a CI/CD pipeline is only expected for the test and prod profiles", "", ref)}, nil
	}
	provider, hasSection := pipelineSection(in.AzureYAML)
	if hasSection && (provider == "" || provider == "github" || provider == "azdo") {
		return []rules.Result{rules.Pass()}, nil
	}
	if in.Project.Root == "" {
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("repository-root-not-available"),
			"the repository root is not available, so pipeline files cannot be looked up", "repository root", ref)}, nil
	}
	fsys, err := r.opener(in.Project.Root)
	if err != nil {
		return []rules.Result{rules.SkipFor(r, rules.ReasonUnresolvedValue("repository-root-not-available"),
			"the repository root cannot be opened", "repository root", ref)}, nil
	}
	defer fsys.Close()

	var present, unverified []string
	for _, p := range pipelineFiles {
		if fi, err := fsys.Lstat(p); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		present = append(present, p)
		if txt, err := readBounded(fsys, p); err == nil && azdDeployRe.MatchString(txt) {
			return []rules.Result{rules.Pass()}, nil
		}
		unverified = append(unverified, p)
	}
	if invoker := workflowInvokingAzd(fsys); invoker != "" {
		return []rules.Result{rules.Pass()}, nil
	}
	if len(present) > 0 {
		f := rules.NewFinding(r, sdk.ResourceRef{Kind: "file", Name: present[0]},
			"Pipeline definition "+strings.Join(unverified, ", ")+" exists but does not invoke azd provision, deploy or up. Other deployment tools are valid, so this is not asserted as a failure.",
			pipelineRecommendation)
		f.Key = "pipeline-without-azd"
		f.Location = sdk.Location{File: present[0]}
		f.Fix = pipelineFix
		f.Confidence = sdk.ConfidenceUncertain
		return []rules.Result{rules.Uncertain(f)}, nil
	}
	if hasSection {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"azure.yaml pipeline.provider is not github or azdo; other CI systems are not detected", "CI systems other than GitHub Actions and Azure Pipelines", ref)}, nil
	}
	f := rules.NewFinding(r, ref,
		"No azd pipeline definition found: none of "+strings.Join(pipelineFiles, ", ")+", no workflow under "+workflowDir+" that invokes azd, and no azure.yaml pipeline section. Only GitHub Actions and Azure Pipelines definitions are detected; other CI systems are not.",
		pipelineRecommendation)
	f.Key = "no-pipeline"
	f.Location = sdk.Location{File: in.Project.AzureYAMLPath}
	f.Fix = pipelineFix
	f.Confidence = sdk.ConfidenceLikely
	return []rules.Result{rules.Fail(f)}, nil
}

const (
	pipelineRecommendation = "Run azd pipeline config to generate azure-dev.yml and the federated credential for GitHub Actions or Azure Pipelines, and use one pipeline per azd environment."
	pipelineFix            = "azd pipeline config --provider github"
)

// pipelineSection reads the top-level azure.yaml pipeline.provider. The default provider is github.
func pipelineSection(a *model.AzureYAML) (provider string, present bool) {
	if a == nil || a.Root == nil {
		return "", false
	}
	n := a.Root.Lookup("pipeline")
	if n == nil {
		return "", false
	}
	if p := a.Root.Lookup("pipeline", "provider"); p != nil && p.Kind == model.NodeScalar {
		return p.Value, true
	}
	return "", true
}

// workflowInvokingAzd returns the first workflow file under .github/workflows that runs azd.
func workflowInvokingAzd(fsys repoFS) string {
	entries, err := fsys.ReadDir(workflowDir)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if e.Type().IsRegular() && (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	if len(names) > maxWorkflowFiles {
		names = names[:maxWorkflowFiles]
	}
	for _, n := range names {
		if txt, err := readBounded(fsys, workflowDir+"/"+n); err == nil && azdAnyRe.MatchString(txt) {
			return workflowDir + "/" + n
		}
	}
	return ""
}
