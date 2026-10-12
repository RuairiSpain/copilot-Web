package bicep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBicepFakeProcess is re-executed by the fault-injection tests below as a
// stand-in for the Bicep CLI. It is a no-op in a normal test run.
func TestBicepFakeProcess(t *testing.T) {
	if os.Getenv("FD_BICEP_FAKE") != "1" {
		return
	}
	scenario := ""
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			scenario = os.Args[i+1]
		}
	}
	sarif := func(rule, level string) string {
		return fmt.Sprintf(`{"version":"2.1.0","runs":[{"results":[{"ruleId":%q,"level":%q,"message":{"text":"boom"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"main.bicep"},"region":{"startLine":3,"charOffset":4}}}]}]}]}`, rule, level)
	}
	switch scenario {
	case "ok":
		fmt.Fprint(os.Stdout, `{"resources":[]}`)
		os.Exit(0)
	case "error":
		fmt.Fprint(os.Stderr, sarif("BCP057", "error"))
		os.Exit(1)
	case "flood":
		fmt.Fprint(os.Stdout, strings.Repeat("O", 64<<10))
		fmt.Fprint(os.Stderr, strings.Repeat("E", 64<<10))
		os.Exit(0)
	case "terminal":
		fmt.Fprint(os.Stderr, "\x1b[31merror\x1b[0m\r\npassword=hunter2 token=abc123 Authorization: Bearer sekret")
		os.Exit(1)
	case "sleep":
		time.Sleep(30 * time.Second)
	case "environment":
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			fmt.Fprintln(os.Stdout, name)
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown fake scenario")
		os.Exit(2)
	}
}

// reexecRunner swaps the real bicep arguments for the re-exec arguments and
// delegates to a genuine ExecRunner, so process handling is really exercised.
type reexecRunner struct {
	inner    ExecRunner
	scenario string
}

func (f reexecRunner) Run(ctx context.Context, _ string, _ []string, dir string) (RunResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return RunResult{}, err
	}
	f.inner.ExtraEnv = append(append([]string{}, f.inner.ExtraEnv...), "FD_BICEP_FAKE=1")
	return f.inner.Run(ctx, exe, []string{"-test.run=^TestBicepFakeProcess$", "--", f.scenario}, dir)
}

func fakeCompiler(scenario string, r ExecRunner) Compiler {
	return Compiler{Tool: Tool{Path: "fake"}, Runner: reexecRunner{inner: r, scenario: scenario}, Root: "/proj"}
}

func TestExecRunnerFaultInjection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		scenario string
		runner   ExecRunner
		ctx      func() (context.Context, context.CancelFunc)
		check    func(t *testing.T, res Result, err error)
	}{
		{
			name: "success", scenario: "ok",
			check: func(t *testing.T, res Result, err error) {
				if err != nil || !res.OK {
					t.Fatalf("res=%+v err=%v", res, err)
				}
			},
		},
		{
			name: "compile error is a result, not an error", scenario: "error",
			check: func(t *testing.T, res Result, err error) {
				if err != nil || res.OK || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != "BCP057" {
					t.Fatalf("res=%+v err=%v", res, err)
				}
			},
		},
		{
			name: "timeout is a dependency error", scenario: "sleep",
			runner: ExecRunner{Timeout: 300 * time.Millisecond},
			check: func(t *testing.T, _ Result, err error) {
				var te *ToolError
				if !errors.As(err, &te) || te.Kind != KindTimeout {
					t.Fatalf("want timeout ToolError, got %v", err)
				}
			},
		},
		{
			name: "caller cancellation is not a tool error", scenario: "sleep",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 300*time.Millisecond)
			},
			check: func(t *testing.T, _ Result, err error) {
				var te *ToolError
				if err == nil || errors.As(err, &te) {
					t.Fatalf("want plain context error, got %v", err)
				}
			},
		},
		{
			name: "flood is bounded and rejected", scenario: "flood",
			runner: ExecRunner{MaxOutput: 1024},
			check: func(t *testing.T, res Result, err error) {
				var te *ToolError
				if !errors.As(err, &te) || te.Kind != KindAdapter || !res.Truncated || res.OK {
					t.Fatalf("res=%+v err=%v", res, err)
				}
				if len(res.ARM) != 0 {
					t.Fatalf("truncated ARM must not be returned: %d bytes", len(res.ARM))
				}
			},
		},
		{
			name: "terminal escapes and secrets never reach the error", scenario: "terminal",
			check: func(t *testing.T, _ Result, err error) {
				if err == nil {
					t.Fatal("expected error for unparsable failure output")
				}
				for _, bad := range []string{"\x1b", "hunter2", "abc123", "sekret", "\r"} {
					if strings.Contains(err.Error(), bad) {
						t.Fatalf("error leaks %q: %q", bad, err.Error())
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			if tt.ctx != nil {
				ctx, cancel = tt.ctx()
			}
			defer cancel()
			if tt.runner.Timeout == 0 {
				tt.runner.Timeout = 20 * time.Second
			}
			dir := t.TempDir()
			res, err := fakeCompiler(tt.scenario, tt.runner).Compile(ctx, dir+string(os.PathSeparator)+"main.bicep")
			tt.check(t, res, err)
		})
	}
}

func TestExecRunnerRawOutputBoundsAndSanitisation(t *testing.T) {
	t.Parallel()
	got, err := reexecRunner{inner: ExecRunner{MaxOutput: 512, Timeout: 20 * time.Second}, scenario: "flood"}.
		Run(context.Background(), "", nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Stdout) != 512 || len(got.Stderr) != 512 {
		t.Fatalf("truncated=%v stdout=%d stderr=%d", got.Truncated, len(got.Stdout), len(got.Stderr))
	}
	term, err := reexecRunner{inner: ExecRunner{Timeout: 20 * time.Second}, scenario: "terminal"}.
		Run(context.Background(), "", nil, t.TempDir())
	if err != nil || term.ExitCode != 1 {
		t.Fatalf("exit=%d err=%v", term.ExitCode, err)
	}
	s := sanitize(string(term.Stderr), 200)
	for _, bad := range []string{"\x1b", "hunter2", "abc123", "sekret", "\r", "\n"} {
		if strings.Contains(s, bad) {
			t.Fatalf("sanitize leaks %q: %q", bad, s)
		}
	}
	if !strings.Contains(s, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %q", s)
	}
}

func TestExecRunnerEnvironmentAllowList(t *testing.T) {
	t.Parallel()
	secrets := map[string]string{
		"AZURE_CLIENT_SECRET": "x", "AZURE_TENANT_ID": "x", "HTTPS_PROXY": "x",
		"GITHUB_TOKEN": "x", "OTEL_EXPORTER_OTLP_ENDPOINT": "x", "PATH": os.Getenv("PATH"),
	}
	r := ExecRunner{Timeout: 20 * time.Second, Getenv: func(k string) string { return secrets[k] }}
	res, err := reexecRunner{inner: r, scenario: "environment"}.Run(context.Background(), "", nil, t.TempDir())
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("exit=%d err=%v", res.ExitCode, err)
	}
	out := string(res.Stdout)
	for name := range secrets {
		if name == "PATH" {
			continue
		}
		if strings.Contains(out, name+"\n") || strings.Contains(out, name+"\r\n") {
			t.Fatalf("%s leaked into child environment:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "DOTNET_CLI_TELEMETRY_OPTOUT") || !strings.Contains(out, "FD_BICEP_FAKE") {
		t.Fatalf("expected allow-listed/extra variables:\n%s", out)
	}
}
