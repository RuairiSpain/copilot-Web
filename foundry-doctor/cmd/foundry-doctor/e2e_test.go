package main

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
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/app"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
)

func sample(name string) string { return filepath.Join("..", "..", "samples", name) }

type e2eReport struct {
	ExitCode int `json:"exitCode"`
	Findings []struct {
		RuleID string `json:"ruleId"`
	} `json:"findings"`
	Skipped []struct {
		RuleID string `json:"ruleId"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

func runJSON(t *testing.T, svc app.Services, args ...string) (int, e2eReport, string) {
	t.Helper()
	t.Setenv("FOUNDRY_DOCTOR_ENVIRONMENT", "")
	t.Setenv("AZURE_ENV_NAME", "")
	var out, errb bytes.Buffer
	code := run(context.Background(), append(args, "--format", "json"), &out, &errb, svc)
	var r e2eReport
	if out.Len() > 0 {
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, out.String())
		}
	}
	return code, r, errb.String()
}

func ids(r e2eReport) []string {
	var s []string
	for _, f := range r.Findings {
		s = append(s, f.RuleID)
	}
	slices.Sort(s)
	return s
}

// noBicep forces the "bicep CLI missing" path regardless of the host.
func noBicep(stderr io.Writer) app.Services {
	return app.NewServices(stderr, app.Options{
		AzdVersion: func(context.Context) string { return "1.34.2" },
		Bicep: bicep.DiscoverOptions{LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		}},
	})
}

type fakeBicep struct{ arm []byte }

func (f fakeBicep) Run(_ context.Context, _ string, args []string, _ string) (bicep.RunResult, error) {
	if slices.Contains(args, "--version") {
		return bicep.RunResult{Stdout: []byte("Bicep CLI version 0.48.1 (abc)\n")}, nil
	}
	return bicep.RunResult{Stdout: f.arm}, nil
}

func withBicep(t *testing.T, stderr io.Writer) app.Services {
	t.Helper()
	arm, err := os.ReadFile(filepath.Join("..", "..", "internal", "bicep", "testdata", "golden", "main.arm.json"))
	if err != nil {
		t.Fatal(err)
	}
	return app.NewServices(stderr, app.Options{
		AzdVersion:  func(context.Context) string { return "1.34.2" },
		BicepRunner: fakeBicep{arm: arm},
		Bicep: bicep.DiscoverOptions{
			LookPath: func(string) (string, error) { return "bicep", nil },
			Runner:   fakeBicep{arm: arm},
		},
	})
}

func TestSamplesEndToEnd(t *testing.T) {
	var errb bytes.Buffer
	t.Run("good exits 0 with explicit skips", func(t *testing.T) {
		code, r, _ := runJSON(t, noBicep(&errb), "doctor", "--local", "--dir", sample("good"))
		if code != 0 || len(r.Findings) != 0 {
			t.Fatalf("exit=%d findings=%v", code, ids(r))
		}
		if len(r.Skipped) == 0 {
			t.Fatal("skipped checks must be listed, never converted to pass")
		}
	})
	t.Run("good under strict reports skips as exit 3", func(t *testing.T) {
		code, _, _ := runJSON(t, noBicep(&errb), "doctor", "--local", "--strict", "--dir", sample("good"))
		if code != 3 {
			t.Fatalf("exit=%d, want 3", code)
		}
	})
	t.Run("bad exits 1 with expected rules", func(t *testing.T) {
		code, r, _ := runJSON(t, noBicep(&errb), "doctor", "--local", "--dir", sample("bad"))
		if code != 1 {
			t.Fatalf("exit=%d", code)
		}
		got := ids(r)
		for _, want := range []string{"FND-CFG-001", "FND-CFG-004"} {
			if !slices.Contains(got, want) {
				t.Errorf("missing %s in %v", want, got)
			}
		}
	})
	t.Run("azure-yaml-only skips Bicep rules explicitly", func(t *testing.T) {
		code, r, _ := runJSON(t, noBicep(&errb), "doctor", "--local", "--dir", sample("azure-yaml-only"))
		stderr := errb.String()
		if code != 0 {
			t.Fatalf("exit=%d findings=%v", code, ids(r))
		}
		if !strings.Contains(stderr, "Bicep-dependent rules are skipped") {
			t.Errorf("stderr=%q", stderr)
		}
		found := false
		for _, s := range r.Skipped {
			if strings.HasPrefix(s.RuleID, "FND-NET-") && s.Reason == "input-unavailable" {
				found = true
			}
		}
		if !found {
			t.Error("NET rules should be skipped input-unavailable")
		}
	})
	t.Run("bicep-backed with fake compiler runs ARM rules", func(t *testing.T) {
		var bb bytes.Buffer
		code, r, _ := runJSON(t, withBicep(t, &bb), "doctor", "--local", "--dir", sample("bicep-backed"))
		stderr := bb.String()
		if code != 0 && code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, stderr)
		}
		if strings.Contains(stderr, "unavailable") {
			t.Fatalf("ARM stage should have run: %s", stderr)
		}
		for _, s := range r.Skipped {
			if s.RuleID == "FND-SEC-004" && s.Reason == "input-unavailable" {
				t.Error("SEC-004 should have evaluated against the ARM model")
			}
		}
	})
}

func TestInternalErrorExitsFour(t *testing.T) {
	var errb bytes.Buffer
	svc := noBicep(&errb)
	svc.Reporter = failingReporter{}
	code, _, _ := runJSON(t, svc, "doctor", "--local", "--dir", sample("good"))
	if code != 4 {
		t.Fatalf("exit=%d, want 4", code)
	}
}

type failingReporter struct{}

func (failingReporter) Render(io.Writer, string, app.Report) error {
	return errors.New("boom")
}

func TestExpiredSuppressionFailsRun(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(sample("good"), "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	sup := "version: 1\nsuppressions:\n  - rule: FND-CFG-004\n    reason: temporary\n    owner: team\n    expires: \"" +
		time.Now().AddDate(-1, 0, 0).Format("2006-01-02") + "\"\n"
	sp := filepath.Join(dir, "sup.yaml")
	if err := os.WriteFile(sp, []byte(sup), 0o600); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	code, _, _ := runJSON(t, noBicep(&errb), "doctor", "--local", "--dir", dir, "--suppressions", sp)
	if code == 0 {
		t.Fatal("expired suppression must not exit 0")
	}
}
