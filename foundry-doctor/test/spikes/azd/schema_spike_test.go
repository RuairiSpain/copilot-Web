//go:build spike

package azdspike

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
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
		spikeDependencyUnavailable(t, "AZURE_DEV_DIR must name an absolute azure-dev clone at "+pinnedAzureDevCommit)
	}
	assertPinnedAzureDevCheckout(t, clone)
	script, err := filepath.Abs("validate_schema.py")
	if err != nil {
		t.Fatal(err)
	}
	run := func(file string) (string, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "python3", script, clone, file)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
		out, err := cmd.CombinedOutput()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			spikeDependencyUnavailable(t, "cannot run required python3 schema dependency: "+err.Error())
		}
		return string(out), code
	}

	valid := filepath.Join("valid-azure-yaml-only", "azure.yaml")
	out, code := run(valid)
	if code == 2 {
		spikeDependencyUnavailable(t, "required Python schema environment is not usable: "+out)
	}
	if code != 0 {
		t.Fatalf("valid fixture rejected by the azd schema (exit %d):\n%s", code, out)
	}

	src, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	broken := regexp.MustCompile(`(?m)^[ \t]+project:[^\r\n]*(?:\r?\n|$)`).ReplaceAllString(string(src), "")
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
