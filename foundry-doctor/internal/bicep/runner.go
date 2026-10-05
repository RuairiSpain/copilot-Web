package bicep

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultTimeout   = 60 * time.Second
	defaultMaxOutput = 32 << 20
)

// RunResult is the captured outcome of one process run.
type RunResult struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Truncated bool
}

// Runner executes the Bicep CLI. A non-nil error means the process could not be
// started or was cancelled/timed out; a non-zero exit code is NOT an error.
type Runner interface {
	Run(ctx context.Context, exe string, args []string, dir string) (RunResult, error)
}

// ExecRunner is the production Runner. It passes a minimal allow-listed
// environment so credentials, proxies and tracing settings are not inherited.
type ExecRunner struct {
	Timeout   time.Duration
	MaxOutput int
	// ExtraEnv entries (NAME=value) are appended after the allow-listed set.
	ExtraEnv []string
	// Getenv overrides os.Getenv (tests).
	Getenv func(string) string
}

type boundedBuffer struct {
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func (w *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > w.remaining {
		p = p[:w.remaining]
		w.truncated = true
	}
	if len(p) > 0 {
		w.buf.Write(p)
		w.remaining -= len(p)
	}
	return n, nil
}

var envAllow = []string{
	"PATH", "HOME", "USERPROFILE", "TMP", "TEMP", "TMPDIR", "SystemRoot",
	"DOTNET_BUNDLE_EXTRACT_BASE_DIR", "DOTNET_CLI_TELEMETRY_OPTOUT",
}

func (r ExecRunner) env() []string {
	get := r.Getenv
	if get == nil {
		get = os.Getenv
	}
	env := []string{"DOTNET_CLI_TELEMETRY_OPTOUT=1", "BICEP_TELEMETRY_OPTOUT=1"}
	for _, n := range envAllow {
		if n == "SystemRoot" && runtime.GOOS != "windows" {
			continue
		}
		if v := get(n); v != "" {
			env = append(env, n+"="+v)
		}
	}
	return append(env, r.ExtraEnv...)
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, exe string, args []string, dir string) (RunResult, error) {
	timeout, limit := r.Timeout, r.MaxOutput
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if limit <= 0 {
		limit = defaultMaxOutput
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, args...)
	cmd.Dir = dir
	cmd.Env = r.env()
	out := &boundedBuffer{remaining: limit}
	errb := &boundedBuffer{remaining: limit}
	cmd.Stdout, cmd.Stderr = out, errb
	err := cmd.Run()
	res := RunResult{
		Stdout:    append([]byte(nil), out.buf.Bytes()...),
		Stderr:    append([]byte(nil), errb.buf.Bytes()...),
		Truncated: out.truncated || errb.truncated,
	}
	if err == nil {
		return res, nil
	}
	if ctx.Err() != nil { // caller cancelled or its own deadline expired
		return res, ctx.Err()
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return res, toolErr(KindTimeout, "bicep CLI timed out", err)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		if res.ExitCode < 0 { // killed by signal
			res.ExitCode = 1
		}
		return res, nil
	}
	return res, fmt.Errorf("start bicep CLI: %w", err)
}

var (
	ansiRE   = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")
	secretRE = regexp.MustCompile(`(?i)((?:password|passwd|pwd|secret|token|apikey|api[-_]key|key|authorization|sig|sas)\s*[=:]\s*)(?:bearer\s+)?[^\s,;"']+`)
)

// sanitize strips control sequences, redacts secret-looking assignments and
// bounds the length of text derived from compiler output.
func sanitize(s string, max int) string {
	s = ansiRE.ReplaceAllString(s, "")
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = secretRE.ReplaceAllString(s, "${1}[REDACTED]")
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		s = strings.ToValidUTF8(s[:max], "") + "..."
	}
	return s
}
