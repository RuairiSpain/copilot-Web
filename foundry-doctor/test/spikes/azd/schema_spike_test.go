//go:build spike

package azdspike

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidFixtureMatchesAzdSchema validates the valid fixture against schemas/v1.0/azure.yaml.json and the
// extension schemas it references, read from a local azure-dev clone. It is skipped unless AZURE_DEV_DIR names
// that clone and python3 has pyyaml and jsonschema. It also proves the check can fail: a copy of the fixture
// with the agent's required `project` removed must be rejected.
//
//	AZURE_DEV_DIR=/path/to/azure-dev go test -tags spike ./test/spikes/azd/
func TestValidFixtureMatchesAzdSchema(t *testing.T) {
	clone := os.Getenv("AZURE_DEV_DIR")
	if clone == "" || !filepath.IsAbs(clone) {
		t.Skip("AZURE_DEV_DIR (absolute path to an azure-dev clone) not set")
	}
	script, err := filepath.Abs("validate_schema.py")
	if err != nil {
		t.Fatal(err)
	}
	run := func(file string) (string, int) {
		cmd := exec.Command("python3", script, clone, file)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("cannot run python3: %v", err)
		}
		return string(out), code
	}

	valid := filepath.Join("valid-azure-yaml-only", "azure.yaml")
	out, code := run(valid)
	if code == 2 {
		t.Skipf("python environment not usable: %s", out)
	}
	if code != 0 {
		t.Fatalf("valid fixture rejected by the azd schema (exit %d):\n%s", code, out)
	}

	src, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(src), "    project: ./agents/assistant\n", "", 1)
	if broken == string(src) {
		t.Fatal("fixture no longer contains the line this test removes")
	}
	bad := filepath.Join(t.TempDir(), "azure.yaml")
	if err := os.WriteFile(bad, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code = run(bad)
	if code != 1 || !strings.Contains(out, "project") {
		t.Fatalf("schema should reject an agent without `project` (exit %d):\n%s", code, out)
	}
}
