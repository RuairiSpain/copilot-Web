package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	testARM        = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","contentVersion":"1.0.0.0","metadata":{"_generator":{"name":"bicep","version":"0.48.1","templateHash":"111"}},"resources":[]}`
	testARMNewHash = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","contentVersion":"1.0.0.0","metadata":{"_generator":{"name":"bicep","version":"0.48.1","templateHash":"222"}},"resources":[]}`
	testARMDrift   = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","contentVersion":"1.0.0.0","metadata":{"_generator":{"name":"bicep","version":"0.48.1","templateHash":"111"}},"resources":[],"outputs":{"x":{"type":"string","value":"changed"}}}`
	bannerText     = "Do not deploy this file."
)

// annotateRunner is a bicep.Runner that serves one result for the original
// project and, optionally, another for builds inside a .review copy.
type annotateRunner struct {
	root     string
	original bicep.RunResult
	review   *bicep.RunResult
	calls    []string
}

func (r *annotateRunner) Run(_ context.Context, _ string, args []string, dir string) (bicep.RunResult, error) {
	if args[0] == "--version" {
		return bicep.RunResult{Stdout: []byte("Bicep CLI version 0.48.1 (test)")}, nil
	}
	rel, err := filepath.Rel(r.root, filepath.Join(dir, args[1]))
	if err != nil {
		return bicep.RunResult{}, err
	}
	rel = filepath.ToSlash(rel)
	r.calls = append(r.calls, args[0]+" "+rel)
	if args[0] != "build" {
		return bicep.RunResult{ExitCode: 2}, nil
	}
	if r.review != nil && strings.Contains(rel, ".review/") {
		return *r.review, nil
	}
	return r.original, nil
}

func (r *annotateRunner) built(rel string) bool { return slices.Contains(r.calls, "build "+rel) }

func (r *annotateRunner) anyBuild() bool {
	for _, c := range r.calls {
		if strings.HasPrefix(c, "build") {
			return true
		}
	}
	return false
}

func okRunner() *annotateRunner {
	return &annotateRunner{original: bicep.RunResult{Stdout: []byte(testARM)}}
}

func annotateSvc(t *testing.T, dir string, src Source, run *annotateRunner, fs ...sdk.Finding) Services {
	t.Helper()
	svc, _ := newSvc(&fakeEngine{out: RunOutput{Findings: fs}})
	if run != nil {
		run.root = dir
		fakeExe := filepath.Join(t.TempDir(), "bicep")
		if err := os.WriteFile(fakeExe, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		svc.ARM = armLoader{disc: bicep.DiscoverOptions{Path: fakeExe, Runner: run}, runner: run}
	}
	src.Dir = dir
	svc.Project = fakeProject{src: src}
	return svc
}

func readText(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustNotExist(t *testing.T, dir, rel string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s must not exist: %v", rel, err)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestResolveInfra(t *testing.T) {
	tests := []struct {
		name, yaml, at, cfg string
		wantDir, wantEntry  string
		wantOK, wantBicep   bool
	}{
		{"defaults", "name: demo\n", "azure.yaml", "infra", "infra", "infra/main.bicep", true, true},
		{"path and bare module", "name: demo\ninfra:\n  path: deploy\n  module: app\n", "azure.yaml", "infra", "deploy", "deploy/app.bicep", true, true},
		{"module with extension", "name: demo\ninfra:\n  path: ./deploy\n  module: app.bicep\n", "azure.yaml", "", "deploy", "deploy/app.bicep", true, true},
		{"azure.yaml in subdirectory", "name: demo\n", "app/azure.yaml", "infra", "app/infra", "app/infra/main.bicep", true, true},
		{"subdirectory with infra.path", "name: demo\ninfra:\n  path: deploy\n", "app/azure.yaml", "infra", "app/deploy", "app/deploy/main.bicep", true, true},
		{"config override wins", "name: demo\ninfra:\n  path: deploy\n", "azure.yaml", "iac", "iac", "iac/main.bicep", true, true},
		{"terraform", "name: demo\ninfra:\n  provider: terraform\n", "azure.yaml", "infra", "infra", "infra/main.bicep", true, false},
		{"extension provider", "name: demo\ninfra:\n  provider: microsoft.foundry\n", "azure.yaml", "infra", "infra", "infra/main.bicep", true, false},
		{"absolute path", "name: demo\ninfra:\n  path: /etc\n", "azure.yaml", "infra", "", "", false, true},
		{"escaping path", "name: demo\ninfra:\n  path: ../outside\n", "azure.yaml", "infra", "", "", false, true},
		{"escaping module", "name: demo\ninfra:\n  module: ../main\n", "azure.yaml", "infra", "", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := azureyaml.Parse([]byte(tt.yaml), tt.at)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := resolveInfra(Source{AzureYAMLAt: tt.at, InfraPath: tt.cfg}, doc)
			if ok != tt.wantOK || got.isBicep() != tt.wantBicep {
				t.Fatalf("resolveInfra() = %+v, ok=%v bicep=%v", got, ok, got.isBicep())
			}
			if ok && (got.Dir != tt.wantDir || got.Entry != tt.wantEntry) {
				t.Fatalf("resolveInfra() = %+v, want dir %q entry %q", got, tt.wantDir, tt.wantEntry)
			}
			entry, eok := resolveBicepEntry(Source{AzureYAMLAt: tt.at, InfraPath: tt.cfg}, doc)
			if eok != (tt.wantOK && tt.wantBicep) || (eok && entry != tt.wantEntry) {
				t.Fatalf("resolveBicepEntry() = %q, %v", entry, eok)
			}
		})
	}
}

func TestAnnotateReviewUsesAzureYAMLInfraPathAndModule(t *testing.T) {
	dir := t.TempDir()
	yml := "name: demo\ninfra:\n  path: deploy\n  module: app\n"
	bicepSrc := "param location string = 'westeurope'\n"
	writeFile(t, dir, "azure.yaml", yml)
	writeFile(t, dir, "deploy/app.bicep", bicepSrc)
	run := okRunner()
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, run)
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "review/x.review", Dir: dir}, io.Discard); err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !run.built("deploy/app.bicep") || !run.built("review/x.review/deploy/app.bicep") {
		t.Fatalf("the original and the review copy must both compile; calls=%v", run.calls)
	}
	if got := readText(t, dir, "review/x.review/deploy/app.bicep"); !strings.Contains(got, bannerText) || !strings.HasSuffix(got, bicepSrc) {
		t.Fatalf("bicep copy:\n%s", got)
	}
	if got := readText(t, dir, "review/x.review/azure.review.yaml"); !strings.HasPrefix(got, "# Foundry Doctor review copy") {
		t.Fatalf("azure.yaml copy must always carry the banner:\n%s", got)
	}
	mustNotExist(t, dir, "review/x.review/azure.yaml")
	if readText(t, dir, "azure.yaml") != yml || readText(t, dir, "deploy/app.bicep") != bicepSrc {
		t.Fatal("originals changed")
	}
}

func TestAnnotateReviewAzureYAMLInSubdirectory(t *testing.T) {
	dir := t.TempDir()
	yml := "name: demo\n"
	writeFile(t, dir, "app/azure.yaml", yml)
	writeFile(t, dir, "app/infra/main.bicep", "param location string = 'westeurope'\n")
	run := okRunner()
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "app/azure.yaml", InfraPath: "infra"}, run)
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "review/sub.review", Dir: dir}, io.Discard); err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !run.built("app/infra/main.bicep") || !run.built("review/sub.review/app/infra/main.bicep") {
		t.Fatalf("calls=%v", run.calls)
	}
	readText(t, dir, "review/sub.review/app/azure.review.yaml")
	mustNotExist(t, dir, "review/sub.review/app/azure.yaml")
}

func TestAnnotateNonBicepProviderIsNotCompiled(t *testing.T) {
	dir := t.TempDir()
	yml := "name: demo\ninfra:\n  provider: terraform\n"
	writeFile(t, dir, "azure.yaml", yml)
	writeFile(t, dir, "infra/main.bicep", "param location string = 'westeurope'\n")
	writeFile(t, dir, "infra/main.tf", "# terraform\n")
	run := okRunner()
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, run)
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "tf.review", Dir: dir}, io.Discard); err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if run.anyBuild() {
		t.Fatalf("a terraform project must not be compiled with Bicep; calls=%v", run.calls)
	}
	if got := readText(t, dir, "tf.review/infra/main.tf"); got != "# terraform\n" {
		t.Fatalf("terraform file copy changed: %q", got)
	}
}

func TestAnnotateFileLoadGuardBlocksBothCompiles(t *testing.T) {
	for _, format := range []string{"review", "github", "sarif"} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			yml := "name: demo\n"
			writeFile(t, dir, "azure.yaml", yml)
			writeFile(t, dir, "infra/main.bicep", hostileLoad)
			run := okRunner()
			svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, run)
			req := AnnotateRequest{Format: format, Dir: dir}
			if format == "review" {
				req.Out = "guard.review"
			}
			code, err := Annotate(context.Background(), svc, req, io.Discard)
			if format == "review" {
				if code != ExitUnavailable || err == nil || !strings.Contains(err.Error(), bicep.ReasonFileLoadOutsideProject) {
					t.Fatalf("code=%d err=%v", code, err)
				}
				mustNotExist(t, dir, "guard.review")
			} else if err != nil || code != ExitOK {
				t.Fatalf("code=%d err=%v", code, err)
			}
			if run.anyBuild() {
				t.Fatalf("a refused entry must never reach the Bicep CLI; calls=%v", run.calls)
			}
		})
	}
}

func TestAnnotateReviewValidation(t *testing.T) {
	result := func(stdout, stderr string, exit int) *bicep.RunResult {
		return &bicep.RunResult{Stdout: []byte(stdout), Stderr: []byte(stderr), ExitCode: exit}
	}
	tests := []struct {
		name    string
		review  *bicep.RunResult
		wantErr string
	}{
		{"identical", nil, ""},
		{"generator metadata only", result(testARMNewHash, "", 0), ""},
		{"arm drift", result(testARMDrift, "", 0), "changed compiled ARM output"},
		{"compile status", result("", "main.bicep(2,1) : Error BCP057: The name \"x\" does not exist in the current context.\n", 1), "changed compile status"},
		{"diagnostic drift", result(testARM, "main.bicep(2,1) : Warning no-unused-params: Parameter \"location\" is declared but never used.\n", 0), "changed compiler diagnostics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			yml := "name: demo\n"
			bicepSrc := "param location string = 'westeurope'\n"
			writeFile(t, dir, "azure.yaml", yml)
			writeFile(t, dir, "infra/main.bicep", bicepSrc)
			run := okRunner()
			run.review = tt.review
			svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, run)
			code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "v.review", Dir: dir}, io.Discard)
			if !run.built("v.review/infra/main.bicep") {
				t.Fatalf("review copy not compiled; calls=%v", run.calls)
			}
			if tt.wantErr == "" {
				if err != nil || code != ExitOK {
					t.Fatalf("code=%d err=%v", code, err)
				}
				readText(t, dir, "v.review/annotations-manifest.json")
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || code == ExitOK {
				t.Fatalf("code=%d err=%v, want %q", code, err, tt.wantErr)
			}
			mustNotExist(t, dir, "v.review")
			if readText(t, dir, "infra/main.bicep") != bicepSrc {
				t.Fatal("original changed")
			}
		})
	}
}

func TestAnnotateReviewDetectsYAMLBlockScalarDrift(t *testing.T) {
	dir := t.TempDir()
	yml := "name: demo\nmetadata:\n  template: |\n    one\n    two\n"
	writeFile(t, dir, "azure.yaml", yml)
	f := sdk.Finding{RuleID: "FND-CFG-001", Severity: sdk.SeverityWarning, Location: sdk.Location{File: "azure.yaml", Line: 5, Column: 1}, Evidence: "inside a block scalar", Fingerprint: "fp-block"}
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil, f)
	code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "drift.review", Dir: dir}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "changed YAML semantics") || code == ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	mustNotExist(t, dir, "drift.review")
	if readText(t, dir, "azure.yaml") != yml {
		t.Fatal("original changed")
	}
}

func TestAnnotateReviewAbsoluteOutInsideProject(t *testing.T) {
	dir := t.TempDir()
	yml := "name: demo\n"
	writeFile(t, dir, "azure.yaml", yml)
	writeFile(t, dir, "infra/main.bicep", "param location string = 'westeurope'\n")
	run := okRunner()
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(yml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, run)
	out := filepath.Join(dir, "review", "abs.review")
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: out, Dir: dir}, io.Discard); err != nil || code != ExitOK {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if !run.built("review/abs.review/infra/main.bicep") {
		t.Fatalf("the absolute --out copy must be validated; calls=%v", run.calls)
	}
	readText(t, dir, "review/abs.review/annotations-manifest.json")

	var sarifOut bytes.Buffer
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Out: filepath.Join(dir, "abs.sarif"), Dir: dir}, &sarifOut); err != nil || code != ExitOK {
		t.Fatalf("sarif code=%d err=%v", code, err)
	}
	readText(t, dir, "abs.sarif")
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Out: filepath.Join(t.TempDir(), "x.sarif"), Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("absolute --out outside the project: code=%d err=%v", code, err)
	}
}

func TestAnnotateSARIFOmitsRegionForManifestOnlyEntries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", minimalYAML)
	fs := []sdk.Finding{
		{RuleID: "FND-NET-001", Severity: sdk.SeverityWarning, Location: sdk.Location{File: "infra/main.bicep", Line: 4, Column: 2}, Evidence: "50% of the quota", Fingerprint: "fp-bicep"},
		{RuleID: "FND-CFG-001", Severity: sdk.SeverityWarning, Location: sdk.Location{File: "azure.yaml", Line: 2, Column: 1}, Evidence: "inline", Fingerprint: "fp-yaml"},
	}
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil, fs...)
	var out bytes.Buffer
	if _, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Dir: dir}, &out); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Runs []struct {
			Results []struct {
				RuleID  string `json:"ruleId"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region *struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &log); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	seen := 0
	for _, r := range log.Runs[0].Results {
		switch r.RuleID {
		case "FND-NET-001":
			seen++
			if len(r.Locations) != 1 || r.Locations[0].PhysicalLocation.Region != nil {
				t.Fatalf("untrusted-bicep-location must have no region: %+v", r.Locations)
			}
			if !strings.Contains(r.Message.Text, "50%") || strings.Contains(r.Message.Text, "%25") {
				t.Fatalf("SARIF text must be plain, got %q", r.Message.Text)
			}
		case "FND-CFG-001":
			seen++
			if len(r.Locations) != 1 || r.Locations[0].PhysicalLocation.Region == nil || r.Locations[0].PhysicalLocation.Region.StartLine != 2 {
				t.Fatalf("inline entry must keep its region: %+v", r.Locations)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("results missing:\n%s", out.String())
	}
}

func TestAnnotateFileOutputRefusesInputAliases(t *testing.T) {
	newProject := func(t *testing.T) (string, Services) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", minimalYAML)
		writeFile(t, dir, "infra/main.bicep", "param location string = 'westeurope'\n")
		f := sdk.Finding{RuleID: "FND-CFG-001", Severity: sdk.SeverityWarning, Location: sdk.Location{File: "azure.yaml", Line: 1, Column: 1}, Evidence: "x", Fingerprint: "fp"}
		return dir, annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil, f)
	}
	t.Run("symlinked out file", func(t *testing.T) {
		dir, svc := newProject(t)
		symlinkOrSkip(t, "azure.yaml", filepath.Join(dir, "annotate.txt"))
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "github", Out: "annotate.txt", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
			t.Fatalf("code=%d err=%v", code, err)
		}
		if readText(t, dir, "azure.yaml") != minimalYAML {
			t.Fatal("azure.yaml overwritten through a symlink")
		}
	})
	t.Run("symlinked out ancestor", func(t *testing.T) {
		dir, svc := newProject(t)
		symlinkOrSkip(t, "infra", filepath.Join(dir, "alias"))
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Out: "alias/main.bicep", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
			t.Fatalf("code=%d err=%v", code, err)
		}
		if !strings.HasPrefix(readText(t, dir, "infra/main.bicep"), "param location") {
			t.Fatal("infra/main.bicep overwritten through a symlinked directory")
		}
	})
	t.Run("hard link to azure.yaml", func(t *testing.T) {
		dir, svc := newProject(t)
		if err := os.Link(filepath.Join(dir, "azure.yaml"), filepath.Join(dir, "linked.sarif")); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Out: "linked.sarif", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
			t.Fatalf("code=%d err=%v", code, err)
		}
		if readText(t, dir, "azure.yaml") != minimalYAML {
			t.Fatal("azure.yaml overwritten through a hard link")
		}
	})
	t.Run("case alias of infra", func(t *testing.T) {
		dir, svc := newProject(t)
		if _, err := os.Stat(filepath.Join(dir, "INFRA")); err != nil {
			t.Skip("case-sensitive file system")
		}
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "INFRA/x.review", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
			t.Fatalf("code=%d err=%v", code, err)
		}
		mustNotExist(t, dir, "infra/x.review")
	})
	t.Run("regular out file is replaced", func(t *testing.T) {
		dir, svc := newProject(t)
		writeFile(t, dir, "out/annotate.txt", "stale\n")
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "github", Out: "out/annotate.txt", Dir: dir}, io.Discard); err != nil || code != ExitOK {
			t.Fatalf("code=%d err=%v", code, err)
		}
		if got := readText(t, dir, "out/annotate.txt"); !strings.HasPrefix(got, "::warning file=azure.yaml") {
			t.Fatalf("out = %q", got)
		}
	})
}

func TestAnnotateReviewKeepsForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "azure.yaml", minimalYAML)
	writeFile(t, dir, "docs.review/notes.txt", "user content\n")
	svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil)
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "docs.review", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if readText(t, dir, "docs.review/notes.txt") != "user content\n" {
		t.Fatal("user directory was deleted")
	}
}

func TestAnnotateReviewRefusesSymlinkedInputs(t *testing.T) {
	t.Run("azure.yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "real/azure.yaml", minimalYAML)
		symlinkOrSkip(t, filepath.Join("real", "azure.yaml"), filepath.Join(dir, "azure.yaml"))
		svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil)
		if _, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "s.review", Dir: dir}, io.Discard); err == nil {
			t.Fatal("a symlinked azure.yaml must be refused")
		}
		mustNotExist(t, dir, "s.review")
	})
	t.Run("file inside infra", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", minimalYAML)
		writeFile(t, dir, "secret.txt", "outside infra\n")
		writeFile(t, dir, "infra/main.json", "{}\n")
		symlinkOrSkip(t, filepath.Join("..", "secret.txt"), filepath.Join(dir, "infra", "link.txt"))
		svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil)
		if _, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "s.review", Dir: dir}, io.Discard); err == nil {
			t.Fatal("a symlink inside infra must be refused")
		}
		mustNotExist(t, dir, "s.review")
	})
	t.Run("infra directory", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", minimalYAML)
		writeFile(t, dir, "elsewhere/main.json", "{}\n")
		symlinkOrSkip(t, "elsewhere", filepath.Join(dir, "infra"))
		svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil)
		if _, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "s.review", Dir: dir}, io.Discard); err == nil {
			t.Fatal("a symlinked infra directory must be refused")
		}
		mustNotExist(t, dir, "s.review")
	})
	t.Run("output ancestor", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "azure.yaml", minimalYAML)
		if err := os.MkdirAll(filepath.Join(dir, "target"), 0o755); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, "target", filepath.Join(dir, "link"))
		svc := annotateSvc(t, dir, Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml", InfraPath: "infra"}, nil)
		if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "link/s.review", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
			t.Fatalf("code=%d err=%v", code, err)
		}
		mustNotExist(t, dir, "target/s.review")
	})
}
