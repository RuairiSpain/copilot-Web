package psrule_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters/psrule"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const fixtureDir = "../../../test/fixtures/adapters/psrule"

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeRunner stands in for process execution. detect and run are separate handlers, selected by the
// script argument, so a test controls what "pwsh" would print and write.
type fakeRunner struct {
	detect func(adapters.Command) (adapters.Result, error)
	run    func(adapters.Command) (adapters.Result, error)
	calls  []adapters.Command
}

func (f *fakeRunner) Run(_ context.Context, c adapters.Command) (adapters.Result, error) {
	f.calls = append(f.calls, c)
	if len(c.Args) == 4 && strings.Contains(c.Args[3], "Get-Module -ListAvailable") {
		return f.detect(c)
	}
	return f.run(c)
}

func detectOut(json string) func(adapters.Command) (adapters.Result, error) {
	return func(adapters.Command) (adapters.Result, error) { return adapters.Result{Stdout: []byte(json)}, nil }
}

const okDetect = `{"psVersion":"7.4.6","module":"1.47.0"}`

func newAdapter(f *fakeRunner, mut func(*psrule.Config)) *psrule.Adapter {
	cfg := psrule.Config{
		Runner:   f,
		LookPath: func(string) (string, error) { return "/opt/pwsh/pwsh", nil },
		Environ: func() []string {
			return []string{"PATH=/usr/bin", "HOME=/home/u", "AZURE_CLIENT_SECRET=leak", "GITHUB_TOKEN=leak", "PSModulePath=/m"}
		},
	}
	if mut != nil {
		mut(&cfg)
	}
	return psrule.New(cfg)
}

func TestName(t *testing.T) {
	if psrule.New(psrule.Config{}).Name() != "psrule" {
		t.Error("adapter name must be psrule")
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name   string
		mut    func(*psrule.Config)
		detect func(adapters.Command) (adapters.Result, error)
		state  sdk.ToolState
		inDet  string
		calls  int
	}{
		{"available", nil, detectOut(okDetect), sdk.ToolAvailable, "PSRule.Rules.Azure 1.47.0", 1},
		{"disabled", func(c *psrule.Config) { c.Mode = adapters.ModeFalse }, nil, sdk.ToolDisabled, "disabled", 0},
		{"pwsh missing", func(c *psrule.Config) {
			c.LookPath = func(string) (string, error) { return "", errors.New("not found") }
		}, nil, sdk.ToolMissing, "pwsh not found", 0},
		{"pwsh relative path", func(c *psrule.Config) {
			c.LookPath = func(string) (string, error) { return "./pwsh", nil }
		}, nil, sdk.ToolMissing, "non-absolute", 0},
		{"module missing", nil, detectOut(`{"psVersion":"7.4.6","module":null}`), sdk.ToolMissing, "not installed", 1},
		{"module empty", nil, detectOut(`{"psVersion":"7.4.6","module":""}`), sdk.ToolMissing, "not installed", 1},
		{"old powershell", nil, detectOut(`{"psVersion":"7.2.1","module":"1.47.0"}`), sdk.ToolUnsupported, "older than the minimum 7.4.0", 1},
		{"preview powershell ok", nil, detectOut(`{"psVersion":"7.5.0-preview.1","module":"1.47.0"}`), sdk.ToolAvailable, "", 1},
		{"module below configured minimum", func(c *psrule.Config) { c.MinModule = psrule.ProposedMinModule },
			detectOut(`{"psVersion":"7.4.6","module":"1.40.0"}`), sdk.ToolUnsupported, "older than the minimum 1.47.0", 1},
		{"module below proposed minimum is detection only", nil, detectOut(`{"psVersion":"7.4.6","module":"1.40.0"}`), sdk.ToolAvailable, "", 1},
		{"runner error", nil, func(adapters.Command) (adapters.Result, error) { return adapters.Result{}, adapters.ErrTimeout }, sdk.ToolFailed, "timed out", 1},
		{"runner error output too large", nil, func(adapters.Command) (adapters.Result, error) {
			return adapters.Result{}, adapters.ErrOutputTooLarge
		}, sdk.ToolFailed, "output too large", 1},
		{"runner error other", nil, func(adapters.Command) (adapters.Result, error) {
			return adapters.Result{}, errors.New("secret path /x")
		}, sdk.ToolFailed, "could not execute", 1},
		{"non-zero exit", nil, func(adapters.Command) (adapters.Result, error) { return adapters.Result{ExitCode: 1}, nil }, sdk.ToolFailed, "code 1", 1},
		{"not json", nil, detectOut(`PowerShell 7.4`), sdk.ToolFailed, "not the expected JSON", 1},
		{"unknown field", nil, detectOut(`{"psVersion":"7.4.6","module":"1.47.0","extra":1}`), sdk.ToolFailed, "not the expected JSON", 1},
		{"bad ps version", nil, detectOut(`{"psVersion":"seven","module":"1.47.0"}`), sdk.ToolFailed, "unparseable PowerShell", 1},
		{"bad module version", nil, detectOut(`{"psVersion":"7.4.6","module":"x.y"}`), sdk.ToolFailed, "unparseable PSRule", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{detect: tc.detect}
			st := newAdapter(f, tc.mut).Detect(context.Background())
			if st.State != tc.state || st.Name != "psrule" {
				t.Fatalf("state %q (%s), want %q", st.State, st.Detail, tc.state)
			}
			if !strings.Contains(st.Detail, tc.inDet) {
				t.Errorf("detail %q lacks %q", st.Detail, tc.inDet)
			}
			if len(f.calls) != tc.calls {
				t.Errorf("%d runner calls, want %d", len(f.calls), tc.calls)
			}
		})
	}
}

func TestDetectCommandIsFixedAndEnvRestricted(t *testing.T) {
	f := &fakeRunner{detect: detectOut(okDetect)}
	st := newAdapter(f, nil).Detect(context.Background())
	if st.State != sdk.ToolAvailable || st.Version != "1.47.0" || st.Path != "/opt/pwsh/pwsh" {
		t.Fatalf("status %+v", st)
	}
	c := f.calls[0]
	if c.Path != "/opt/pwsh/pwsh" || !slices.Equal(c.Args[:3], []string{"-NoProfile", "-NonInteractive", "-Command"}) || len(c.Args) != 4 {
		t.Errorf("args %v", c.Args)
	}
	env := strings.Join(c.Env, "\n")
	for _, bad := range []string{"AZURE_CLIENT_SECRET", "GITHUB_TOKEN", "leak"} {
		if strings.Contains(env, bad) {
			t.Errorf("environment contains %q", bad)
		}
	}
	for _, want := range []string{"PATH=/usr/bin", "PSModulePath=/m", "POWERSHELL_TELEMETRY_OPTOUT=1", "NO_COLOR=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %q: %v", want, c.Env)
		}
	}
	if c.Timeout <= 0 || c.MaxOutput <= 0 {
		t.Errorf("detect needs a timeout and output limit: %+v", c)
	}
}

func TestDetectRequiredFlagAndAvailability(t *testing.T) {
	f := &fakeRunner{}
	a := newAdapter(f, func(c *psrule.Config) {
		c.Mode = adapters.ModeTrue
		c.LookPath = func(string) (string, error) { return "", errors.New("x") }
	})
	st := a.Detect(context.Background())
	if !st.Required || st.State != sdk.ToolMissing {
		t.Fatalf("%+v", st)
	}
	u := a.Availability(st)
	if u == nil || !u.Fatal || u.Adapter != "psrule" {
		t.Errorf("explicitly enabled and missing must be fatal (exit 2): %+v", u)
	}
	auto := newAdapter(f, func(c *psrule.Config) { c.LookPath = func(string) (string, error) { return "", errors.New("x") } })
	if u := auto.Availability(auto.Detect(context.Background())); u == nil || u.Fatal {
		t.Errorf("auto and missing must be a non-fatal diagnostic: %+v", u)
	}
}

func armOptions(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{psrule.InputPrefix + "infra/main.bicep": fixture(t, "arm-main.json")}
}

// writeResults returns a run handler that, like Assert-PSRule, writes the SARIF file into the work dir.
func writeResults(t *testing.T, content string, seen *adapters.Command, files *[]string) func(adapters.Command) (adapters.Result, error) {
	return func(c adapters.Command) (adapters.Result, error) {
		if seen != nil {
			*seen = c
		}
		if files != nil {
			_ = filepath.Walk(c.Dir, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					rel, _ := filepath.Rel(c.Dir, p)
					*files = append(*files, filepath.ToSlash(rel))
				}
				return nil
			})
		}
		if content != "" {
			if err := os.WriteFile(filepath.Join(c.Dir, "out", "results.sarif"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return adapters.Result{}, nil
	}
}

func TestRunHappyPath(t *testing.T) {
	var seen adapters.Command
	var files []string
	f := &fakeRunner{detect: detectOut(okDetect), run: writeResults(t, fixture(t, "results-basic.sarif.json"), &seen, &files)}
	a := newAdapter(f, func(c *psrule.Config) { c.Mapper = overlapIndex() })
	got, err := a.Run(context.Background(), sdk.AdapterRequest{ProjectRoot: "/ignored", Options: armOptions(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d findings: %+v", len(got), got)
	}
	for _, g := range got {
		if g.Adapter != "psrule" {
			t.Errorf("adapter %q", g.Adapter)
		}
	}
	// The command is the fixed constant; nothing from the request is interpolated.
	if len(seen.Args) != 4 || seen.Args[3] != psrule.RunScriptForTest() {
		t.Errorf("args %v", seen.Args)
	}
	if strings.Contains(seen.Args[3], "main.bicep") || strings.Contains(seen.Args[3], seen.Dir) {
		t.Error("request data leaked into the command text")
	}
	wantFiles := []string{"input/0001.json", "input/0001.parameters.json", "ps-rule.yaml"}
	if !slices.Equal(files, wantFiles) {
		t.Errorf("work dir files %v, want %v", files, wantFiles)
	}
	env := strings.Join(seen.Env, "\n")
	if strings.Contains(env, "leak") || !strings.Contains(env, "TMPDIR="+seen.Dir) {
		t.Errorf("env %v", seen.Env)
	}
	if seen.Timeout <= 0 || seen.MaxOutput <= 0 {
		t.Errorf("run needs a timeout and output limit: %+v", seen)
	}
	if _, err := os.Stat(seen.Dir); !os.IsNotExist(err) {
		t.Errorf("work directory %s was not removed", seen.Dir)
	}
	if seen.Dir == os.TempDir() || !strings.Contains(filepath.Base(seen.Dir), "fd-psrule-") {
		t.Errorf("not a fresh work directory: %s", seen.Dir)
	}
}

func TestRunRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("disabled never runs the tool", func(t *testing.T) {
		f := &fakeRunner{}
		a := newAdapter(f, func(c *psrule.Config) { c.Mode = adapters.ModeFalse })
		_, err := a.Run(ctx, sdk.AdapterRequest{Options: armOptions(t)})
		if !errors.Is(err, adapters.ErrDisabled) || len(f.calls) != 0 {
			t.Errorf("err %v calls %d", err, len(f.calls))
		}
	})
	t.Run("missing", func(t *testing.T) {
		f := &fakeRunner{detect: detectOut(`{"psVersion":"7.4.6","module":null}`)}
		_, err := newAdapter(f, nil).Run(ctx, sdk.AdapterRequest{Options: armOptions(t)})
		if !errors.Is(err, adapters.ErrUnavailable) || !strings.Contains(err.Error(), "not installed") {
			t.Errorf("err %v", err)
		}
		if len(f.calls) != 1 {
			t.Errorf("only detection may run, got %d calls", len(f.calls))
		}
	})
	t.Run("no input", func(t *testing.T) {
		f := &fakeRunner{detect: detectOut(okDetect)}
		_, err := newAdapter(f, nil).Run(ctx, sdk.AdapterRequest{Options: map[string]string{"other": "x"}})
		if !errors.Is(err, psrule.ErrNoInput) || len(f.calls) != 0 {
			t.Errorf("err %v calls %d", err, len(f.calls))
		}
	})
	badInputs := map[string]map[string]string{
		"absolute name":    {"arm:/etc/passwd": "{}"},
		"traversal":        {"arm:../x.bicep": "{}"},
		"backslash":        {`arm:a\b.bicep`: "{}"},
		"drive":            {"arm:C:/x": "{}"},
		"control char":     {"arm:a\x01b": "{}"},
		"empty segment":    {"arm:a//b": "{}"},
		"empty name":       {"arm:": "{}"},
		"not json":         {"arm:a.bicep": "nope"},
		"not object":       {"arm:a.bicep": "[1]"},
		"null":             {"arm:a.bicep": "null"},
		"oversize":         {"arm:a.bicep": "{\"x\":\"" + strings.Repeat("a", 17<<20) + "\"}"},
		"too many":         manyInputs(257),
		"name too long":    {"arm:" + strings.Repeat("a", 300): "{}"},
		"dot segment only": {"arm:./a": "{}"},
	}
	for name, opts := range badInputs {
		t.Run("invalid input "+name, func(t *testing.T) {
			f := &fakeRunner{detect: detectOut(okDetect)}
			if _, err := newAdapter(f, nil).Run(ctx, sdk.AdapterRequest{Options: opts}); err == nil || len(f.calls) != 0 {
				t.Errorf("err %v calls %d", err, len(f.calls))
			}
		})
	}
}

func manyInputs(n int) map[string]string {
	m := map[string]string{}
	for i := 0; i < n; i++ {
		m["arm:f"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".bicep"] = "{}"
	}
	return m
}

func TestRunFailures(t *testing.T) {
	ctx := context.Background()
	basic := fixture(t, "results-basic.sarif.json")
	tests := []struct {
		name string
		run  func(adapters.Command) (adapters.Result, error)
		cfg  func(*psrule.Config)
		want error
	}{
		{"timeout", func(adapters.Command) (adapters.Result, error) { return adapters.Result{}, adapters.ErrTimeout }, nil, adapters.ErrTimeout},
		{"stdout flood", func(adapters.Command) (adapters.Result, error) { return adapters.Result{}, adapters.ErrOutputTooLarge }, nil, adapters.ErrOutputTooLarge},
		{"non-zero exit", func(adapters.Command) (adapters.Result, error) { return adapters.Result{ExitCode: 3}, nil }, nil, adapters.ErrProtocol},
		{"no results file", writeResults(t, "", nil, nil), nil, adapters.ErrProtocol},
		{"malformed", writeResults(t, fixture(t, "results-malformed.sarif.json"), nil, nil), nil, adapters.ErrProtocol},
		{"wrong version", writeResults(t, fixture(t, "results-bad-version.sarif.json"), nil, nil), nil, adapters.ErrProtocol},
		{"hostile rule name", writeResults(t, fixture(t, "results-bad-rulename.sarif.json"), nil, nil), nil, adapters.ErrProtocol},
		{"oversize results file", writeResults(t, basic, nil, nil), func(c *psrule.Config) { c.MaxOutput = 100 }, adapters.ErrOutputTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{detect: detectOut(okDetect), run: tc.run}
			got, err := newAdapter(f, tc.cfg).Run(ctx, sdk.AdapterRequest{Options: armOptions(t)})
			if !errors.Is(err, tc.want) || got != nil {
				t.Errorf("err %v findings %v, want %v", err, got, tc.want)
			}
		})
	}
	t.Run("results path is a directory", func(t *testing.T) {
		f := &fakeRunner{detect: detectOut(okDetect), run: func(c adapters.Command) (adapters.Result, error) {
			return adapters.Result{}, os.Mkdir(filepath.Join(c.Dir, "out", "results.sarif"), 0o700)
		}}
		if _, err := newAdapter(f, nil).Run(ctx, sdk.AdapterRequest{Options: armOptions(t)}); !errors.Is(err, adapters.ErrProtocol) {
			t.Errorf("err %v", err)
		}
	})
}

// overlapIndex is a catalogue-shaped overlap index with the PSRule names of the fixtures.
func overlapIndex() engine.OverlapIndex {
	return engine.NewOverlapIndex([]catalog.Rule{
		{ID: "FND-SEC-005", Overlap: catalog.Overlap{PSRule: []string{"Azure.KeyVault.SoftDelete"}}},
		{ID: "FND-SEC-001", Overlap: catalog.Overlap{PSRule: []string{"Azure.AI.DisableLocalAuth"}}},
		{ID: "FND-SEC-002", Overlap: catalog.Overlap{PSRule: []string{"Azure.AI.DisableLocalAuth"}}},
	})
}

func TestDedupWithNative(t *testing.T) {
	f := &fakeRunner{detect: detectOut(okDetect), run: writeResults(t, fixture(t, "results-basic.sarif.json"), nil, nil)}
	idx := overlapIndex()
	adapterFindings, err := newAdapter(f, func(c *psrule.Config) { c.Mapper = idx }).Run(context.Background(), sdk.AdapterRequest{Options: armOptions(t)})
	if err != nil {
		t.Fatal(err)
	}
	native := []sdk.Finding{{
		RuleID: "FND-SEC-005", Fingerprint: "fp-native",
		Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "Microsoft.KeyVault/vaults", Name: "kv1"},
	}}
	kept, absorbed := engine.DedupAdapter(native, adapterFindings, idx)
	if len(absorbed) != 1 || absorbed[0].NativeRuleID != "FND-SEC-005" || absorbed[0].NativeFingerprint != "fp-native" {
		t.Fatalf("absorbed %+v", absorbed)
	}
	if absorbed[0].Adapter.Key != "Azure.KeyVault.SoftDelete" {
		t.Errorf("absorbed the wrong finding: %+v", absorbed[0].Adapter)
	}
	// The AI account findings have no native twin here and the storage finding is unmapped: they stay.
	var ids []string
	for _, k := range kept {
		ids = append(ids, k.RuleID)
	}
	want := []string{"FND-SEC-001", "FND-SEC-002", "psrule/Azure.Storage.SoftDelete"}
	if !slices.Equal(ids, want) {
		t.Errorf("kept %v, want %v", ids, want)
	}
	// With no native findings nothing is absorbed.
	if kept, absorbed := engine.DedupAdapter(nil, adapterFindings, idx); len(kept) != 4 || len(absorbed) != 0 {
		t.Errorf("kept %d absorbed %d", len(kept), len(absorbed))
	}
}
