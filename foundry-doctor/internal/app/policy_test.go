package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type errARM struct{ err error }

func (e errARM) Load(context.Context, Source) (sdk.ARMModel, error) { return nil, e.err }

func TestDoctorBicepRequiredMapsFailureToExit2(t *testing.T) {
	failed := fmt.Errorf("%w: %w: bicep CLI not found", ErrARMUnavailable, ErrBicepFailed)
	noInfra := fmt.Errorf("%w: %w: infra/main.bicep or infra/main.json not found", ErrARMUnavailable, ErrNoInfrastructure)
	plain := fmt.Errorf("%w: infra.layers is not read", ErrARMUnavailable)
	tests := []struct {
		name          string
		required      bool
		err           error
		wantExit      int
		wantWarn      bool
		wantSynthetic bool
	}{
		{"required, cli missing or compile failure", true, failed, ExitUnavailable, false, false},
		{"optional, cli missing or compile failure", false, failed, ExitOK, true, false},
		{"required, no infrastructure is synthetic", true, noInfra, ExitOK, true, true},
		{"optional, no infrastructure is synthetic", false, noInfra, ExitOK, true, true},
		{"required, other unavailable reason stays a warning", true, plain, ExitOK, true, false},
		{"required, loader succeeded", true, nil, ExitOK, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := &fakeEngine{}
			svc, cfg := newSvc(eng)
			cfg.byEnv = map[string]Settings{"": {Profile: "foundry-prod", BicepRequired: tt.required}}
			svc.ARM = errARM{err: tt.err}
			var stderr bytes.Buffer
			svc.Stderr = &stderr
			code, err := Doctor(context.Background(), svc, DoctorRequest{Local: true}, io.Discard)
			if code != tt.wantExit {
				t.Fatalf("exit = %d (err %v), want %d", code, err, tt.wantExit)
			}
			if tt.wantExit == ExitUnavailable {
				if !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrBicepFailed) {
					t.Fatalf("err = %v, want ErrUnavailable and ErrBicepFailed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(stderr.String(), "Bicep-dependent rules are skipped"); got != tt.wantWarn {
				t.Fatalf("warning printed = %v, want %v (stderr %q)", got, tt.wantWarn, stderr.String())
			}
			if eng.in.Synthetic != tt.wantSynthetic {
				t.Fatalf("RunInput.Synthetic = %v, want %v", eng.in.Synthetic, tt.wantSynthetic)
			}
		})
	}
}

func TestArmLoaderClassifiesFailures(t *testing.T) {
	missingCLI := bicep.DiscoverOptions{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	write := func(t *testing.T, dir, rel, data string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("bicep entry with no CLI is a bicep failure", func(t *testing.T) {
		t.Setenv("BICEP_PATH", "")
		dir := t.TempDir()
		write(t, dir, "azure.yaml", "name: demo\n")
		write(t, dir, "infra/main.bicep", "targetScope = 'resourceGroup'\n")
		_, err := armLoader{disc: missingCLI}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
		if !errors.Is(err, ErrARMUnavailable) || !errors.Is(err, ErrBicepFailed) || errors.Is(err, ErrNoInfrastructure) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unreadable ARM JSON is a bicep failure", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "azure.yaml", "name: demo\n")
		write(t, dir, "infra/main.json", "not json")
		_, err := armLoader{}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
		if !errors.Is(err, ErrBicepFailed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no entry is synthetic, not a failure", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "azure.yaml", "name: demo\n")
		_, err := armLoader{disc: missingCLI}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte("name: demo\n"), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
		if !errors.Is(err, ErrARMUnavailable) || !errors.Is(err, ErrNoInfrastructure) || errors.Is(err, ErrBicepFailed) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("infra.layers is neither synthetic nor a failure", func(t *testing.T) {
		dir := t.TempDir()
		yaml := "name: demo\ninfra:\n  layers:\n    - path: shared\n"
		write(t, dir, "azure.yaml", yaml)
		_, err := armLoader{disc: missingCLI}.Load(context.Background(), Source{Dir: dir, AzureYAML: []byte(yaml), AzureYAMLAt: "azure.yaml", InfraPath: "infra"})
		if !errors.Is(err, ErrARMUnavailable) || errors.Is(err, ErrNoInfrastructure) || errors.Is(err, ErrBicepFailed) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestExitCodeStrictCountsOnlyHiddenChecks(t *testing.T) {
	byDesign := []sdk.Skip{
		{RuleID: "A", Reason: sdk.SkipNotImplemented},
		{RuleID: "B", Reason: sdk.SkipMissingPolicyKey("tags.required")},
		{RuleID: "C", Reason: sdk.SkipSyntheticInfrastructure},
		{RuleID: "D", Reason: sdk.SkipNotApplicable},
	}
	tests := []struct {
		name  string
		skips []sdk.Skip
		want  int
	}{
		{"clean project, by-design skips only", byDesign, ExitOK},
		{"no skips", nil, ExitOK},
		{"missing input", append(append([]sdk.Skip{}, byDesign...), sdk.Skip{RuleID: "E", Reason: sdk.SkipInputUnavailable}), ExitStrictSkip},
		{"azd version unknown", []sdk.Skip{{RuleID: "E", Reason: sdk.SkipAzdVersionUnknown}}, ExitStrictSkip},
		{"unsupported version", []sdk.Skip{{RuleID: "E", Reason: sdk.SkipUnsupportedVersion}}, ExitStrictSkip},
		{"required skip still 2", []sdk.Skip{{RuleID: "E", Reason: sdk.SkipNotImplemented, Required: true}}, ExitUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(Outcome{Strict: true, Skips: tt.skips}); got != tt.want {
				t.Fatalf("strict ExitCode = %d, want %d", got, tt.want)
			}
			if got := ExitCode(Outcome{Skips: tt.skips}); got != ExitOK && got != ExitUnavailable {
				t.Fatalf("non-strict ExitCode = %d", got)
			}
		})
	}
}

func TestAzdVersionOverride(t *testing.T) {
	tests := []struct {
		name, flag, env, want string
		wantErr               bool
	}{
		{"unset", "", "", "", false},
		{"flag", "1.35.0", "", "1.35.0", false},
		{"v prefix", "v1.35.0", "", "1.35.0", false},
		{"env", "", "1.34.2", "1.34.2", false},
		{"flag beats env", "1.35.0", "1.34.2", "1.35.0", false},
		{"prerelease", "1.36.0-beta.1", "", "1.36.0-beta.1", false},
		{"garbage flag", "latest", "", "", true},
		{"garbage env", "", "1.x", "", true},
		{"trailing text", "1.35.0 extra", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := azdVersionOverride(tt.flag, tt.env)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %q, %v; want %q, err=%v", got, err, tt.want, tt.wantErr)
			}
			if err != nil && ExitCodeForError(err) != ExitUnavailable {
				t.Fatalf("bad version must exit 2, got %d", ExitCodeForError(err))
			}
		})
	}
}

type viewStub struct{ values map[string]string }

func (viewStub) Path() string           { return "azure.yaml" }
func (viewStub) ServiceNames() []string { return nil }
func (v viewStub) Lookup(path ...string) (string, sdk.Location, bool) {
	s, ok := v.values[strings.Join(path, ".")]
	return s, sdk.Location{}, ok
}

func TestVersionsNotDetectedNote(t *testing.T) {
	plain := VersionsNotDetectedNote(nil)
	if !strings.HasPrefix(plain, "versions not detected: azd") || strings.Contains(plain, "azure.yaml requires") {
		t.Fatalf("note = %q", plain)
	}
	declared := VersionsNotDetectedNote(viewStub{values: map[string]string{"requiredVersions.azd": ">=1.34.2"}})
	if !strings.Contains(declared, "azure.yaml requires azd >=1.34.2") {
		t.Fatalf("note = %q", declared)
	}
	if !strings.Contains(plain, EnvAzdVersion) {
		t.Fatalf("note must name the override: %q", plain)
	}
}

func TestSkipSummaryNotes(t *testing.T) {
	if got := SkipSummaryNotes([]sdk.Skip{{RuleID: "A", Reason: sdk.SkipNotImplemented}}); got != nil {
		t.Fatalf("notes = %v", got)
	}
	got := SkipSummaryNotes([]sdk.Skip{
		{RuleID: "FND-NET-002", Reason: sdk.SkipSyntheticInfrastructure},
		{RuleID: "FND-NET-001", Reason: sdk.SkipSyntheticInfrastructure},
		{RuleID: "FND-CFG-001", Reason: sdk.SkipNotImplemented},
	})
	want := "2 checks need compiled infrastructure and were skipped (synthetic-infrastructure): FND-NET-001, FND-NET-002"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("notes = %q, want %q", got, want)
	}
}

func TestDoctorReportsAzdNoteAndDiagnostics(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{
		Notes:       []string{"versions not detected: azd"},
		Diagnostics: []string{"FND-CFG-005: findings share a fingerprint"},
	}}
	svc, _ := newSvc(eng)
	var stderr bytes.Buffer
	svc.Stderr = &stderr
	var got Report
	svc.Reporter = reporterFunc(func(_ io.Writer, _ string, r Report) error { got = r; return nil })
	if _, err := Doctor(context.Background(), svc, DoctorRequest{Local: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != 1 || got.Notes[0] != "versions not detected: azd" {
		t.Fatalf("report notes = %v", got.Notes)
	}
	if !strings.Contains(stderr.String(), "versions not detected: azd") || !strings.Contains(stderr.String(), "share a fingerprint") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReportAdapterPrintsNotes(t *testing.T) {
	r := Report{Profile: "foundry-prod", Notes: []string{"versions not detected: azd"}}
	for _, format := range []string{"console", "markdown"} {
		var buf bytes.Buffer
		if err := (reportAdapter{}).Render(&buf, format, r); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "versions not detected: azd") {
			t.Fatalf("%s output missing note:\n%s", format, buf.String())
		}
	}
	var buf bytes.Buffer
	if err := (reportAdapter{}).Render(&buf, "json", r); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "versions not detected") || !strings.HasPrefix(buf.String(), "{") {
		t.Fatalf("json output must stay pure JSON:\n%s", buf.String())
	}
}

func TestNormalizeOutPath(t *testing.T) {
	for in, want := range map[string]string{"": "", "a/b": "a/b", "a\\b\\c": "a/b/c", "..\\x": "../x"} {
		if got := normalizeOutPath(in); got != want {
			t.Errorf("normalizeOutPath(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"C:\\temp\\x", "c:/x", "..\\x", "/etc/x", "a/../../x", ""} {
		if _, err := safeRepoOutputPath(bad); err == nil {
			t.Errorf("safeRepoOutputPath(%q) accepted", bad)
		}
	}
	if got, err := safeRepoOutputPath("review\\demo.review"); err != nil || got != "review/demo.review" {
		t.Errorf("safeRepoOutputPath = %q, %v", got, err)
	}
}

func TestConfigAdapterBicepRequiredAndAzdVersion(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name         string
		profile      string
		config       string
		env          map[string]string
		flagAzd      string
		wantRequired bool
		wantAzd      string
		wantErr      bool
	}{
		{"prod requires bicep", "prod", "", nil, "", true, "", false},
		{"test requires bicep", "test", "", nil, "", true, "", false},
		{"dev does not", "dev", "", nil, "", false, "", false},
		{"config can relax prod", "prod", "version: 1\nvalidation:\n  bicep: optional\n", nil, "", false, "", false},
		{"config can require in dev", "dev", "version: 1\nvalidation:\n  bicep: required\n", nil, "", true, "", false},
		{"azd version from env", "dev", "", map[string]string{EnvAzdVersion: "1.35.0"}, "", false, "1.35.0", false},
		{"azd version flag wins", "dev", "", map[string]string{EnvAzdVersion: "1.34.2"}, "1.35.0", false, "1.35.0", false},
		{"bad azd version", "dev", "", nil, "soon", false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := t.TempDir()
			if err := os.WriteFile(filepath.Join(d, "azure.yaml"), []byte(minimalYAML), 0o644); err != nil {
				t.Fatal(err)
			}
			if tt.config != "" {
				if err := os.MkdirAll(filepath.Join(d, ".foundry-doctor"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(d, RepoConfigPath), []byte(tt.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			set, err := configAdapter{getenv: env(tt.env)}.Resolve(context.Background(), ConfigRequest{Dir: d, Profile: tt.profile, AzdVersion: tt.flagAzd})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if set.BicepRequired != tt.wantRequired || set.AzdVersion != tt.wantAzd {
				t.Fatalf("BicepRequired=%v AzdVersion=%q, want %v %q", set.BicepRequired, set.AzdVersion, tt.wantRequired, tt.wantAzd)
			}
		})
	}
}
