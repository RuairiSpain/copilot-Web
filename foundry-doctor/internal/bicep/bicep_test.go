package bicep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// fakeRunner returns canned results and records the specs it was given.
type fakeRunner struct {
	results []RunResult
	err     error
	specs   []RunSpec
}

func (f *fakeRunner) Run(_ context.Context, spec RunSpec) (RunResult, error) {
	f.specs = append(f.specs, spec)
	if f.err != nil {
		return RunResult{ExitCode: -1}, f.err
	}
	i := min(len(f.specs)-1, len(f.results)-1)
	return f.results[i], nil
}

func available(path string) Discovery {
	return Discovery{Path: path, Version: Version{Minor: 47, Patch: 16},
		Status: sdk.ToolStatus{Name: ToolName, State: sdk.ToolAvailable, Version: "0.47.16"}}
}

func newProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseVersion(t *testing.T) {
	v, err := ParseVersion("Bicep CLI version 0.47.16 (3f73e1a234)\n")
	if err != nil || v.String() != "0.47.16" || v.Hash != "3f73e1a234" {
		t.Fatalf("%v %+v", err, v)
	}
	if v, err = ParseVersion("Bicep CLI version 0.24.24"); err != nil || v.Hash != "" || v.Compare(MinVersion()) != 0 {
		t.Fatalf("%v %+v", err, v)
	}
	for _, bad := range []string{"", "bicep 1.2.3", "Bicep CLI version x.y.z"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
	cases := []struct {
		a, b Version
		want int
	}{
		{Version{Major: 0, Minor: 24, Patch: 23}, MinVersion(), -1},
		{Version{Major: 0, Minor: 25}, MinVersion(), 1},
		{Version{Major: 1}, MinVersion(), 1},
		{Version{Major: 0, Minor: 24, Patch: 25}, MinVersion(), 1},
		{Version{Minor: 24, Patch: 24, Hash: "x"}, MinVersion(), 0},
	}
	for _, c := range cases {
		if got := c.a.Compare(c.b); got != c.want {
			t.Errorf("%v vs %v = %d", c.a, c.b, got)
		}
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bicep")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ver := func(out string, code int, err error) *fakeRunner {
		return &fakeRunner{results: []RunResult{{Stdout: []byte(out), ExitCode: code}}, err: err}
	}
	found := func(string) (string, error) { return exe, nil }
	tests := []struct {
		name  string
		opts  DiscoverOptions
		state sdk.ToolState
		want  string // substring of Detail or Version
	}{
		{"explicit ok", DiscoverOptions{ExplicitPath: exe, Runner: ver("Bicep CLI version 0.47.16 (abc)", 0, nil)}, sdk.ToolAvailable, "0.47.16"},
		{"path ok", DiscoverOptions{LookPath: found, Runner: ver("Bicep CLI version 0.30.1 (abc)", 0, nil)}, sdk.ToolAvailable, "0.30.1"},
		{"old", DiscoverOptions{LookPath: found, Runner: ver("Bicep CLI version 0.24.23 (abc)", 0, nil)}, sdk.ToolUnsupported, "older than"},
		{"relative explicit", DiscoverOptions{ExplicitPath: "bicep"}, sdk.ToolFailed, "absolute"},
		{"not on path", DiscoverOptions{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }}, sdk.ToolMissing, "not found on PATH"},
		{"dot entry", DiscoverOptions{LookPath: func(string) (string, error) { return "", exec.ErrDot }}, sdk.ToolMissing, "relative PATH"},
		{"missing file", DiscoverOptions{ExplicitPath: filepath.Join(dir, "nope")}, sdk.ToolMissing, "does not exist"},
		{"not executable", DiscoverOptions{ExplicitPath: plain}, sdk.ToolMissing, "not an executable"},
		{"dir", DiscoverOptions{ExplicitPath: dir}, sdk.ToolMissing, "not an executable"},
		{"run error", DiscoverOptions{ExplicitPath: exe, Runner: ver("", 0, errors.New("boom"))}, sdk.ToolFailed, "failed"},
		{"exit 1", DiscoverOptions{ExplicitPath: exe, Runner: ver("", 1, nil)}, sdk.ToolFailed, "failed"},
		{"garbage", DiscoverOptions{ExplicitPath: exe, Runner: ver("hello", 0, nil)}, sdk.ToolFailed, "unrecognised"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Required = true
			d := Discover(t.Context(), tt.opts)
			if d.Status.State != tt.state || !d.Status.Required || d.Status.Name != ToolName {
				t.Fatalf("status = %+v", d.Status)
			}
			if !strings.Contains(d.Status.Detail+d.Status.Version, tt.want) {
				t.Errorf("detail/version = %q / %q, want %q", d.Status.Detail, d.Status.Version, tt.want)
			}
			if d.Available() != (tt.state == sdk.ToolAvailable) {
				t.Error("Available mismatch")
			}
		})
	}
}

func TestNewCompilerRequiresAvailableCLI(t *testing.T) {
	_, err := NewCompiler(Discovery{Status: sdk.ToolStatus{State: sdk.ToolMissing, Detail: "gone"}}, CompilerOptions{ProjectDir: t.TempDir()})
	if !errors.Is(err, ErrCLIUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err = NewCompiler(available("/x"), CompilerOptions{}); !errors.Is(err, ErrBadInput) {
		t.Fatalf("empty dir err = %v", err)
	}
	if _, err = NewCompiler(available("/x"), CompilerOptions{ProjectDir: "/does/not/exist"}); err == nil {
		t.Fatal("missing dir must fail")
	}
	a := NewAdapter(Discovery{Status: sdk.ToolStatus{State: sdk.ToolMissing}}, CompilerOptions{})
	if a.Name() != "bicep" || a.Detect(t.Context()).State != sdk.ToolMissing {
		t.Error("adapter detect")
	}
	if _, err = a.Run(t.Context(), sdk.AdapterRequest{ProjectRoot: t.TempDir()}); !errors.Is(err, ErrCLIUnavailable) {
		t.Errorf("adapter run err = %v", err)
	}
}

func TestCompile(t *testing.T) {
	dir := newProject(t, map[string]string{
		"main.bicep":         "#disable-next-line no-unused-params a b // why\nparam x string\n",
		"modules/m.bicep":    "",
		"main.bicepparam":    "using 'main.bicep'\n",
		"notbicep.txt":       "",
		"other/outside.json": "",
	})
	stderr := dir + "/main.bicep(2,7) : Warning no-unused-params: Parameter \"x\" is declared but never used. [https://aka.ms/bicep/linter-diagnostics#no-unused-params]\n" +
		"WARNING: experimental feature noise\n" + dir + "/modules/m.bicep(3,1) : Error BCP057: bad in " + dir + "/modules/m.bicep. [https://aka.ms/bicep/core-diagnostics#BCP057]\n"
	t.Run("ok", func(t *testing.T) {
		fr := &fakeRunner{results: []RunResult{{Stdout: []byte(`{"resources":[]}`), Stderr: []byte(stderr[:strings.Index(stderr, "WARNING")])}}}
		c, err := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: fr})
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Compile(t.Context(), "main.bicep")
		if err != nil || !res.OK || string(res.ARM) != `{"resources":[]}` || res.Version.String() != "0.47.16" || c.Version() != res.Version {
			t.Fatalf("%v %+v", err, res)
		}
		if len(res.Diagnostics) != 2 {
			t.Fatalf("diagnostics = %+v", res.Diagnostics)
		}
		d := res.Diagnostics[0]
		if d.File != "main.bicep" || d.Pos != (model.Pos{Line: 2, Column: 7}) || d.Code != "no-unused-params" || d.Severity != sdk.SeverityWarning || d.Source != "bicep" {
			t.Errorf("diag = %+v", d)
		}
		info := res.Diagnostics[1]
		if info.Code != CodeDisableDirective || info.Severity != sdk.SeverityInfo || info.Pos.Line != 1 ||
			!strings.HasSuffix(info.Message, "no-unused-params, a, b") {
			t.Errorf("directive diag = %+v", info)
		}
		spec := fr.specs[0]
		if spec.Dir != dir || spec.Path != "/opt/bicep" || strings.Join(spec.Args, " ") != "build "+dir+"/main.bicep --stdout --no-restore" {
			t.Errorf("spec = %+v", spec)
		}
		for _, e := range spec.Env {
			if strings.HasPrefix(e, "AZURE_") || strings.Contains(strings.ToLower(e), "token") {
				t.Errorf("forbidden env %s", e)
			}
		}
	})
	t.Run("compile error", func(t *testing.T) {
		fr := &fakeRunner{results: []RunResult{{ExitCode: 1, Stderr: []byte(stderr)}}}
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: fr})
		res, err := c.Compile(t.Context(), filepath.Join(dir, "main.bicep"))
		if err != nil || res.OK || res.ARM != nil || !HasError(res.Diagnostics) || len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipCompileFailed {
			t.Fatalf("%v %+v", err, res)
		}
		if got := res.Diagnostics[1].Message; strings.Contains(got, dir) || res.Diagnostics[1].File != "modules/m.bicep" {
			t.Errorf("absolute path leaked or file wrong: %+v", res.Diagnostics[1])
		}
	})
	t.Run("failure without diagnostics", func(t *testing.T) {
		fr := &fakeRunner{results: []RunResult{{ExitCode: 1, Stderr: []byte("An error occurred")}}}
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: fr})
		if _, err := c.Compile(t.Context(), "main.bicep"); !errors.Is(err, ErrNoDiagnostics) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("stdout not json", func(t *testing.T) {
		fr := &fakeRunner{results: []RunResult{{Stdout: []byte("oops")}}}
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: fr})
		if _, err := c.Compile(t.Context(), "main.bicep"); !errors.Is(err, ErrNoDiagnostics) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("runner error", func(t *testing.T) {
		fr := &fakeRunner{err: ErrTimeout}
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: fr, Home: t.TempDir()})
		if _, err := c.Compile(t.Context(), "main.bicep"); !errors.Is(err, ErrTimeout) {
			t.Fatalf("err = %v", err)
		}
		if _, err := c.CompileParams(t.Context(), "main.bicepparam"); !errors.Is(err, ErrTimeout) {
			t.Fatalf("params err = %v", err)
		}
	})
	t.Run("input checks", func(t *testing.T) {
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: &fakeRunner{}})
		outside := newProject(t, map[string]string{"evil.bicep": ""})
		link := filepath.Join(dir, "link.bicep")
		if err := os.Symlink(filepath.Join(outside, "evil.bicep"), link); err != nil {
			t.Skip("symlinks unavailable")
		}
		for _, f := range []string{"notbicep.txt", "missing.bicep", filepath.Join(outside, "evil.bicep"), "link.bicep", "../x.bicep"} {
			if _, err := c.Compile(t.Context(), f); !errors.Is(err, ErrBadInput) {
				t.Errorf("%s: err = %v", f, err)
			}
		}
		if _, err := c.CompileParams(t.Context(), "main.bicep"); !errors.Is(err, ErrBadInput) {
			t.Errorf("params ext: %v", err)
		}
	})
}

func TestCompileParams(t *testing.T) {
	dir := newProject(t, map[string]string{"p.bicepparam": "using 'm.bicep'\n"})
	good := `{"parametersJson":"{\"parameters\":{\"a\":{\"value\":1},\"kv\":{\"reference\":{\"x\":1}}}}","templateJson":"{\"resources\":[]}","templateSpecId":null}`
	run := func(r RunResult) (ParamsResult, error) {
		c, _ := NewCompiler(available("/opt/bicep"), CompilerOptions{ProjectDir: dir, Runner: &fakeRunner{results: []RunResult{r}}})
		return c.CompileParams(t.Context(), "p.bicepparam")
	}
	res, err := run(RunResult{Stdout: []byte(good)})
	if err != nil || !res.OK || len(res.Values) != 1 || res.Values["a"] != float64(1) || string(res.Template) != `{"resources":[]}` || res.TemplateSpecID != "" {
		t.Fatalf("%v %+v", err, res)
	}
	res, err = run(RunResult{Stdout: []byte(strings.Replace(good, "null", `"ts/id"`, 1))})
	if err != nil || res.TemplateSpecID != "ts/id" {
		t.Fatalf("%v %+v", err, res)
	}
	// BCP427: environment variable not available -> diagnostic plus skip, not a Go error.
	env := []byte(dir + "/p.bicepparam(4,39) : Error BCP427: Environment variable \"X\" does not exist and there's no default value set. [https://aka.ms/bicep/core-diagnostics#BCP427]\n")
	res, err = run(RunResult{ExitCode: 1, Stderr: env})
	if err != nil || res.OK || len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipParamEnvVar || res.Diagnostics[0].Code != "BCP427" {
		t.Fatalf("%v %+v", err, res)
	}
	other := []byte(dir + "/p.bicepparam(1,1) : Error BCP999: x\n")
	if res, err = run(RunResult{ExitCode: 1, Stderr: other}); err != nil || len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipCompileFailed {
		t.Fatalf("%v %+v", err, res)
	}
	for name, r := range map[string]RunResult{
		"no diagnostics": {ExitCode: 1},
		"bad json":       {Stdout: []byte("{")},
		"bad template":   {Stdout: []byte(`{"parametersJson":"{}","templateJson":"x"}`)},
		"bad params":     {Stdout: []byte(`{"parametersJson":"[1]","templateJson":"{}"}`)},
	} {
		if _, err := run(r); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestAdapterRun(t *testing.T) {
	dir := newProject(t, map[string]string{"a.bicep": "", "p.bicepparam": "", "x.txt": ""})
	stderr := dir + "/a.bicep(1,2) : Warning no-unused-vars: v. [https://aka.ms/bicep/linter-diagnostics#no-unused-vars]\n"
	fr := &fakeRunner{results: []RunResult{{Stdout: []byte("{}"), Stderr: []byte(stderr)}, {Stdout: []byte(`{"parametersJson":"{}","templateJson":"{}"}`)}}}
	a := NewAdapter(available("/opt/bicep"), CompilerOptions{Runner: fr})
	fs, err := a.Run(t.Context(), sdk.AdapterRequest{ProjectRoot: dir, Profile: "foundry-dev", Paths: []string{"a.bicep", "p.bicepparam"}})
	if err != nil || len(fs) != 1 {
		t.Fatalf("%v %+v", err, fs)
	}
	f := fs[0]
	if f.RuleID != "bicep/no-unused-vars" || f.Severity != sdk.SeverityWarning || f.Adapter != "bicep" || f.Confidence != sdk.ConfidenceCertain ||
		f.Location.File != "a.bicep" || f.Location.Line != 1 || f.Location.Column != 2 || f.Profile != "foundry-dev" ||
		f.DocsURL != "https://aka.ms/bicep/linter-diagnostics#no-unused-vars" || f.Resource.Kind != "file" {
		t.Errorf("finding = %+v", f)
	}
	if _, err = a.Run(t.Context(), sdk.AdapterRequest{ProjectRoot: dir, Paths: []string{"x.txt"}}); !errors.Is(err, ErrBadInput) {
		t.Errorf("err = %v", err)
	}
	fr.err = ErrTimeout
	if _, err = a.Run(t.Context(), sdk.AdapterRequest{ProjectRoot: dir, Paths: []string{"a.bicep"}}); !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v", err)
	}
	if _, err = a.Run(t.Context(), sdk.AdapterRequest{ProjectRoot: dir, Paths: []string{"p.bicepparam"}}); !errors.Is(err, ErrTimeout) {
		t.Errorf("err = %v", err)
	}
}

// ---- diagnostics from captured compiler output ----

func TestParseCapturedDiagnostics(t *testing.T) {
	tests := []struct {
		file string
		want []string // code@line:col:severity
	}{
		{"error.stderr", []string{"BCP037@8:5:warning", "BCP057@11:21:error"}},
		{"lint-warning.stderr", []string{"no-unused-params@1:7:warning", "no-unused-vars@2:5:warning"}},
		{"secure-output.stderr", []string{"outputs-should-not-contain-secrets@8:21:warning", "outputs-should-not-contain-secrets@11:26:warning"}},
		{"readenv.stderr", []string{"BCP427@4:39:error"}},
		{"readenv-default.stderr", nil},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			ds := ParseDiagnostics(string(readFixture(t, "diagnostics", tt.file)))
			var got []string
			for _, d := range ds {
				got = append(got, fmt.Sprintf("%s@%d:%d:%s", d.Code, d.Line, d.Column, d.Severity))
				if d.DocsURL != DocsURL(d.Code) {
					t.Errorf("docs url %q != %q", d.DocsURL, DocsURL(d.Code))
				}
				if strings.Contains(d.Message, "https://aka.ms/bicep/") && strings.HasSuffix(d.Message, "]") {
					t.Errorf("docs url left in message: %q", d.Message)
				}
			}
			if strings.Join(got, ";") != strings.Join(tt.want, ";") {
				t.Errorf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestParseDiagnosticsFormats(t *testing.T) {
	ds := ParseDiagnostics("C:\\p\\a.bicep(3,4) : Info some-rule: hi\r\n/x : Error BCP1: no position\nrandom : text\n")
	if len(ds) != 2 || ds[0].Severity != sdk.SeverityInfo || ds[0].File != "C:\\p\\a.bicep" || ds[0].Line != 3 || ds[1].Line != 0 || ds[1].File != "/x" {
		t.Fatalf("%+v", ds)
	}
}

func TestCleanMessageAndPaths(t *testing.T) {
	long := strings.Repeat("é", 800)
	m := cleanMessage("a\x00b\n"+long, "")
	if len(m) > maxMessage+3 || strings.ContainsAny(m, "\x00\n") {
		t.Errorf("message not cleaned: %d", len(m))
	}
	if got := relPath("/p", "/q/outside.bicep"); got != "outside.bicep" {
		t.Errorf("outside = %s", got)
	}
	if got := relPath("/p", "sub/x.bicep"); got != "sub/x.bicep" {
		t.Errorf("relative = %s", got)
	}
	if relPath("/p", "") != "" || relPath("", "/a/b.bicep") != "b.bicep" {
		t.Error("edge cases")
	}
	for code, want := range map[string]string{
		"": "", CodeDisableDirective: "", "BCP035": "https://aka.ms/bicep/core-diagnostics#BCP035",
		"no-hardcoded-env-urls": "https://aka.ms/bicep/linter-diagnostics#no-hardcoded-env-urls",
	} {
		if DocsURL(code) != want {
			t.Errorf("DocsURL(%q)", code)
		}
	}
}

func TestScanDisableDirectives(t *testing.T) {
	ds, err := ScanDisableDirectives(fixturePath("params"), fixturePath("params", "m.bicep"))
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %+v", err, ds)
	}
	d := ds[0]
	if d.Code != "disable-next-line" || d.Severity != sdk.SeverityInfo || d.File != "m.bicep" || d.Pos.Line != 4 || !strings.Contains(d.Message, "no-unused-params") {
		t.Errorf("%+v", d)
	}
	f := ToFinding(d, "foundry-test")
	if f.RuleID != "bicep/disable-next-line" || f.Severity != sdk.SeverityInfo || f.DocsURL != "" || f.Location.Line != 4 {
		t.Errorf("%+v", f)
	}
	if _, err := ScanDisableDirectives("", "/does/not/exist.bicep"); err == nil {
		t.Error("missing file must fail")
	}
}

// ---- real process execution against a fake CLI script ----

func writeScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake CLI")
	}
	p := filepath.Join(t.TempDir(), "bicep")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecRunner(t *testing.T) {
	t.Setenv("AZURE_CLIENT_SECRET", "must-not-leak")
	t.Setenv("MY_TOKEN", "must-not-leak")
	script := writeScript(t, `
case "$1" in
  --version) echo "Bicep CLI version 0.47.16 (abc)";;
  envdump) printenv; pwd;;
  big) head -c 100000 /dev/zero;;
  slow) sleep 30;;
  fail) echo "boom" >&2; exit 3;;
esac
`)
	r := ExecRunner{}
	dir := t.TempDir()
	home := t.TempDir()
	spec := RunSpec{Path: script, Dir: dir, Env: MinimalEnv(home)}

	spec.Args = []string{"envdump"}
	res, err := r.Run(t.Context(), spec)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	out := string(res.Stdout)
	for _, bad := range []string{"AZURE_", "must-not-leak", "TOKEN", "SECRET"} {
		if strings.Contains(out, bad) {
			t.Errorf("environment leaked %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{"HOME=" + home, "DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !strings.Contains(out, filepath.Base(dir)) {
		t.Error("working directory not applied")
	}

	spec.Args = []string{"fail"}
	if res, err = r.Run(t.Context(), spec); err != nil || res.ExitCode != 3 || !strings.Contains(string(res.Stderr), "boom") {
		t.Errorf("fail: %v %+v", err, res)
	}
	spec.Args, spec.MaxStdout = []string{"big"}, 1000
	if res, err = r.Run(t.Context(), spec); !errors.Is(err, ErrOutputTooLarge) || !res.Truncated || len(res.Stdout) != 1000 {
		t.Errorf("big: %v len=%d", err, len(res.Stdout))
	}
	spec.MaxStdout = 0
	spec.Args, spec.Timeout = []string{"slow"}, 200*time.Millisecond
	start := time.Now()
	if _, err = r.Run(t.Context(), spec); !errors.Is(err, ErrTimeout) || time.Since(start) > 10*time.Second {
		t.Errorf("slow: %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	spec.Timeout = 0
	if _, err = r.Run(ctx, spec); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	if _, err = r.Run(t.Context(), RunSpec{}); err == nil {
		t.Error("empty path must fail")
	}
	if _, err = r.Run(t.Context(), RunSpec{Path: filepath.Join(dir, "none")}); err == nil {
		t.Error("missing executable must fail")
	}

	// Discovery and compile through the real runner.
	d := Discover(t.Context(), DiscoverOptions{ExplicitPath: script, Required: true})
	if !d.Available() || d.Status.Version != "0.47.16" || d.Version.Hash != "abc" {
		t.Fatalf("discover = %+v", d)
	}
}

func TestMinimalEnvHasNoAmbientVariables(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "t")
	for _, e := range MinimalEnv("/h") {
		if strings.HasPrefix(e, "AZURE_") || strings.Contains(e, "=t") && strings.HasPrefix(e, "AZURE") {
			t.Errorf("unexpected %s", e)
		}
	}
}

func TestIsEnvVarMissing(t *testing.T) {
	e := sdk.SeverityError
	cases := []struct {
		d    model.Diagnostic
		want bool
	}{
		{model.Diagnostic{Code: "BCP427", Severity: e}, true},
		{model.Diagnostic{Code: "BCP338", Severity: e, Message: "Environment variable does not exist"}, true},
		{model.Diagnostic{Code: "BCP338", Severity: e, Message: "other"}, false},
		{model.Diagnostic{Code: "BCP427", Severity: sdk.SeverityWarning}, false},
	}
	for _, c := range cases {
		if isEnvVarMissing(c.d) != c.want {
			t.Errorf("%+v", c.d)
		}
	}
}
