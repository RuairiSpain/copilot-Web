// Package adapters holds the helpers shared by the wrappers around optional external tools
// (ADR-001): the enable mode, process execution behind a consumer-defined interface, the
// restricted subprocess environment, bounded output, and the adapter-unavailable data the app
// layer turns into an FND-SYS-ADAPTER-UNAVAILABLE diagnostic (ADR-009).
//
// Adapter output is untrusted input. Nothing here interprets it; each adapter validates it strictly.
package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Errors returned by adapters. The app maps ErrDisabled and ErrUnavailable by Mode (exit 2 only when the
// adapter was explicitly enabled) and every other error to exit 4 (adapter protocol or crash failure).
var (
	ErrDisabled       = errors.New("adapter is disabled")
	ErrUnavailable    = errors.New("adapter is unavailable")
	ErrTimeout        = errors.New("adapter timed out")
	ErrOutputTooLarge = errors.New("adapter output exceeds the size limit")
	ErrProtocol       = errors.New("adapter produced invalid output")
)

// Mode is the tri-state enable switch (advanced.<tool>.enabled: auto, true or false).
type Mode string

// Mode values.
const (
	ModeAuto  Mode = "auto"
	ModeTrue  Mode = "true"
	ModeFalse Mode = "false"
)

// ParseMode converts a configuration value to a Mode. The empty string is auto.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return ModeAuto, nil
	case ModeAuto, ModeTrue, ModeFalse:
		return m, nil
	}
	return "", fmt.Errorf("unknown adapter mode %q: want auto, true or false", s)
}

// Command is one process invocation. Args are passed directly, never through a shell.
type Command struct {
	Path      string        // absolute path of the executable
	Args      []string      //
	Dir       string        // working directory
	Env       []string      // the complete environment; the parent environment is never inherited
	Timeout   time.Duration // zero means no limit beyond ctx
	MaxOutput int64         // limit per stream in bytes; zero means DefaultMaxOutput
}

// Result is the outcome of a process that ran to completion. A non-zero ExitCode is not an error here.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes a Command. It is defined here, where adapters consume it; tests inject a fake.
// Implementations return ErrTimeout when Timeout elapsed and ErrOutputTooLarge when a stream
// exceeded MaxOutput.
type Runner interface {
	Run(ctx context.Context, c Command) (Result, error)
}

// DefaultMaxOutput bounds each captured stream and each output file.
const DefaultMaxOutput int64 = 8 << 20

// ExecRunner runs real processes with os/exec.
type ExecRunner struct{}

// Run implements Runner. The child gets no stdin and only c.Env; it is killed on timeout or overflow.
func (ExecRunner) Run(ctx context.Context, c Command) (Result, error) {
	if c.Path == "" {
		return Result{}, errors.New("adapters: empty command path")
	}
	limit := c.MaxOutput
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	parent := ctx
	if c.Timeout > 0 {
		var cancelT context.CancelFunc
		ctx, cancelT = context.WithTimeout(ctx, c.Timeout)
		defer cancelT()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Path, c.Args...) //nolint:gosec // fixed arguments built by the adapter, no shell
	cmd.Dir = c.Dir
	cmd.Env = append([]string{}, c.Env...) // non-nil: an empty slice means an empty environment
	cmd.WaitDelay = 2 * time.Second
	out, errb := &capped{max: limit, cancel: cancel}, &capped{max: limit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, errb
	err := cmd.Run()
	switch {
	case out.over.Load() || errb.over.Load():
		return Result{}, ErrOutputTooLarge
	case parent.Err() != nil:
		return Result{}, parent.Err()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Result{}, ErrTimeout
	}
	res := Result{Stdout: out.buf.Bytes(), Stderr: errb.buf.Bytes()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
	default:
		return Result{}, fmt.Errorf("run %s: %w", baseName(c.Path), err)
	}
	return res, nil
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// capped buffers up to max bytes. On overflow it records the fact, cancels the process and discards the rest.
type capped struct {
	buf    bytes.Buffer
	max    int64
	over   atomic.Bool
	cancel context.CancelFunc
}

func (c *capped) Write(p []byte) (int, error) {
	if c.over.Load() {
		return len(p), nil
	}
	if int64(c.buf.Len()+len(p)) > c.max {
		c.over.Store(true)
		c.cancel()
		return len(p), nil
	}
	return c.buf.Write(p)
}

// ReadFileLimited reads a file the adapter's tool wrote, refusing more than max bytes (ErrOutputTooLarge).
func ReadFileLimited(path string, max int64) ([]byte, error) {
	if max <= 0 {
		max = DefaultMaxOutput
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrOutputTooLarge
	}
	return b, nil
}

// RestrictedEnv builds a subprocess environment from environ ("NAME=value" entries): only the
// allow-listed names (compared upper-case) are copied, then set overrides or adds entries. Nothing else
// is inherited, so Azure credentials, tokens and CI variables never reach an adapter. The result is sorted.
func RestrictedEnv(environ, allow []string, set map[string]string) []string {
	ok := map[string]bool{}
	for _, a := range allow {
		ok[strings.ToUpper(a)] = true
	}
	kv := map[string]string{}
	for _, e := range environ {
		name, val, found := strings.Cut(e, "=")
		if !found || name == "" || !ok[strings.ToUpper(name)] {
			continue
		}
		kv[name] = val
	}
	for k, v := range set {
		for existing := range kv {
			if strings.EqualFold(existing, k) {
				delete(kv, existing)
			}
		}
		kv[k] = v
	}
	out := make([]string, 0, len(kv))
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// Unavailability is the data behind an FND-SYS-ADAPTER-UNAVAILABLE diagnostic (ADR-009).
type Unavailability struct {
	Adapter string
	State   sdk.ToolState
	Reason  string
	// Fatal is true when the adapter was explicitly enabled (Mode true) but cannot run: exit 2.
	// For auto and false it is false: report the diagnostic, skip dependent checks, continue.
	Fatal bool
}

// Evidence is the diagnostic text. It contains no path, only the state and reason.
func (u Unavailability) Evidence() string {
	return fmt.Sprintf("adapter unavailable: %s (%s): %s", u.Adapter, u.State, u.Reason)
}

// CheckAvailability returns nil when st is available, otherwise the unavailability for mode.
func CheckAvailability(adapter string, st sdk.ToolStatus, mode Mode) *Unavailability {
	if st.State == sdk.ToolAvailable {
		return nil
	}
	reason := st.Detail
	if reason == "" {
		reason = string(st.State)
	}
	return &Unavailability{
		Adapter: adapter, State: st.State, Reason: reason,
		Fatal: mode == ModeTrue && st.State != sdk.ToolDisabled,
	}
}

// Version is a parsed numeric version (major.minor.patch); pre-release text is ignored for ordering.
type Version [3]int

// ParseVersion parses "7.4.6", "7.5.0-preview.1" or "1.47.0.1"; a fourth numeric part is dropped.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		if p == "" || len(p) > 6 {
			return Version{}, false
		}
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return Version{}, false
			}
			n = n*10 + int(r-'0')
		}
		if i < 3 {
			v[i] = n
		}
	}
	return v, true
}

// Less reports whether v is older than w.
func (v Version) Less(w Version) bool {
	for i := range v {
		if v[i] != w[i] {
			return v[i] < w[i]
		}
	}
	return false
}
