//go:build spike

package bicep

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Run with:
//
//	BICEP_PATH=<absolute bicep path> go test -tags spike ./internal/bicep/...
//
// Without BICEP_PATH the tests skip; REQUIRE_SPIKE_DEPS=1 turns that into a failure.
func realTool(t *testing.T) (Tool, ExecRunner) {
	t.Helper()
	path := os.Getenv("BICEP_PATH")
	if path == "" {
		if os.Getenv("REQUIRE_SPIKE_DEPS") == "1" {
			t.Fatal("BICEP_PATH is required when REQUIRE_SPIKE_DEPS=1")
		}
		t.Skip("BICEP_PATH not set")
	}
	home := t.TempDir()
	env := map[string]string{
		"PATH": os.Getenv("PATH"), "HOME": home, "USERPROFILE": home,
		"DOTNET_BUNDLE_EXTRACT_BASE_DIR": filepath.Join(home, "net"),
		"SystemRoot":                     os.Getenv("SystemRoot"),
		"TEMP":                           home, "TMP": home,
	}
	runner := ExecRunner{Timeout: 90 * time.Second, Getenv: func(k string) string { return env[k] }}
	tool, err := Discover(context.Background(), DiscoverOptions{
		Path:   path,
		Runner: runner,
		Getenv: func(string) string { return "" },
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return tool, runner
}

func TestRealBicepCompile(t *testing.T) {
	tool, runner := realTool(t)
	c := Compiler{Tool: tool, Runner: runner, Root: "testdata"}
	ctx := context.Background()

	t.Run("main compiles with warnings only", func(t *testing.T) {
		res, err := c.Compile(ctx, filepath.Join("testdata", "main.bicep"))
		if err != nil || !res.OK {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		tmpl, err := ParseARM(res.ARM)
		if err != nil || len(tmpl.Resources) != 5 {
			t.Fatalf("tmpl=%+v err=%v", tmpl, err)
		}
	})
	t.Run("compile error is a normal result", func(t *testing.T) {
		res, err := c.Compile(ctx, filepath.Join("testdata", "error.bicep"))
		if err != nil || res.OK || !HasErrors(res.Diagnostics) {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		var found bool
		for _, d := range res.Diagnostics {
			if d.Code == "BCP057" && d.Line == 11 {
				found = true
			}
		}
		if !found {
			t.Fatalf("BCP057 not reported: %+v", res.Diagnostics)
		}
	})
	t.Run("lint warning fixture", func(t *testing.T) {
		res, err := c.Compile(ctx, filepath.Join("testdata", "lint.bicep"))
		if err != nil || !res.OK || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "no-unused-params" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
	t.Run("missing file is not a dependency error", func(t *testing.T) {
		res, err := c.Compile(ctx, filepath.Join("testdata", "nope.bicep"))
		if err != nil && IsDependencyError(err) {
			t.Fatalf("missing source must not be exit 2: %v", err)
		}
		if err == nil && res.OK {
			t.Fatal("missing file compiled")
		}
	})
}
