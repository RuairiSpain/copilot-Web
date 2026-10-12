package adapters_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// TestHelperProcess is re-executed by the ExecRunner tests; it is not a test by itself.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("FD_HELPER_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "env":
		for _, e := range os.Environ() {
			fmt.Println(e)
		}
	case "sleep":
		time.Sleep(30 * time.Second)
	case "flood":
		for i := 0; i < 100000; i++ {
			fmt.Println(strings.Repeat("x", 100))
		}
	case "exit7":
		fmt.Fprint(os.Stderr, "boom")
		os.Exit(7)
	}
	os.Exit(0)
}

func helperCmd(mode string, extra ...string) adapters.Command {
	return adapters.Command{
		Path: os.Args[0],
		Args: []string{"-test.run=^TestHelperProcess$"},
		Dir:  os.TempDir(),
		Env:  append([]string{"FD_HELPER_MODE=" + mode}, extra...),
	}
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]adapters.Mode{"": adapters.ModeAuto, "auto": adapters.ModeAuto, " TRUE ": adapters.ModeTrue, "false": adapters.ModeFalse} {
		got, err := adapters.ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := adapters.ParseMode("yes"); err == nil {
		t.Error("ParseMode(yes) accepted")
	}
}

func TestRestrictedEnv(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin", "path_dup_ignored", "AZURE_CLIENT_SECRET=s", "GITHUB_TOKEN=t", "HOME=/home/u", "=novalue",
		"Lang=C", "TMPDIR=/should/be/overridden",
	}
	got := adapters.RestrictedEnv(environ, []string{"PATH", "HOME", "LANG", "TMPDIR"}, map[string]string{"TMPDIR": "/work", "NO_COLOR": "1"})
	want := []string{"HOME=/home/u", "Lang=C", "NO_COLOR=1", "PATH=/usr/bin", "TMPDIR=/work"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("RestrictedEnv = %v, want %v", got, want)
	}
	if got := adapters.RestrictedEnv(nil, nil, nil); len(got) != 0 {
		t.Errorf("empty input gave %v", got)
	}
}

func TestCheckAvailability(t *testing.T) {
	tests := []struct {
		name  string
		st    sdk.ToolStatus
		mode  adapters.Mode
		nilOK bool
		fatal bool
	}{
		{"available", sdk.ToolStatus{State: sdk.ToolAvailable}, adapters.ModeTrue, true, false},
		{"missing auto", sdk.ToolStatus{State: sdk.ToolMissing, Detail: "pwsh not found"}, adapters.ModeAuto, false, false},
		{"missing true is fatal", sdk.ToolStatus{State: sdk.ToolMissing}, adapters.ModeTrue, false, true},
		{"unsupported true is fatal", sdk.ToolStatus{State: sdk.ToolUnsupported}, adapters.ModeTrue, false, true},
		{"disabled is never fatal", sdk.ToolStatus{State: sdk.ToolDisabled}, adapters.ModeFalse, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := adapters.CheckAvailability("psrule", tc.st, tc.mode)
			if tc.nilOK {
				if u != nil {
					t.Fatalf("got %+v, want nil", u)
				}
				return
			}
			if u == nil || u.Fatal != tc.fatal || u.Adapter != "psrule" || u.State != tc.st.State {
				t.Fatalf("got %+v", u)
			}
			if !strings.HasPrefix(u.Evidence(), "adapter unavailable: psrule (") {
				t.Errorf("evidence %q", u.Evidence())
			}
			if tc.st.Detail == "" && u.Reason != string(tc.st.State) {
				t.Errorf("reason %q should default to the state", u.Reason)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	good := map[string]adapters.Version{"7.4.6": {7, 4, 6}, "v1.47.0": {1, 47, 0}, "7.5.0-preview.1": {7, 5, 0}, "1.47.0.12": {1, 47, 0}, "7.4": {7, 4, 0}}
	for in, want := range good {
		got, ok := adapters.ParseVersion(in)
		if !ok || got != want {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "7", "a.b.c", "7..1", "1.2.3.4.5", "7.4.x", "1234567.1.1"} {
		if _, ok := adapters.ParseVersion(in); ok {
			t.Errorf("ParseVersion(%q) accepted", in)
		}
	}
	a, _ := adapters.ParseVersion("7.4.0")
	b, _ := adapters.ParseVersion("7.10.0")
	if !a.Less(b) || b.Less(a) || a.Less(a) {
		t.Error("Less ordering is wrong")
	}
}

func TestReadFileLimited(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := adapters.ReadFileLimited(p, 5); err != nil || string(b) != "12345" {
		t.Errorf("at limit: %q, %v", b, err)
	}
	if _, err := adapters.ReadFileLimited(p, 4); !errors.Is(err, adapters.ErrOutputTooLarge) {
		t.Errorf("over limit: %v", err)
	}
	if _, err := adapters.ReadFileLimited(p+".missing", 0); err == nil {
		t.Error("missing file accepted")
	}
}

func TestExecRunner(t *testing.T) {
	t.Setenv("FD_PARENT_SECRET", "must-not-leak")
	r := adapters.ExecRunner{}
	ctx := context.Background()

	t.Run("environment is not inherited", func(t *testing.T) {
		res, err := r.Run(ctx, helperCmd("env", "ONLY_THIS=1"))
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("err %v exit %d", err, res.ExitCode)
		}
		out := string(res.Stdout)
		if strings.Contains(out, "must-not-leak") || !strings.Contains(out, "ONLY_THIS=1") {
			t.Errorf("environment leaked or missing: %q", out)
		}
	})
	t.Run("non-zero exit is a result", func(t *testing.T) {
		res, err := r.Run(ctx, helperCmd("exit7"))
		if err != nil || res.ExitCode != 7 || !strings.Contains(string(res.Stderr), "boom") {
			t.Errorf("got %+v, %v", res, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		c := helperCmd("sleep")
		c.Timeout = 300 * time.Millisecond
		if _, err := r.Run(ctx, c); !errors.Is(err, adapters.ErrTimeout) {
			t.Errorf("got %v, want ErrTimeout", err)
		}
	})
	t.Run("parent cancellation", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		time.AfterFunc(200*time.Millisecond, cancel)
		if _, err := r.Run(cctx, helperCmd("sleep")); !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	})
	t.Run("oversize output", func(t *testing.T) {
		c := helperCmd("flood")
		c.MaxOutput = 1024
		if _, err := r.Run(ctx, c); !errors.Is(err, adapters.ErrOutputTooLarge) {
			t.Errorf("got %v, want ErrOutputTooLarge", err)
		}
	})
	t.Run("cannot start", func(t *testing.T) {
		_, err := r.Run(ctx, adapters.Command{Path: filepath.Join(t.TempDir(), "nope")})
		if err == nil || errors.Is(err, adapters.ErrTimeout) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("empty path", func(t *testing.T) {
		if _, err := r.Run(ctx, adapters.Command{}); err == nil {
			t.Error("empty path accepted")
		}
	})
}
