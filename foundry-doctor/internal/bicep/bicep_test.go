package bicep

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	res   RunResult
	err   error
	calls [][]string
	dir   string
}

func (f *fakeRunner) Run(_ context.Context, _ string, args []string, dir string) (RunResult, error) {
	f.calls = append(f.calls, args)
	f.dir = dir
	return f.res, f.err
}

type fakeInfo struct{ dir bool }

func (fakeInfo) Name() string       { return "bicep" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool      { return f.dir }
func (fakeInfo) Sys() any           { return nil }

func okStat(string) (os.FileInfo, error) { return fakeInfo{}, nil }

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{"Bicep CLI version 0.48.1 (abc123)", Version{0, 48, 1}, false},
		{"warn\nBicep CLI version 1.2.3 (x)\n", Version{1, 2, 3}, false},
		{"", Version{}, true},
		{"Bicep CLI version x.y.z", Version{}, true},
	}
	for _, tc := range tests {
		got, err := ParseVersion(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseVersion(%q)=%v,%v", tc.in, got, err)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	tests := []struct {
		a, b Version
		want int
	}{
		{Version{0, 48, 1}, Version{0, 48, 1}, 0},
		{Version{0, 47, 16}, Version{0, 48, 1}, -1},
		{Version{0, 48, 2}, Version{0, 48, 1}, 1},
		{Version{1, 0, 0}, Version{0, 99, 99}, 1},
	}
	for _, tc := range tests {
		if got := tc.a.Compare(tc.b); got != tc.want {
			t.Errorf("%v vs %v = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if (Version{0, 48, 1}).String() != "0.48.1" || ContractVersion != Contract.String() {
		t.Error("contract string mismatch")
	}
}

func TestDiscover(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	lookOK := func(string) (string, error) { return "/usr/bin/bicep", nil }
	lookNone := func(string) (string, error) { return "", errors.New("not found") }
	ver := func(s string) *fakeRunner { return &fakeRunner{res: RunResult{Stdout: []byte(s)}} }

	tests := []struct {
		name     string
		opts     DiscoverOptions
		wantKind Kind
		wantSrc  string
		newer    bool
		dep      bool
	}{
		{"path ok", DiscoverOptions{Getenv: env(nil), LookPath: lookOK, Runner: ver("Bicep CLI version 0.48.1 (h)")}, "", "PATH", false, false},
		{"env ok", DiscoverOptions{Getenv: env(map[string]string{"BICEP_PATH": "/x/bicep"}), Stat: okStat, Runner: ver("Bicep CLI version 0.48.1 (h)")}, "", "BICEP_PATH", false, false},
		{"auto means PATH", DiscoverOptions{Path: "auto", Getenv: env(map[string]string{"BICEP_PATH": "auto"}), LookPath: lookOK, Runner: ver("Bicep CLI version 0.48.1 (h)")}, "", "PATH", false, false},
		{"option wins", DiscoverOptions{Path: "/o/bicep", Getenv: env(map[string]string{"BICEP_PATH": "/x"}), Stat: okStat, Runner: ver("Bicep CLI version 0.49.0 (h)")}, "", "option", true, false},
		{"missing on PATH", DiscoverOptions{Getenv: env(nil), LookPath: lookNone, Runner: ver("")}, KindMissing, "", false, true},
		{"missing configured", DiscoverOptions{Getenv: env(map[string]string{"BICEP_PATH": "/nope"}), Stat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }, Runner: ver("")}, KindMissing, "", false, true},
		{"directory", DiscoverOptions{Path: "/d", Getenv: env(nil), Stat: func(string) (os.FileInfo, error) { return fakeInfo{dir: true}, nil }, Runner: ver("")}, KindMissing, "", false, true},
		{"too old", DiscoverOptions{Getenv: env(nil), LookPath: lookOK, Runner: ver("Bicep CLI version 0.47.16 (h)")}, KindVersionTooOld, "", false, true},
		{"unknown version", DiscoverOptions{Getenv: env(nil), LookPath: lookOK, Runner: ver("hello")}, KindVersionUnknown, "", false, true},
		{"non-zero exit", DiscoverOptions{Getenv: env(nil), LookPath: lookOK, Runner: &fakeRunner{res: RunResult{ExitCode: 3}}}, KindNotExecutable, "", false, true},
		{"start failure", DiscoverOptions{Getenv: env(nil), LookPath: lookOK, Runner: &fakeRunner{err: errors.New("boom")}}, KindNotExecutable, "", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := Discover(context.Background(), tc.opts)
			if tc.wantKind == "" {
				if err != nil {
					t.Fatal(err)
				}
				if tool.Source != tc.wantSrc || tool.Newer != tc.newer {
					t.Fatalf("tool=%+v", tool)
				}
				return
			}
			var te *ToolError
			if !errors.As(err, &te) || te.Kind != tc.wantKind {
				t.Fatalf("err=%v want kind %s", err, tc.wantKind)
			}
			if IsDependencyError(err) != tc.dep {
				t.Fatalf("IsDependencyError=%v want %v", IsDependencyError(err), tc.dep)
			}
		})
	}
}

func TestDiscoverSentinels(t *testing.T) {
	_, err := Discover(context.Background(), DiscoverOptions{
		Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { return "", errors.New("x") },
	})
	if !errors.Is(err, ErrCLIMissing) {
		t.Fatalf("want ErrCLIMissing, got %v", err)
	}
	_, err = Discover(context.Background(), DiscoverOptions{
		Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { return "b", nil },
		Runner: &fakeRunner{res: RunResult{Stdout: []byte("Bicep CLI version 0.1.0 (h)")}},
	})
	if !errors.Is(err, ErrVersionUnsupported) {
		t.Fatalf("want ErrVersionUnsupported, got %v", err)
	}
	if IsDependencyError(errors.New("other")) || IsDependencyError(nil) {
		t.Error("non-tool errors are not dependency errors")
	}
}

func TestDiscoverContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Discover(ctx, DiscoverOptions{
		Getenv: func(string) string { return "" }, LookPath: func(string) (string, error) { return "b", nil },
		Runner: &fakeRunner{err: ctx.Err()},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestParseTextDiagnostics(t *testing.T) {
	in := "Bicep banner\n/proj/main.bicep(3,5) : Error BCP057: The name \"x\" does not exist. [https://aka.ms/bicep/core-diagnostics#BCP057]\n" +
		"/proj/mod/a.bicep(1,1) : Warning no-unused-params: Parameter unused.\n" +
		"\x1b[31m/other/z.bicep(2,2) : Info x-y: note\x1b[0m\n"
	got := ParseTextDiagnostics(in, "/proj")
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	by := map[string]Diagnostic{}
	for _, d := range got {
		by[d.Code] = d
	}
	got = []Diagnostic{by["BCP057"], by["no-unused-params"], by["x-y"]}
	if got[0].File != "main.bicep" || got[0].Severity != SeverityError || got[0].DocsURL == "" || strings.Contains(got[0].Message, "aka.ms") {
		t.Errorf("first: %+v", got[0])
	}
	if !strings.HasSuffix(got[1].File, "a.bicep") || got[2].File != "z.bicep" {
		t.Errorf("paths leak or wrong: %+v %+v", got[1], got[2])
	}
	if len(ParseTextDiagnostics("nothing here", "")) != 0 {
		t.Error("expected none")
	}
}

func TestParseSARIF(t *testing.T) {
	valid := `{"runs":[{"results":[
	 {"ruleId":"BCP057","level":"error","message":{"text":"bad"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"file:///proj/main.bicep"},"region":{"startLine":3,"startColumn":5}}}]},
	 {"ruleId":"no-unused-params","message":{"text":"unused"},"locations":[]}]}]}`
	tests := []struct {
		name    string
		in      string
		wantN   int
		wantErr bool
	}{
		{"valid", valid, 2, false},
		{"empty runs", `{"runs":[]}`, 0, false},
		{"malformed", `{"runs":`, 0, true},
		{"bad rule id", `{"runs":[{"results":[{"ruleId":"bad id!","message":{"text":"x"}}]}]}`, 0, true},
		{"negative line", `{"runs":[{"results":[{"ruleId":"A1","message":{"text":"x"},"locations":[{"physicalLocation":{"region":{"startLine":-1}}}]}]}]}`, 0, true},
		{"too many runs", `{"runs":[{},{},{},{},{},{},{},{},{}]}`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSARIF([]byte(tc.in), "/proj")
			if (err != nil) != tc.wantErr || len(got) != tc.wantN {
				t.Fatalf("got %v err %v", got, err)
			}
			if tc.wantErr && !errors.Is(err, ErrAdapter) {
				t.Errorf("want adapter error: %v", err)
			}
		})
	}
	got, _ := ParseSARIF([]byte(valid), "/proj")
	if got[0].File != "" && got[1].File != "main.bicep" {
		t.Errorf("order/paths: %+v", got)
	}
	var warn *Diagnostic
	for i := range got {
		if got[i].Code == "no-unused-params" {
			warn = &got[i]
		}
	}
	if warn == nil || warn.Severity != SeverityWarning {
		t.Errorf("absent level should be warning: %+v", warn)
	}
	if !HasErrors(got) || HasErrors(nil) {
		t.Error("HasErrors")
	}
}

func TestSanitizeRedactsAndStrips(t *testing.T) {
	got := sanitize("\x1b[31mpassword=hunter2\x1b[0m ok\x07", 100)
	if strings.Contains(got, "hunter2") || strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("not sanitised: %q", got)
	}
	if n := len(sanitize(strings.Repeat("a", 500), 50)); n > 60 {
		t.Errorf("not truncated: %d", n)
	}
}

const goodARM = `{"resources":[{"type":"Microsoft.Storage/storageAccounts","apiVersion":"2023-01-01","name":"s"}]}`

func TestCompileInterpret(t *testing.T) {
	sarifErr := `{"runs":[{"results":[{"ruleId":"BCP057","level":"error","message":{"text":"bad"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"error.bicep"},"region":{"startLine":2,"startColumn":1}}}]}]}]}`
	tests := []struct {
		name     string
		res      RunResult
		err      error
		wantOK   bool
		wantKind Kind
		wantDiag int
	}{
		{"success", RunResult{Stdout: []byte(goodARM)}, nil, true, "", 0},
		{"success with warning", RunResult{Stdout: []byte(goodARM), Stderr: []byte("m.bicep(1,1) : Warning w-x: careful")}, nil, true, "", 1},
		{"compile error", RunResult{ExitCode: 1, Stderr: []byte(sarifErr)}, nil, false, "", 1},
		{"error text fallback", RunResult{ExitCode: 1, Stderr: []byte("e.bicep(1,1) : Error BCP001: nope")}, nil, false, "", 1},
		{"no diagnostics on failure", RunResult{ExitCode: 1, Stderr: []byte("crash")}, nil, false, KindAdapter, 0},
		{"malformed ARM", RunResult{Stdout: []byte("{not json")}, nil, false, KindAdapter, 0},
		{"ARM without resources", RunResult{Stdout: []byte(`{"a":1}`)}, nil, false, KindAdapter, 0},
		{"empty stdout", RunResult{}, nil, false, KindAdapter, 0},
		{"truncated", RunResult{Stdout: []byte(goodARM), Truncated: true}, nil, false, KindAdapter, 0},
		{"malformed sarif", RunResult{ExitCode: 1, Stderr: []byte(`{"runs":[{"results":[{"ruleId":"!!"}]}]}`)}, nil, false, KindAdapter, 0},
		{"timeout", RunResult{}, toolErr(KindTimeout, "t", nil), false, KindTimeout, 0},
		{"start failure", RunResult{}, errors.New("exec"), false, KindNotExecutable, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeRunner{res: tc.res, err: tc.err}
			c := Compiler{Tool: Tool{Path: "bicep", Version: Contract}, Runner: fr}
			r, err := c.Compile(context.Background(), "/p/sub/main.bicep")
			if tc.wantKind != "" {
				var te *ToolError
				if !errors.As(err, &te) || te.Kind != tc.wantKind {
					t.Fatalf("err=%v want %s", err, tc.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.OK != tc.wantOK || len(r.Diagnostics) != tc.wantDiag {
				t.Fatalf("%+v", r)
			}
			args := strings.Join(fr.calls[0], " ")
			if !strings.Contains(args, "--no-restore") || !strings.Contains(args, "--stdout") || strings.Contains(args, "/p/") {
				t.Errorf("args: %s", args)
			}
			if fr.dir == "" {
				t.Error("working dir not set")
			}
		})
	}
}

func TestCompileNoTool(t *testing.T) {
	_, err := Compiler{}.Compile(context.Background(), "x.bicep")
	if !IsDependencyError(err) {
		t.Fatalf("want dependency error, got %v", err)
	}
}
