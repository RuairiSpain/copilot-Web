//go:build bicep

package bicep

// Run with the real compiler:
//
//	BICEP_PATH=/abs/path/to/bicep go test -tags bicep ./internal/bicep/...

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func realCompiler(t *testing.T, dir string) *Compiler {
	t.Helper()
	path := os.Getenv("BICEP_PATH")
	if path == "" {
		t.Skip("BICEP_PATH not set")
	}
	d := Discover(t.Context(), DiscoverOptions{ExplicitPath: path, Required: true})
	if !d.Available() {
		t.Fatalf("bicep not usable: %+v", d.Status)
	}
	t.Logf("bicep %s", d.Status.Version)
	c, err := NewCompiler(d, CompilerOptions{ProjectDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var fixtureCompiles = []struct{ dir, file, arm string }{
	{"spike", "main.bicep", "main.arm.json"},
	{"foundry", "foundry.bicep", "foundry.arm.json"},
	{"foundry-v2", "foundry.bicep", "foundry.arm.json"},
	{"synthetic", "main.bicep", "main.arm.json"},
}

func TestCompileFixtures(t *testing.T) {
	for _, fc := range fixtureCompiles {
		t.Run(fc.dir, func(t *testing.T) {
			c := realCompiler(t, fixturePath(fc.dir))
			res, err := c.Compile(t.Context(), fc.file)
			if err != nil || !res.OK {
				t.Fatalf("%v %+v", err, res.Diagnostics)
			}
			if HasError(res.Diagnostics) {
				t.Errorf("errors: %+v", res.Diagnostics)
			}
			if _, err := Normalise(res.ARM, Options{File: fc.file}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompileDiagnosticFixtures(t *testing.T) {
	c := realCompiler(t, fixturePath("spike"))
	res, err := c.Compile(t.Context(), "error.bicep")
	if err != nil || res.OK || !HasError(res.Diagnostics) || len(res.Skipped) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	want := map[string]model.Pos{"BCP037": {Line: 8, Column: 5}, "BCP057": {Line: 11, Column: 21}}
	for _, d := range res.Diagnostics {
		if p, ok := want[d.Code]; ok {
			if d.Pos != p || d.File != "error.bicep" {
				t.Errorf("%s at %+v in %s", d.Code, d.Pos, d.File)
			}
			delete(want, d.Code)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing diagnostics %v in %+v", want, res.Diagnostics)
	}
	res, err = c.Compile(t.Context(), "secure-output.bicep")
	if err != nil || !res.OK {
		t.Fatalf("%v", err)
	}
	var lines []int
	for _, d := range res.Diagnostics {
		if d.Code == "outputs-should-not-contain-secrets" {
			lines = append(lines, d.Pos.Line)
			if f := ToFinding(d, "foundry-prod"); f.RuleID != "bicep/outputs-should-not-contain-secrets" || f.Severity != sdk.SeverityWarning {
				t.Errorf("finding = %+v", f)
			}
		}
	}
	if len(lines) != 2 || lines[0] != 8 || lines[1] != 11 {
		t.Errorf("secret output lines = %v", lines)
	}
}

func TestCompileParamsEnvVars(t *testing.T) {
	t.Setenv("FD_FIXTURE_VALUE", "leaked-if-forwarded")
	c := realCompiler(t, fixturePath("params"))
	res, err := c.CompileParams(t.Context(), "readenv.bicepparam")
	if err != nil || res.OK || len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipParamEnvVar {
		t.Fatalf("%v %+v", err, res)
	}
	var sawBCP427 bool
	for _, d := range res.Diagnostics {
		sawBCP427 = sawBCP427 || isEnvVarMissing(d)
	}
	if !sawBCP427 {
		t.Errorf("no BCP427 in %+v", res.Diagnostics)
	}
	res, err = c.CompileParams(t.Context(), "readenv-default.bicepparam")
	if err != nil || !res.OK {
		t.Fatalf("%v %+v", err, res)
	}
	if res.Values["token"] != "fixture-default" || res.Values["region"] != "northeurope" {
		t.Errorf("values = %v", res.Values)
	}
	if strings.Contains(string(res.Parameters), "leaked-if-forwarded") {
		t.Error("the host environment reached the compiler")
	}
	n, err := Normalise(res.Template, Options{File: "m.bicep", ParameterValues: res.Values})
	if err != nil || len(n.Template.Outputs) != 1 {
		t.Errorf("%v %+v", err, n)
	}
}

// canonical renders the normalised form deterministically for comparison.
func canonical(t *testing.T, raw []byte, file string) string {
	t.Helper()
	n, err := Normalise(raw, Options{File: file})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(struct {
		T *model.ARMTemplate
		I []ResourceInfo
		P []ParameterInfo
	}{n.Template, n.Info, n.Parameters}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFixtureARMFresh(t *testing.T) {
	for _, fc := range fixtureCompiles {
		t.Run(fc.dir, func(t *testing.T) {
			c := realCompiler(t, fixturePath(fc.dir))
			res, err := c.Compile(t.Context(), fc.file)
			if err != nil || !res.OK {
				t.Fatalf("%v", err)
			}
			committed, err := os.ReadFile(filepath.Join(fixturePath(fc.dir), fc.arm))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := canonical(t, res.ARM, fc.file), canonical(t, committed, fc.file); got != want {
				t.Errorf("committed %s/%s is stale; run scripts/ci/regen-bicep-fixtures.sh", fc.dir, fc.arm)
			}
		})
	}
}
