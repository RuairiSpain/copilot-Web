package main

import (
	osexec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestVersionHonoursLinkerFlag builds the command with -ldflags "-X main.version=..."
// the way scripts/ci/build-matrix.sh does and runs `version`.
func TestVersionHonoursLinkerFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	goBin, err := osexec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "fd")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := osexec.Command(goBin, "build", "-ldflags", "-X main.version=7.8.9-test", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := osexec.Command(bin, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "foundry-doctor 7.8.9-test\n") {
		t.Fatalf("version output = %q", out)
	}
}

func TestDefaultVersionIsLiteral(t *testing.T) {
	if version != "0.1.0-dev" {
		t.Fatalf("default version = %q", version)
	}
}

func TestAzdVersionFlag(t *testing.T) {
	dir := project(t)
	if code, _, errs := exec(t, "doctor", "--local", "--azd-version", "soon", "--dir", dir); code != 2 || !strings.Contains(errs, "semantic version") {
		t.Fatalf("bad --azd-version exit=%d stderr=%q", code, errs)
	}
	code, out, _ := exec(t, "doctor", "--local", "--azd-version", "1.35.0", "--format", "json", "--dir", dir)
	if code != 0 && code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(out, "azd-version-unknown") {
		t.Fatalf("azd-version-unknown reported although --azd-version was given:\n%s", out)
	}
	t.Setenv("FOUNDRY_DOCTOR_AZD_VERSION", "1.35.0")
	if _, out, _ := exec(t, "doctor", "--local", "--format", "json", "--dir", dir); strings.Contains(out, "azd-version-unknown") {
		t.Fatalf("azd-version-unknown reported although env var was set:\n%s", out)
	}
}

func TestMissingAzdIsReported(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("FOUNDRY_DOCTOR_AZD_VERSION", "")
	dir := project(t)
	code, out, _ := exec(t, "doctor", "--local", "--format", "console", "--dir", dir)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "versions not detected: azd") {
		t.Fatalf("console header lacks the versions note:\n%s", out)
	}
	_, js, _ := exec(t, "doctor", "--local", "--format", "json", "--dir", dir)
	if !strings.Contains(js, `"azd-version-unknown"`) {
		t.Fatalf("json lacks azd-version-unknown:\n%s", js)
	}
}
