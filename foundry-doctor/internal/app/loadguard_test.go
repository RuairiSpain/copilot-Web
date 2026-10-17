package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

const hostileLoad = "param prefix = loadTextContent('../../outside.txt')\n"

func TestHostileBicepparamIsNotCompiled(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\n"+hostileLoad)
	run.params["infra/main.bicepparam"] = `{"parametersJson":"{\"parameters\":{\"prefix\":{\"value\":\"SECRETOUTSIDE\"}}}"}`
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if run.called("build-params") {
		t.Error("a bicepparam with an escaping load must not reach the Bicep CLI")
	}
	if !strings.Contains(got.log, bicep.ReasonFileLoadOutsideProject) {
		t.Errorf("the refusal must be named: %q", got.log)
	}
	for _, r := range got.model.Resources() {
		if strings.Contains(r.Name, "SECRETOUTSIDE") {
			t.Errorf("outside content reached the model: %q", r.Name)
		}
	}
}

func TestBicepparamLoadInsideProjectAllowed(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/prefix.txt", "demo")
	writeFile(t, dir, "infra/main.bicepparam", "using './main.bicep'\nparam prefix = loadTextContent('prefix.txt')\n")
	run.params["infra/main.bicepparam"] = `{"parametersJson":"{\"parameters\":{\"prefix\":{\"value\":\"demo\"}}}"}`
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if !run.called("build-params infra/main.bicepparam") {
		t.Error("a load inside the project must be allowed")
	}
}

func TestHostileEntryBicepIsNotCompiled(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.bicep", hostileLoad)
	got := loadProject(t, dir, "", run)
	if !errors.Is(got.err, ErrARMUnavailable) || !errors.Is(got.err, ErrBicepFailed) {
		t.Fatalf("err = %v, want ErrARMUnavailable and ErrBicepFailed (so validation.bicep=required exits 2)", got.err)
	}
	if run.called("build ") {
		t.Error("the entry must not be compiled")
	}
	if !strings.Contains(got.log, bicep.ReasonFileLoadOutsideProject) {
		t.Errorf("the refusal must be named: %q", got.log)
	}
}

func TestHostileModuleReachedFromEntryIsNotCompiled(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/main.bicep", "module m './mod/m.bicep' = {\n  name: 'm'\n}\n")
	writeFile(t, dir, "infra/mod/m.bicep", "var x = loadFileAsBase64('/etc/hostname')\n")
	got := loadProject(t, dir, "", run)
	if !errors.Is(got.err, ErrBicepFailed) {
		t.Fatalf("err = %v, want ErrBicepFailed", got.err)
	}
}

func TestHostileModuleFileNotCompiledAlone(t *testing.T) {
	dir, run := paramsProject(t)
	writeFile(t, dir, "infra/other/loose.bicep", hostileLoad)
	got := loadProject(t, dir, "", run)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if run.called("build infra/other/loose.bicep") {
		t.Error("a module file with an escaping load must not be compiled alone")
	}
	if !strings.Contains(got.log, bicep.ReasonFileLoadOutsideProject) {
		t.Errorf("the refusal must be named: %q", got.log)
	}
}
