package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// fixedNow is the injected clock of every test.
var fixedNow = time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)

func now() time.Time { return fixedNow }

// fakeRule is a rule injected through the registry. The IDs and versions are real catalogue entries, because the
// registry refuses an implementation that has no catalogue rule.
type fakeRule struct {
	id  string
	fn  func(in *model.Input) ([]rules.Result, error)
	ver int
}

func (f fakeRule) ID() string { return f.id }
func (f fakeRule) Version() int {
	if f.ver != 0 {
		return f.ver
	}
	return 1
}
func (f fakeRule) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	return f.fn(in)
}

// failing returns a rule that reports one finding on resource service/api.
func failing(id, evidence string) fakeRule {
	var r fakeRule
	r = fakeRule{id: id, fn: func(*model.Input) ([]rules.Result, error) {
		f := rules.NewFinding(r, sdk.ResourceRef{Kind: "service", Name: "api"}, evidence, "")
		f.Location = sdk.Location{File: "azure.yaml", Line: 3}
		f.Key = "k1"
		return []rules.Result{rules.Fail(f)}, nil
	}}
	return r
}

func passing(id string) fakeRule {
	return fakeRule{id: id, fn: func(*model.Input) ([]rules.Result, error) { return []rules.Result{rules.Pass()}, nil }}
}

func panicking(id string) fakeRule {
	return fakeRule{id: id, fn: func(*model.Input) ([]rules.Result, error) { panic("boom") }}
}

func catalogue(t *testing.T, rs ...fakeRule) *engine.Catalogue {
	t.Helper()
	impls := make([]rules.Rule, len(rs))
	for i, r := range rs {
		impls[i] = r
	}
	c, err := engine.NewCatalogue(context.Background(), impls)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

const minimalAzureYAML = "name: sample\nservices:\n  api:\n    host: containerapp\n    project: ./src\n"

// project writes files into a fresh temp directory (with symlinks resolved) and returns it.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["azure.yaml"]; !ok {
		files["azure.yaml"] = minimalAzureYAML
	}
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// base returns Options for dir with the fake clock, a tool header and the given rules selected.
func base(dir string, cat *engine.Catalogue, selector string) app.Options {
	o := app.Options{
		Dir: dir, Now: now, Catalogue: cat,
		Tool: sdk.ToolInfo{Name: "foundry-doctor", Version: "v0.0.0-test"},
	}
	if selector != "" {
		o.Rules = []string{selector}
	}
	return o
}

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, o app.Options) result {
	t.Helper()
	var out, errb bytes.Buffer
	err := app.Run(context.Background(), o, &out, &errb)
	return result{code: app.CodeOf(err), stdout: out.String(), stderr: errb.String()}
}

// fakeBicep is a Runner that answers `--version` and `build` and returns the ARM JSON given.
type fakeBicep struct {
	arm      string
	stderr   string
	exitCode int
	specs    []bicep.RunSpec
}

func (f *fakeBicep) Run(_ context.Context, spec bicep.RunSpec) (bicep.RunResult, error) {
	f.specs = append(f.specs, spec)
	if len(spec.Args) > 0 && spec.Args[0] == "--version" {
		return bicep.RunResult{Stdout: []byte("Bicep CLI version 0.30.3 (abcdef)\n")}, nil
	}
	if f.exitCode != 0 {
		return bicep.RunResult{ExitCode: f.exitCode, Stderr: []byte(f.stderr)}, nil
	}
	return bicep.RunResult{Stdout: []byte(f.arm), Stderr: []byte(f.stderr)}, nil
}

const armOneResource = `{"$schema":"https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#","contentVersion":"1.0.0.0","resources":[{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"st1","location":"westeurope","properties":{}}]}`

// withBicep makes Discover find an executable stub and routes the compiler through the fake runner.
func withBicep(t *testing.T, o app.Options, f *fakeBicep) app.Options {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "bicep")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	o.Bicep = app.BicepOptions{LookPath: func(string) (string, error) { return exe, nil }, Runner: f}
	return o
}

func contains(t *testing.T, name, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s does not contain %q:\n%s", name, w, got)
		}
	}
}

func notContains(t *testing.T, name, got string, bad ...string) {
	t.Helper()
	for _, w := range bad {
		if strings.Contains(got, w) {
			t.Errorf("%s contains %q:\n%s", name, w, got)
		}
	}
}
