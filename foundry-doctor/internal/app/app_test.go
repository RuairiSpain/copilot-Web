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
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const minimalYAML = "name: demo\nservices:\n  web:\n    host: containerapp\n"

type fakeProject struct {
	src Source
	err error
}

func (f fakeProject) Load(context.Context, string, Inputs) (Source, error) { return f.src, f.err }

type fakeConfig struct {
	byEnv map[string]Settings
	err   error
	last  ConfigRequest
}

func (f *fakeConfig) Resolve(_ context.Context, r ConfigRequest) (Settings, error) {
	f.last = r
	if f.err != nil {
		return Settings{}, f.err
	}
	if s, ok := f.byEnv[r.Environment]; ok {
		return s, nil
	}
	return Settings{Profile: "dev", Environment: r.Environment}, nil
}

type fakeEngine struct {
	out RunOutput
	err error
	in  RunInput
}

func (f *fakeEngine) Run(_ context.Context, in RunInput) (RunOutput, error) {
	f.in = in
	return f.out, f.err
}

type markFilter struct {
	called bool
	err    error
}

func (m *markFilter) Apply(_ context.Context, _ string, in []sdk.Finding, _ time.Time) ([]sdk.Finding, error) {
	m.called = true
	for i := range in {
		in[i].Baselined = true
	}
	return in, m.err
}

type textReporter struct{ err error }

func (r textReporter) Render(w io.Writer, format string, rep Report) error {
	if r.err != nil {
		return r.err
	}
	_, err := fmt.Fprintf(w, "%s findings=%d skipped=%d\n", format, len(rep.Findings), len(rep.Skipped))
	return err
}

func newSvc(eng *fakeEngine) (Services, *fakeConfig) {
	cfg := &fakeConfig{}
	return Services{
		Project:  fakeProject{src: Source{AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml"}},
		Config:   cfg,
		Engine:   eng,
		Reporter: textReporter{},
		Now:      func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}, cfg
}

func finding(sev sdk.Severity) sdk.Finding {
	return sdk.Finding{RuleID: "FND-CFG-001", Severity: sev, Fingerprint: "abc"}
}

func TestExitCode(t *testing.T) {
	sup := finding(sdk.SeverityError)
	sup.Suppressed = &sdk.Suppression{Reason: "r", Owner: "o"}
	base := finding(sdk.SeverityError)
	base.Baselined = true
	tests := []struct {
		name string
		o    Outcome
		want int
	}{
		{"clean", Outcome{}, ExitOK},
		{"warning below default threshold", Outcome{Findings: []sdk.Finding{finding(sdk.SeverityWarning)}}, ExitOK},
		{"error meets default threshold", Outcome{Findings: []sdk.Finding{finding(sdk.SeverityError)}}, ExitFindings},
		{"warning meets fail-on warning", Outcome{Findings: []sdk.Finding{finding(sdk.SeverityWarning)}, FailOn: sdk.SeverityWarning}, ExitFindings},
		{"suppressed ignored", Outcome{Findings: []sdk.Finding{sup}}, ExitOK},
		{"baselined ignored", Outcome{Findings: []sdk.Finding{base}}, ExitOK},
		{"skip without strict is not a failure", Outcome{Skips: []sdk.Skip{{RuleID: "X", Reason: sdk.SkipNotImplemented}}}, ExitOK},
		{"skip with strict is 3", Outcome{Strict: true, Skips: []sdk.Skip{{RuleID: "X", Reason: "r"}}}, ExitStrictSkip},
		{"strict skip beats findings", Outcome{Strict: true, Findings: []sdk.Finding{finding(sdk.SeverityError)}, Skips: []sdk.Skip{{RuleID: "X"}}}, ExitStrictSkip},
		{"optional skip keeps findings", Outcome{Findings: []sdk.Finding{finding(sdk.SeverityError)}, Skips: []sdk.Skip{{RuleID: "X"}}}, ExitFindings},
		{"required skip is 2", Outcome{Skips: []sdk.Skip{{RuleID: "X", Required: true}}}, ExitUnavailable},
		{"exit 2 beats findings and strict", Outcome{Strict: true, Findings: []sdk.Finding{finding(sdk.SeverityError)}, Skips: []sdk.Skip{{RuleID: "X", Required: true}}}, ExitUnavailable},
		{"invalid fail-on defaults to error", Outcome{FailOn: "bogus", Findings: []sdk.Finding{finding(sdk.SeverityWarning)}}, ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.o); got != tt.want {
				t.Fatalf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, ExitOK},
		{ErrUnavailable, ExitUnavailable},
		{fmt.Errorf("wrap: %w", ErrUnavailable), ExitUnavailable},
		{Usagef("bad"), ExitUnavailable},
		{fmt.Errorf("wrap: %w", Usagef("bad")), ExitUnavailable},
		{errors.New("boom"), ExitInternal},
	}
	for _, tt := range tests {
		if got := ExitCodeForError(tt.err); got != tt.want {
			t.Errorf("ExitCodeForError(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestDoctor(t *testing.T) {
	tests := []struct {
		name     string
		eng      fakeEngine
		req      DoctorRequest
		mutate   func(*Services)
		wantExit int
		wantErr  bool
		wantOut  string
	}{
		{name: "clean run", wantExit: ExitOK, wantOut: "console findings=0 skipped=0"},
		{
			name:     "error finding fails",
			eng:      fakeEngine{out: RunOutput{Findings: []sdk.Finding{finding(sdk.SeverityError)}}},
			wantExit: ExitFindings, wantOut: "findings=1",
		},
		{
			name:     "min-severity hides lower findings from the report",
			eng:      fakeEngine{out: RunOutput{Findings: []sdk.Finding{finding(sdk.SeverityInfo)}}},
			req:      DoctorRequest{MinSeverity: sdk.SeverityWarning},
			wantExit: ExitOK, wantOut: "findings=0",
		},
		{
			name:     "skip reported not passed and strict gives 3",
			eng:      fakeEngine{out: RunOutput{Skipped: []sdk.Skip{{RuleID: "FND-X-1", Reason: sdk.SkipInputUnavailable}}}},
			req:      DoctorRequest{Strict: true},
			wantExit: ExitStrictSkip, wantOut: "skipped=1",
		},
		{
			name:     "required skip is exit 2 even with findings",
			eng:      fakeEngine{out: RunOutput{Findings: []sdk.Finding{finding(sdk.SeverityError)}, Skipped: []sdk.Skip{{RuleID: "X", Required: true}}}},
			wantExit: ExitUnavailable, wantOut: "skipped=1",
		},
		{name: "invalid format", req: DoctorRequest{Format: "xml"}, wantExit: ExitUnavailable, wantErr: true},
		{name: "invalid profile", req: DoctorRequest{Profile: "staging"}, wantExit: ExitUnavailable, wantErr: true},
		{name: "fail-on below min-severity", req: DoctorRequest{MinSeverity: sdk.SeverityError, FailOn: sdk.SeverityWarning}, wantExit: ExitUnavailable, wantErr: true},
		{
			name:     "missing azure.yaml is exit 2",
			mutate:   func(s *Services) { s.Project = fakeProject{err: fmt.Errorf("find: %w", ErrUnavailable)} },
			wantExit: ExitUnavailable, wantErr: true,
		},
		{
			name: "malformed azure.yaml is exit 2",
			mutate: func(s *Services) {
				s.Project = fakeProject{src: Source{AzureYAML: []byte("a: [\n"), AzureYAMLAt: "azure.yaml"}}
			},
			wantExit: ExitUnavailable, wantErr: true,
		},
		{
			name:     "empty azure.yaml is exit 2",
			mutate:   func(s *Services) { s.Project = fakeProject{src: Source{AzureYAMLAt: "azure.yaml"}} },
			wantExit: ExitUnavailable, wantErr: true,
		},
		{
			name:     "config usage error is exit 2",
			mutate:   func(s *Services) { s.Config = &fakeConfig{err: Usagef("unknown key")} },
			wantExit: ExitUnavailable, wantErr: true,
		},
		{
			name:     "engine failure is exit 4",
			eng:      fakeEngine{err: errors.New("boom")},
			wantExit: ExitInternal, wantErr: true,
		},
		{
			name:     "reporter failure is exit 4",
			mutate:   func(s *Services) { s.Reporter = textReporter{err: errors.New("render")} },
			wantExit: ExitInternal, wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng := tt.eng
			svc, _ := newSvc(&eng)
			if tt.mutate != nil {
				tt.mutate(&svc)
			}
			var out bytes.Buffer
			code, err := Doctor(context.Background(), svc, tt.req, &out)
			if code != tt.wantExit {
				t.Fatalf("exit = %d, want %d (err=%v)", code, tt.wantExit, err)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("output %q missing %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestDoctorPassesInputsToEngine(t *testing.T) {
	eng := &fakeEngine{}
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {
		Profile: "prod", Environment: "dev", Rules: []string{"FND-CFG-*"},
		Policy: map[string]any{"network.publicAccess": "forbidden"},
	}}
	if _, err := Doctor(context.Background(), svc, DoctorRequest{Local: true, Profile: "prod", Rules: []string{"FND-CFG-*"}}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if cfg.last.Profile != "prod" {
		t.Errorf("config request = %+v", cfg.last)
	}
	if !eng.in.Local || eng.in.Profile != "prod" || len(eng.in.Selectors) != 1 {
		t.Errorf("engine input = %+v", eng.in)
	}
	if v, ok := eng.in.Policy.Get("network.publicAccess"); !ok || v != "forbidden" {
		t.Errorf("policy get = %v %v", v, ok)
	}
	if _, ok := eng.in.Policy.Get("missing.key"); ok {
		t.Error("missing key reported present")
	}
	if eng.in.ARM != nil {
		t.Error("ARM must be nil when no loader is wired")
	}
	if eng.in.AzureYAML == nil || eng.in.AzureYAML.Path() != "azure.yaml" {
		t.Error("azure.yaml view not passed")
	}
}

func TestDoctorBaselineAndSuppress(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{finding(sdk.SeverityError)}}}
	svc, cfg := newSvc(eng)
	cfg.byEnv = map[string]Settings{"": {Profile: "dev", Baseline: "b.json"}}
	bf := &markFilter{}
	sf := &markFilter{}
	svc.Baseline, svc.Suppress = bf, sf
	code, err := Doctor(context.Background(), svc, DoctorRequest{}, io.Discard)
	if err != nil || code != ExitOK {
		t.Fatalf("baselined finding must not fail: code=%d err=%v", code, err)
	}
	if !bf.called {
		t.Error("baseline not applied")
	}
	if sf.called {
		t.Error("suppressions applied without a path")
	}

	bf.err = errors.New("corrupt")
	code, err = Doctor(context.Background(), svc, DoctorRequest{}, io.Discard)
	if err == nil || code != ExitInternal {
		t.Fatalf("baseline error: code=%d err=%v", code, err)
	}
}

func TestDoctorOutFile(t *testing.T) {
	svc, _ := newSvc(&fakeEngine{})
	var gotPath string
	var gotData []byte
	svc.WriteOutput = func(p string, b []byte) error { gotPath, gotData = p, b; return nil }
	var stdout bytes.Buffer
	if _, err := Doctor(context.Background(), svc, DoctorRequest{Out: "r.txt"}, &stdout); err != nil {
		t.Fatal(err)
	}
	if gotPath != "r.txt" || !strings.Contains(string(gotData), "findings=0") || stdout.Len() != 0 {
		t.Errorf("out=%q data=%q stdout=%q", gotPath, gotData, stdout.String())
	}
	svc.WriteOutput = func(string, []byte) error { return errors.New("disk full") }
	if code, err := Doctor(context.Background(), svc, DoctorRequest{Out: "r.txt"}, io.Discard); err == nil || code != ExitInternal {
		t.Errorf("write failure: code=%d err=%v", code, err)
	}
}

func TestDoctorRedactsFindings(t *testing.T) {
	secret := "Bearer " + strings.Repeat("abcdefgh", 6)
	f := finding(sdk.SeverityError)
	f.Evidence = "header " + secret
	svc, _ := newSvc(&fakeEngine{out: RunOutput{Findings: []sdk.Finding{f}}})
	var captured []sdk.Finding
	svc.Reporter = reporterFunc(func(_ io.Writer, _ string, r Report) error { captured = r.Findings; return nil })
	if _, err := Doctor(context.Background(), svc, DoctorRequest{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(captured) != 1 || strings.Contains(captured[0].Evidence, "abcdefgh") {
		t.Errorf("evidence not redacted: %+v", captured)
	}
}

func TestAnnotateGitHubAndSARIF(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{{
		RuleID:         "FND-CFG-001",
		Severity:       sdk.SeverityError,
		Location:       sdk.Location{File: "azure.yaml", Line: 1, Column: 1},
		Evidence:       "bad\nline redacted-marker",
		Recommendation: "fix %",
		Fingerprint:    "fp-1",
	}}}}
	svc, _ := newSvc(eng)
	var out bytes.Buffer
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "github"}, &out); err != nil || code != ExitFindings {
		t.Fatalf("github annotate code=%d err=%v", code, err)
	}
	if got := out.String(); !strings.Contains(got, "::error file=azure.yaml,line=1,col=1,title=FND-CFG-001::") || strings.Contains(got, "bad\nline") {
		t.Fatalf("github output=%q", got)
	}
	out.Reset()
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif"}, &out); err != nil || code != ExitFindings {
		t.Fatalf("sarif annotate code=%d err=%v", code, err)
	}
	if got := out.String(); !strings.Contains(got, "\"ruleId\": \"FND-CFG-001\"") {
		t.Fatalf("sarif output=%q", got)
	}
}

func TestAnnotateRejectsUnsafeNonReviewOut(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{{
		RuleID:      "FND-CFG-001",
		Severity:    sdk.SeverityError,
		Location:    sdk.Location{File: "azure.yaml", Line: 1, Column: 1},
		Evidence:    "bad config",
		Fingerprint: "fp-1",
	}}}}
	svc, _ := newSvc(eng)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte(minimalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.Project = fakeProject{src: Source{Dir: dir, AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml"}}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "github", Out: "..\\bad.txt", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("unsafe github out code=%d err=%v", code, err)
	}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "sarif", Out: "..\\bad.sarif", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("unsafe sarif out code=%d err=%v", code, err)
	}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "github", Out: "azure.yaml", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("overlapping github out code=%d err=%v", code, err)
	}
}

func TestAnnotateReviewWritesCopy(t *testing.T) {
	eng := &fakeEngine{out: RunOutput{Findings: []sdk.Finding{{
		RuleID:      "FND-CFG-001",
		Severity:    sdk.SeverityError,
		Location:    sdk.Location{File: "azure.yaml", Line: 1, Column: 1},
		Evidence:    "bad config",
		Fingerprint: "fp-1",
	}}}}
	svc, _ := newSvc(eng)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), []byte(minimalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.Project = fakeProject{src: Source{Dir: dir, AzureYAML: []byte(minimalYAML), AzureYAMLAt: "azure.yaml"}}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "review\\demo.review", Dir: dir}, io.Discard); err != nil || code != ExitFindings {
		t.Fatalf("review annotate code=%d err=%v", code, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "review", "demo.review", "azure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Foundry Doctor: FND-CFG-001 error") {
		t.Fatalf("annotated yaml missing comment:\n%s", data)
	}
}

func TestResolveBicepEntry(t *testing.T) {
	doc, err := azureyaml.Parse([]byte("name: demo\ninfra:\n  path: deploy\n  module: main\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := resolveBicepEntry(Source{InfraPath: "infra"}, doc); !ok || got != "deploy/main.bicep" {
		t.Fatalf("resolveBicepEntry() = %q, %v", got, ok)
	}

	layered, err := azureyaml.Parse([]byte("name: demo\ninfra:\n  layers:\n    - path: shared\n"), "azure.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !hasInfraLayers(layered) {
		t.Fatal("expected layered infra to be detected")
	}
}

func TestAnnotateReviewFailsOnInfraLayersWhenBicepValidationIsNeeded(t *testing.T) {
	eng := &fakeEngine{}
	svc, _ := newSvc(eng)
	svc.ARM = armLoader{}
	dir := t.TempDir()
	layeredYAML := []byte("name: demo\ninfra:\n  layers:\n    - path: shared\n")
	if err := os.WriteFile(filepath.Join(dir, "azure.yaml"), layeredYAML, 0o644); err != nil {
		t.Fatal(err)
	}
	svc.Project = fakeProject{src: Source{Dir: dir, AzureYAML: layeredYAML, AzureYAMLAt: "azure.yaml", InfraPath: "infra"}}
	if code, err := Annotate(context.Background(), svc, AnnotateRequest{Format: "review", Out: "review\\layered.review", Dir: dir}, io.Discard); err == nil || code != ExitUnavailable {
		t.Fatalf("layered review code=%d err=%v", code, err)
	}
}

type reporterFunc func(io.Writer, string, Report) error

func (f reporterFunc) Render(w io.Writer, format string, r Report) error { return f(w, format, r) }

type fakeExplainer struct{ err error }

func (f fakeExplainer) Explain(_ context.Context, id, format string, w io.Writer) error {
	if f.err != nil {
		return f.err
	}
	_, err := fmt.Fprintf(w, "%s/%s\n", id, format)
	return err
}

func TestExplain(t *testing.T) {
	tests := []struct {
		name, id, format string
		err              error
		wantExit         int
		wantOut          string
	}{
		{"ok", "FND-CFG-001", "", nil, ExitOK, "FND-CFG-001/console"},
		{"markdown", "FND-CFG-001", "markdown", nil, ExitOK, "/markdown"},
		{"bad format", "FND-CFG-001", "sarif", nil, ExitUnavailable, ""},
		{"missing id", "", "", nil, ExitUnavailable, ""},
		{"unknown rule", "FND-NOPE-9", "", Usagef("unknown rule"), ExitUnavailable, ""},
		{"internal", "FND-CFG-001", "", errors.New("boom"), ExitInternal, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := Services{Explainer: fakeExplainer{err: tt.err}}
			var out bytes.Buffer
			code, _ := Explain(context.Background(), svc, tt.id, tt.format, &out)
			if code != tt.wantExit || !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("code=%d out=%q", code, out.String())
			}
		})
	}
}

func TestCompareSettings(t *testing.T) {
	left := map[string]any{"a": 1, "b": "x", "c": []any{"p"}}
	right := map[string]any{"a": 1, "b": "y", "d": true}
	got := CompareSettings(left, right)
	want := []Difference{
		{Key: "b", Left: "x", Right: "y", Presence: "both"},
		{Key: "c", Left: []any{"p"}, Presence: "left-only"},
		{Key: "d", Right: true, Presence: "right-only"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if d := CompareSettings(nil, nil); len(d) != 0 || d == nil {
		t.Errorf("empty compare = %#v", d)
	}
}

func TestCompare(t *testing.T) {
	svc, cfg := newSvc(&fakeEngine{})
	cfg.byEnv = map[string]Settings{
		"dev":  {Profile: "dev", Policy: map[string]any{"network.publicAccess": "allowed"}},
		"prod": {Profile: "prod", Policy: map[string]any{"network.publicAccess": "forbidden"}},
	}
	tests := []struct {
		name     string
		req      CompareRequest
		wantExit int
		want     []string
	}{
		{"console", CompareRequest{Left: "dev", Right: "prod"}, ExitOK, []string{`network.publicAccess: "allowed" -> "forbidden"`, "profile dev"}},
		{"json", CompareRequest{Left: "dev", Right: "prod", Format: "json"}, ExitOK, []string{`"presence": "both"`}},
		{"identical", CompareRequest{Left: "dev", Right: "dev"}, ExitOK, []string{"No policy differences."}},
		{"bad format", CompareRequest{Left: "dev", Right: "prod", Format: "sarif"}, ExitUnavailable, nil},
		{"missing env", CompareRequest{Left: "dev"}, ExitUnavailable, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			code, _ := Compare(context.Background(), svc, tt.req, &out)
			if code != tt.wantExit {
				t.Fatalf("code = %d", code)
			}
			for _, w := range tt.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output missing %q:\n%s", w, out.String())
				}
			}
		})
	}
	svc.Config = &fakeConfig{err: errors.New("boom")}
	if code, _ := Compare(context.Background(), svc, CompareRequest{Left: "a", Right: "b"}, io.Discard); code != ExitInternal {
		t.Errorf("config failure code = %d", code)
	}
}
