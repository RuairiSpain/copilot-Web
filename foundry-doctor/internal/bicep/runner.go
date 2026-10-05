package bicep

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Default limits for one compiler run.
const (
	DefaultTimeout   = 60 * time.Second
	DefaultMaxStdout = 32 << 20 // compiled ARM of large projects is a few MB
	DefaultMaxStderr = 4 << 20
)

// RunSpec describes one execution of the Bicep CLI.
type RunSpec struct {
	Path      string   // absolute path of the executable
	Args      []string // arguments after the executable
	Dir       string   // working directory
	Env       []string // complete environment; nothing is inherited
	Timeout   time.Duration
	MaxStdout int
	MaxStderr int
}

// RunResult is the captured outcome of a run. ExitCode is -1 when the process did not exit normally.
type RunResult struct {
	Stdout, Stderr []byte
	ExitCode       int
	// Truncated is set when stdout or stderr exceeded its limit; the process is then killed.
	Truncated bool
}

// Runner executes the CLI. It is the seam used to fake the compiler in tests.
type Runner interface {
	Run(ctx context.Context, spec RunSpec) (RunResult, error)
}

// ErrTimeout is returned when the compiler exceeds its time limit.
var ErrTimeout = errors.New("bicep: timed out")

// ErrOutputTooLarge is returned when the compiler output exceeds the configured limit.
var ErrOutputTooLarge = errors.New("bicep: output exceeds the size limit")

// ExecRunner runs the CLI as a child process.
type ExecRunner struct{}

// limitBuffer keeps at most max bytes and reports overflow without failing the writer, so the
// child never sees a broken pipe; the caller kills it on overflow.
type limitBuffer struct {
	buf      bytes.Buffer
	max      int
	over     bool
	onExceed func()
}

func (l *limitBuffer) Write(p []byte) (int, error) {
	room := l.max - l.buf.Len()
	if len(p) > room {
		if room > 0 {
			l.buf.Write(p[:room])
		}
		if !l.over {
			l.over = true
			if l.onExceed != nil {
				l.onExceed()
			}
		}
		return len(p), nil
	}
	return l.buf.Write(p)
}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, spec RunSpec) (RunResult, error) {
	if spec.Path == "" {
		return RunResult{ExitCode: -1}, errors.New("run bicep: empty executable path")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxOut, maxErr := spec.MaxStdout, spec.MaxStderr
	if maxOut <= 0 {
		maxOut = DefaultMaxStdout
	}
	if maxErr <= 0 {
		maxErr = DefaultMaxStderr
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if cmd.Env == nil {
		cmd.Env = []string{} // never inherit the parent environment
	}
	cmd.WaitDelay = 2 * time.Second
	out := &limitBuffer{max: maxOut, onExceed: cancel}
	errb := &limitBuffer{max: maxErr, onExceed: cancel}
	cmd.Stdout, cmd.Stderr = out, errb

	err := cmd.Run()
	res := RunResult{Stdout: out.buf.Bytes(), Stderr: errb.buf.Bytes(), ExitCode: -1, Truncated: out.over || errb.over}
	if res.Truncated {
		return res, fmt.Errorf("run bicep %v: %w", spec.Args, ErrOutputTooLarge)
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return res, nil
	}
	var ee *exec.ExitError
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return res, fmt.Errorf("run bicep %v after %s: %w", spec.Args, timeout, ErrTimeout)
	case ctx.Err() != nil:
		return res, fmt.Errorf("run bicep %v: %w", spec.Args, ctx.Err())
	case errors.As(err, &ee):
		return res, nil // a non-zero exit is a result, not an error
	}
	return res, fmt.Errorf("run bicep %v: %w", spec.Args, err)
}

// MinimalEnv returns the allow-listed environment for the compiler. It carries no AZURE_*
// variable, no token and nothing from the user's environment, and it points HOME at the given
// directory so the compiler never reads or writes the real home.
func MinimalEnv(home string) []string {
	env := []string{
		"HOME=" + home,
		"DOTNET_CLI_HOME=" + home,
		"DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1",
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_NOLOGO=1",
		"TMPDIR=" + home,
	}
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+home, "TEMP="+home, "TMP="+home)
		if sr := os.Getenv("SystemRoot"); sr != "" { // the .NET runtime needs it on Windows
			env = append(env, "SystemRoot="+sr)
		}
	} else {
		env = append(env, "PATH=/usr/bin:/bin")
	}
	return env
}
