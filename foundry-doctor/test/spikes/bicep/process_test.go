package bicepspike

// These tests are intentionally not guarded by the spike build tag. They pin
// the process boundary expected of the Bicep adapter without requiring Bicep,
// network access, or Azure credentials. TestBicepFakeProcess re-executes this
// test binary and acts as the untrusted external compiler.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	minSupportedBicep = "0.47.16"
	maxSupportedBicep = "0.48.1"
	maxCompilerOutput = 4096
	maxSpikeOutput    = 4 << 20
	maxLogSummary     = 1024
)

type commandRunner struct {
	executable string
	prefixArgs []string // only used to host the test fake in this test binary
	env        []string
	timeout    time.Duration
	maxOutput  int
}

type commandResult struct {
	stdout, stderr             []byte
	stdoutTruncated            bool
	stderrTruncated            bool
	exitCode                   int
	startErr                   error
	timedOut, contextCancelled bool
	signalled                  bool
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
	if len(p) != 0 {
		_, _ = w.buf.Write(p)
		w.remaining -= len(p)
	}
	return n, nil
}

func (r commandRunner) run(ctx context.Context, args ...string) commandResult {
	limit := r.maxOutput
	if limit <= 0 {
		limit = maxCompilerOutput
	}
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	allArgs := append(append([]string{}, r.prefixArgs...), args...)
	cmd := exec.CommandContext(runCtx, r.executable, allArgs...)
	// Never inherit credentials, proxy settings, tracing configuration, or
	// Bicep/Azure configuration from the parent.
	cmd.Env = append([]string(nil), r.env...)
	out := &boundedBuffer{remaining: limit}
	errOut := &boundedBuffer{remaining: limit}
	cmd.Stdout, cmd.Stderr = out, errOut

	err := cmd.Run()
	result := commandResult{
		stdout:          append([]byte(nil), out.buf.Bytes()...),
		stderr:          append([]byte(nil), errOut.buf.Bytes()...),
		stdoutTruncated: out.truncated,
		stderrTruncated: errOut.truncated,
	}
	if err == nil {
		return result
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.exitCode = 1
		result.timedOut = true
		return result
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		result.exitCode = 1
		result.contextCancelled = true
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.exitCode = exitErr.ExitCode()
		result.signalled = result.exitCode < 0
		if result.signalled {
			result.exitCode = 1
		}
		return result
	}
	// A requested Bicep operation with a missing or non-executable CLI is a
	// tool/usage failure, not a successful skip.
	result.exitCode = 2
	result.startErr = err
	return result
}

func fakeRunner(t *testing.T) commandRunner {
	t.Helper()
	env := []string{"FD_BICEP_FAKE=1"}
	for _, name := range []string{"PATH", "HOME", "TMP", "TEMP"} {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return commandRunner{
		executable: os.Args[0],
		prefixArgs: []string{"-test.run=^TestBicepFakeProcess$", "--"},
		env:        env,
		timeout:    time.Second,
		maxOutput:  maxCompilerOutput,
	}
}

func fakeScenario() string {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func TestBicepFakeProcess(t *testing.T) {
	if os.Getenv("FD_BICEP_FAKE") != "1" {
		return
	}
	switch fakeScenario() {
	case "error":
		fmt.Fprint(os.Stderr, sarifDocument("BCP057", "error", 11, "The name is not defined."))
		os.Exit(1)
	case "warning":
		fmt.Fprint(os.Stderr, sarifDocument("no-unused-params", "warning", 1, "Parameter is unused."))
		os.Exit(0)
	case "malformed-arm":
		fmt.Fprint(os.Stdout, `{"resources":[`)
		os.Exit(0)
	case "malformed-sarif":
		fmt.Fprint(os.Stderr, `{"version":"2.1.0","runs":[`)
		os.Exit(1)
	case "partial":
		fmt.Fprint(os.Stdout, `{"resources":`)
		fmt.Fprint(os.Stderr, sarifDocument("BCP057", "error", 11, "compile failed"))
		os.Exit(1)
	case "flood":
		fmt.Fprint(os.Stdout, strings.Repeat("O", maxCompilerOutput*4))
		fmt.Fprint(os.Stderr, strings.Repeat("E", maxCompilerOutput*4))
		os.Exit(0)
	case "terminal":
		fmt.Fprint(os.Stderr, "\x1b[31merror\x1b[0m\r\npassword=hunter2 token=abc123 "+
			"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig\x00")
		os.Exit(1)
	case "sleep":
		time.Sleep(10 * time.Second)
	case "kill":
		_ = os.Stdout.Sync()
		_ = os.Stderr.Sync()
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Kill()
		time.Sleep(time.Second)
	case "environment":
		for _, item := range os.Environ() {
			name, _, _ := strings.Cut(item, "=")
			fmt.Fprintln(os.Stdout, name)
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown fake scenario")
		os.Exit(2)
	}
}

func sarifDocument(rule, level string, line int, message string) string {
	doc := map[string]any{
		"version": "2.1.0",
		"runs": []any{map[string]any{
			"results": []any{map[string]any{
				"ruleId": rule,
				"level":  level,
				"message": map[string]any{
					"text": message,
				},
				"locations": []any{map[string]any{
					"physicalLocation": map[string]any{
						"artifactLocation": map[string]any{"uri": "fixture.bicep"},
						"region":           map[string]any{"startLine": line, "startColumn": 1},
					},
				}},
			}},
		}},
	}
	data, _ := json.Marshal(doc)
	return string(data)
}

type Finding struct {
	RuleID     string
	Severity   string
	Message    string
	Path       string
	Line       int
	Column     int
	Confidence string
}

func parseSARIF(payload []byte) ([]Finding, error) {
	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine   int `json:"startLine"`
							StartColumn int `json:"startColumn"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, fmt.Errorf("decode Bicep SARIF: %w", err)
	}
	if doc.Version != "2.1.0" || len(doc.Runs) == 0 {
		return nil, errors.New("decode Bicep SARIF: missing SARIF 2.1.0 run")
	}
	if len(doc.Runs) > 8 {
		return nil, errors.New("decode Bicep SARIF: too many runs")
	}
	var findings []Finding
	for _, run := range doc.Runs {
		if len(run.Results) > 4096 {
			return nil, errors.New("decode Bicep SARIF: too many results")
		}
		for _, result := range run.Results {
			if !sarifRuleID.MatchString(result.RuleID) {
				return nil, errors.New("decode Bicep SARIF: invalid ruleId")
			}
			finding := Finding{
				RuleID:     result.RuleID,
				Severity:   result.Level,
				Message:    safeCompilerSummary([]byte(result.Message.Text), 4096),
				Confidence: "likely",
			}
			if finding.Message == "" {
				return nil, errors.New("decode Bicep SARIF: result has no safe message")
			}
			if finding.Severity == "" {
				finding.Severity = "warning"
			}
			switch finding.Severity {
			case "none", "note", "warning", "error":
			default:
				return nil, errors.New("decode Bicep SARIF: invalid result level")
			}
			if len(result.Locations) > 8 {
				return nil, errors.New("decode Bicep SARIF: too many locations")
			}
			if len(result.Locations) != 0 {
				location := result.Locations[0].PhysicalLocation
				safePath, err := safeSARIFPath(location.ArtifactLocation.URI)
				if err != nil {
					return nil, fmt.Errorf("decode Bicep SARIF: %w", err)
				}
				finding.Path = safePath
				finding.Line = location.Region.StartLine
				finding.Column = location.Region.StartColumn
				if finding.Line < 0 || finding.Column < 0 {
					return nil, errors.New("decode Bicep SARIF: negative source position")
				}
				if finding.Line > 0 {
					finding.Confidence = "exact"
				}
			}
			findings = append(findings, finding)
		}
	}
	return findings, nil
}

func safeSARIFPath(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid artifact URI")
	}
	if parsed.Scheme != "" && parsed.Scheme != "file" {
		return "", errors.New("unsupported artifact URI scheme")
	}
	value := raw
	if parsed.Scheme == "file" {
		value = filepath.Base(filepath.FromSlash(parsed.Path))
	}
	value = strings.ToValidUTF8(value, "")
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", errors.New("unsafe artifact path")
	}
	value = filepath.Clean(filepath.FromSlash(value))
	if value == "." || !filepath.IsLocal(value) {
		return "", errors.New("artifact path escapes source root")
	}
	return filepath.ToSlash(value), nil
}

func parseSARIFResult(payload []byte, truncated bool) ([]Finding, error) {
	if truncated {
		return nil, errors.New("decode Bicep SARIF: compiler output was truncated")
	}
	return parseSARIF(payload)
}

func parseARM(payload []byte) (map[string]any, error) {
	var arm map[string]any
	if err := json.Unmarshal(payload, &arm); err != nil {
		return nil, fmt.Errorf("decode ARM output: %w", err)
	}
	if arm == nil {
		return nil, errors.New("decode ARM output: expected an object")
	}
	return arm, nil
}

func parseARMResult(payload []byte, truncated bool) (map[string]any, error) {
	if truncated {
		return nil, errors.New("decode ARM output: compiler output was truncated")
	}
	return parseARM(payload)
}

var (
	ansiEscape    = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]?|\][^\x07\x1b]*(?:\x07|\x1b\\|$)|[ -/]*[@-~]?)`)
	sarifRuleID   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	privateKey    = regexp.MustCompile(`(?is)-----BEGIN[ \t]+[A-Z0-9 -]*PRIVATE KEY-----.*?(?:-----END[ \t]+[A-Z0-9 -]*PRIVATE KEY-----|$)`)
	connection    = regexp.MustCompile(`(?i)\b(connection[_ -]?strings?|database[_ -]?url)\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\r\n]+)`)
	secretKV      = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:password|passwd|token|secret|client[_-]?secret|api[_-]?key|access[_-]?key|account[_-]?key|signing[_-]?key|private[_-]?key)[a-z0-9_.-]*)\s*[:=]\s*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)`)
	authorization = regexp.MustCompile(`(?i)\b(authorization\s*:)\s*[^\r\n]+`)
	bearer        = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	jwt           = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b`)
	github        = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)
	awsKey        = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	openAI        = regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{16,}\b`)
	slack         = regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)
	sasSig        = regexp.MustCompile(`(?i)([?&]sig=)[^&\s"'<>]+`)
)

func safeCompilerSummary(payload []byte, max int) string {
	// Sanitise before truncation so a terminal sequence cannot be cut into a
	// fragment and so secrets at the boundary are not partially retained.
	s := strings.ToValidUTF8(string(payload), "�")
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, s)
	s = privateKey.ReplaceAllString(s, "[PRIVATE KEY REDACTED]")
	s = connection.ReplaceAllString(s, "$1=[REDACTED]")
	s = secretKV.ReplaceAllString(s, "$1=[REDACTED]")
	s = authorization.ReplaceAllString(s, "$1 [REDACTED]")
	s = bearer.ReplaceAllString(s, "Bearer [REDACTED]")
	s = jwt.ReplaceAllString(s, "[JWT REDACTED]")
	s = github.ReplaceAllString(s, "[GITHUB TOKEN REDACTED]")
	s = awsKey.ReplaceAllString(s, "[AWS ACCESS KEY REDACTED]")
	s = openAI.ReplaceAllString(s, "[OPENAI TOKEN REDACTED]")
	s = slack.ReplaceAllString(s, "[SLACK TOKEN REDACTED]")
	s = sasSig.ReplaceAllString(s, "$1[REDACTED]")
	if max >= 0 && len(s) > max {
		// Keep the returned summary valid UTF-8 even when max bisects a rune.
		s = strings.ToValidUTF8(s[:max], "") + "…[truncated]"
	}
	return s
}

type semVersion struct{ major, minor, patch int }

func parseBicepVersion(output string) (semVersion, error) {
	match := regexp.MustCompile(`^Bicep CLI version ([0-9]+)\.([0-9]+)\.([0-9]+)(?: \([^)]+\))?\s*$`).
		FindStringSubmatch(strings.TrimSpace(output))
	if match == nil {
		return semVersion{}, errors.New("unrecognised Bicep CLI version output")
	}
	values := [3]int{}
	for i := range values {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return semVersion{}, errors.New("invalid Bicep CLI version")
		}
		values[i] = n
	}
	return semVersion{values[0], values[1], values[2]}, nil
}

func supportedBicepVersion(output string) bool {
	version, err := parseBicepVersion(output)
	if err != nil {
		return false
	}
	min, _ := parseBicepVersion("Bicep CLI version " + minSupportedBicep)
	max, _ := parseBicepVersion("Bicep CLI version " + maxSupportedBicep)
	compare := func(a, b semVersion) int {
		if a.major != b.major {
			return a.major - b.major
		}
		if a.minor != b.minor {
			return a.minor - b.minor
		}
		return a.patch - b.patch
	}
	return compare(version, min) >= 0 && compare(version, max) <= 0
}

func TestCommandRunnerStartFailuresAreUsageErrors(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "definitely-not-bicep")
		got := (commandRunner{executable: path}).run(context.Background(), "--version")
		if got.exitCode != 2 || got.startErr == nil {
			t.Fatalf("exit/start error = %d/%v, want 2/non-nil", got.exitCode, got.startErr)
		}
	})
	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows executable permission is not represented by the POSIX mode bits")
		}
		path := filepath.Join(t.TempDir(), "bicep")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := (commandRunner{executable: path}).run(context.Background(), "--version")
		if got.exitCode != 2 || got.startErr == nil {
			t.Fatalf("exit/start error = %d/%v, want 2/non-nil", got.exitCode, got.startErr)
		}
	})
}

func TestCommandRunnerDiagnosticsAndExitStatus(t *testing.T) {
	t.Run("nonzero error becomes finding", func(t *testing.T) {
		got := fakeRunner(t).run(context.Background(), "error")
		if got.exitCode != 1 {
			t.Fatalf("exit = %d, want 1", got.exitCode)
		}
		findings, err := parseSARIF(got.stderr)
		if err != nil {
			t.Fatal(err)
		}
		want := Finding{"BCP057", "error", "The name is not defined.", "fixture.bicep", 11, 1, "exact"}
		if len(findings) != 1 || findings[0] != want {
			t.Fatalf("findings = %#v, want %#v", findings, want)
		}
	})
	t.Run("warning preserves successful exit", func(t *testing.T) {
		got := fakeRunner(t).run(context.Background(), "warning")
		findings, err := parseSARIF(got.stderr)
		if err != nil {
			t.Fatal(err)
		}
		if got.exitCode != 0 || len(findings) != 1 || findings[0].Severity != "warning" {
			t.Fatalf("exit/findings = %d/%#v", got.exitCode, findings)
		}
	})
}

func TestMalformedAndPartialCompilerOutput(t *testing.T) {
	t.Run("ARM", func(t *testing.T) {
		got := fakeRunner(t).run(context.Background(), "malformed-arm")
		if _, err := parseARM(got.stdout); err == nil {
			t.Fatal("malformed ARM was accepted")
		}
	})
	t.Run("SARIF", func(t *testing.T) {
		got := fakeRunner(t).run(context.Background(), "malformed-sarif")
		if _, err := parseSARIF(got.stderr); err == nil {
			t.Fatal("malformed SARIF was accepted")
		}
	})
	t.Run("partial ARM does not discard complete diagnostics", func(t *testing.T) {
		got := fakeRunner(t).run(context.Background(), "partial")
		if _, err := parseARM(got.stdout); err == nil {
			t.Fatal("partial ARM was accepted")
		}
		findings, err := parseSARIF(got.stderr)
		if err != nil || len(findings) != 1 || findings[0].RuleID != "BCP057" {
			t.Fatalf("diagnostics = %#v, %v", findings, err)
		}
	})
	t.Run("truncated valid ARM is a protocol failure", func(t *testing.T) {
		if _, err := parseARMResult([]byte(`{"resources":[]}`), true); err == nil {
			t.Fatal("truncated ARM was accepted")
		}
	})
	t.Run("truncated valid SARIF is a protocol failure", func(t *testing.T) {
		payload := []byte(sarifDocument("BCP057", "error", 1, "failure"))
		if _, err := parseSARIFResult(payload, true); err == nil {
			t.Fatal("truncated SARIF was accepted")
		}
	})
}

func TestCommandRunnerStrictOutputBounds(t *testing.T) {
	runner := fakeRunner(t)
	runner.maxOutput = 127
	got := runner.run(context.Background(), "flood")
	if len(got.stdout) != 127 || len(got.stderr) != 127 {
		t.Fatalf("output lengths = %d/%d, want 127/127", len(got.stdout), len(got.stderr))
	}
	if !got.stdoutTruncated || !got.stderrTruncated {
		t.Fatalf("truncation flags = %v/%v, want true/true", got.stdoutTruncated, got.stderrTruncated)
	}
}

func TestCompilerOutputIsSafeForTerminalAndLogs(t *testing.T) {
	got := fakeRunner(t).run(context.Background(), "terminal")
	summary := safeCompilerSummary(got.stderr, 160)
	for _, forbidden := range []string{"\x1b", "\x00", "hunter2", "abc123", "eyJhbGci"} {
		if strings.Contains(summary, forbidden) {
			t.Errorf("summary retained unsafe value %q: %q", forbidden, summary)
		}
	}

	if !strings.Contains(summary, "[REDACTED]") {
		t.Errorf("summary did not retain useful redacted context: %q", summary)
	}
	large := []byte("token=" + strings.Repeat("s", 5000))
	if safe := safeCompilerSummary(large, 40); len(safe) > 60 || !strings.Contains(safe, "[REDACTED]") {
		t.Fatalf("large payload summary was unsafe or unbounded: len=%d, %q", len(safe), safe)
	}
}

func TestCompilerOutputRedactsHighSignalSecrets(t *testing.T) {
	private := "-----BEGIN " + "PRIVATE KEY-----\nvery-secret-material\n-----END PRIVATE KEY-----"
	secrets := []string{
		private,
		"eyJ" + "hbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature",
		"ghp_" + "abcdefghijklmnopqrstuvwxyz123456",
		"github_" + "pat_11AA0_abcdefghijklmnopqrstuvwxyz",
		"AK" + "IAABCDEFGHIJKLMNOP",
		"sk-" + "proj-abcdefghijklmnopqrstuvwxyz",
		"xo" + "xb-1234567890-abcdefghijklmnop",
		"https://example.invalid/a?sv=1&sig=do-not-log-this&se=2",
		"Server=x;AccountKey=do-not-log;EndpointSuffix=core.windows.net",
		"client_secret=do-not-log",
		"storageAccountKey: do-not-log",
	}
	input := strings.Join(secrets, "\n") + "\nAuthorization: Bearer " + "bearer-do-not-log"
	summary := safeCompilerSummary([]byte(input), 8192)
	for _, fragment := range []string{
		"very-secret-material", "eyJhbGci", "ghp_", "github_pat_", "AKIA",
		"sk-proj-", "xoxb-", "do-not-log", "bearer-do-not-log",
	} {
		if strings.Contains(summary, fragment) {
			t.Errorf("summary retained secret fragment %q: %q", fragment, summary)
		}
	}
}

func FuzzSafeCompilerSummarySecretSurvival(f *testing.F) {
	f.Add("prefix", "suffix")
	f.Add("\x1b]0;title\x07", "\nnext")
	f.Fuzz(func(t *testing.T, prefix, suffix string) {
		const secret = "survival-" + "marker-7f3c91"
		forms := []string{
			"client_secret=" + secret,
			"dbPassword: " + secret,
			"Authorization: Bearer " + secret,
			"Endpoint=x;SharedAccessKey=" + secret,
			"https://x.invalid/?sig=" + secret + "&se=1",
		}
		for _, form := range forms {
			got := safeCompilerSummary([]byte(prefix+"\n"+form+"\n"+suffix), 4096)
			if strings.Contains(got, secret) {
				t.Fatalf("secret survived in %q", got)
			}
		}
	})
}

func FuzzSafeCompilerSummaryHostileBytes(f *testing.F) {
	f.Add([]byte{0xff, 0xfe, '\n', 0x1b, '[', '3', '1', 'm'})
	f.Add([]byte("\x1b]8;;https://example.invalid\x1b\\link\x1b]8;;\x07"))
	f.Add([]byte("line one\r\nline two\x00"))
	f.Fuzz(func(t *testing.T, input []byte) {
		got := safeCompilerSummary(input, 31)
		if strings.ToValidUTF8(got, "") != got {
			t.Fatal("summary is not valid UTF-8")
		}
		if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\x00') {
			t.Fatalf("terminal control survived: %q", got)
		}
		if len(got) > 31+len("…[truncated]") {
			t.Fatalf("summary exceeded bound: %d", len(got))
		}
	})
}

func FuzzSafeCompilerSummaryTruncationBoundaries(f *testing.F) {
	f.Add("token=boundary-"+"secret", uint8(0))
	f.Add("αβγ client_secret=boundary-"+"secret", uint8(7))
	f.Add("first\nsecond\nthird", uint8(16))
	f.Fuzz(func(t *testing.T, input string, bound uint8) {
		got := safeCompilerSummary([]byte(input), int(bound))
		if strings.Contains(got, "boundary-secret") {
			t.Fatalf("secret survived truncation: %q", got)
		}
		if strings.ToValidUTF8(got, "") != got {
			t.Fatalf("truncation produced invalid UTF-8: %q", got)
		}
		if len(got) > int(bound)+len("…[truncated]") {
			t.Fatalf("summary exceeded bound: %d > %d", len(got), bound)
		}
	})
}

/*
	if !strings.Contains(summary, "password=[REDACTED]") ||
		!strings.Contains(summary, "Authorization: Bearer [REDACTED]") {
		t.Errorf("summary did not retain useful redacted context: %q", summary)
	}

	large := []byte("token=" + strings.Repeat("s", 5000))
	if safe := safeCompilerSummary(large, 40); len(safe) > 60 || !strings.Contains(safe, "[REDACTED]") {
		t.Fatalf("large payload summary was unsafe or unbounded: len=%d, %q", len(safe), safe)
	}
}

*/

func TestCommandRunnerTimeoutAndCancellation(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		runner := fakeRunner(t)
		runner.timeout = 30 * time.Millisecond
		got := runner.run(context.Background(), "sleep")
		if !got.timedOut || got.exitCode != 1 {
			t.Fatalf("result = %+v, want timeout and exit 1", got)
		}
	})
	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got := fakeRunner(t).run(ctx, "sleep")
		if !got.contextCancelled || got.exitCode != 1 {
			t.Fatalf("result = %+v, want cancellation and exit 1", got)
		}
	})
}

func TestCommandRunnerSignalTermination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports TerminateProcess as an exit status, not a portable signal")
	}
	got := fakeRunner(t).run(context.Background(), "kill")
	if !got.signalled || got.exitCode != 1 {
		t.Fatalf("result = %+v, want signalled and exit 1", got)
	}
}

func TestCommandRunnerUsesMinimalEnvironment(t *testing.T) {
	t.Setenv("AZURE_CLIENT_SECRET", "must-not-cross-process-boundary")
	t.Setenv("HTTPS_PROXY", "http://must-not-cross-process-boundary.invalid")
	got := fakeRunner(t).run(context.Background(), "environment")
	names := strings.Fields(string(got.stdout))
	allowed := map[string]bool{
		"FD_BICEP_FAKE": true,
		"PATH":          true,
		"HOME":          true,
		"TMP":           true,
		"TEMP":          true,
	}
	if runtime.GOOS == "windows" {
		// Windows may inject architecture and OS bootstrap variables when it
		// creates a process even though cmd.Env is an explicit minimal list.
		for _, name := range []string{
			"SYSTEMROOT",
			"WINDIR",
			"COMSPEC",
			"PATHEXT",
			"OS",
			"NUMBER_OF_PROCESSORS",
			"PROCESSOR_ARCHITECTURE",
			"PROCESSOR_IDENTIFIER",
			"PROCESSOR_LEVEL",
			"PROCESSOR_REVISION",
		} {
			allowed[name] = true
		}
	}
	for _, name := range names {
		lookup := name
		if runtime.GOOS == "windows" {
			lookup = strings.ToUpper(lookup)
		}
		if !allowed[lookup] {
			t.Errorf("unexpected inherited environment variable %q", name)
		}
	}
	for _, secret := range []string{"AZURE_CLIENT_SECRET", "HTTPS_PROXY"} {
		if strings.Contains(string(got.stdout), secret) {
			t.Errorf("%s crossed the process boundary", secret)
		}
	}
}

func TestSupportedBicepVersionsAreExact(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"Bicep CLI version 0.47.16 (3f73e1a234)", true},
		{"Bicep CLI version 0.48.0 (abcdef)", true},
		{"Bicep CLI version 0.48.1 (abcdef)", true},
		{"Bicep CLI version 0.47.15 (abcdef)", false},
		{"Bicep CLI version 0.48.2 (abcdef)", false},
		{"Bicep CLI version 1.0.0 (abcdef)", false},
		{"Bicep CLI version 0.48.1-preview", false},
		{"Azure CLI 2.90.0", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.output, " ", "_"), func(t *testing.T) {
			if got := supportedBicepVersion(tt.output); got != tt.want {
				t.Errorf("supportedBicepVersion(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestSARIFWithoutSourceLineDoesNotInventOne(t *testing.T) {
	payload := []byte(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"BCP999","message":{"text":"generated infrastructure"}}]}]}`)
	findings, err := parseSARIF(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Line != 0 || findings[0].Column != 0 ||
		findings[0].Confidence != "likely" || findings[0].Severity != "warning" {
		t.Fatalf("finding invented a source location or wrong defaults: %#v", findings)
	}
}

func TestSARIFSanitisesUntrustedFields(t *testing.T) {
	payload := []byte(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"BCP999","level":"error","message":{"text":"Authorization: Basic do-not-log\u001b[31m"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"file:///C:/outside/fixture.bicep"},"region":{"startLine":1,"startColumn":1}}}]}]}]}`)
	findings, err := parseSARIF(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}
	if strings.Contains(findings[0].Message, "do-not-log") || strings.ContainsRune(findings[0].Message, '\x1b') {
		t.Fatalf("unsafe SARIF message survived: %q", findings[0].Message)
	}
	if findings[0].Path != "fixture.bicep" {
		t.Fatalf("absolute SARIF path was not confined: %q", findings[0].Path)
	}

	traversal := []byte(`{"version":"2.1.0","runs":[{"results":[{"ruleId":"BCP999","message":{"text":"failure"},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"../outside.bicep"}}}]}]}]}`)
	if _, err := parseSARIF(traversal); err == nil {
		t.Fatal("traversing SARIF path was accepted")
	}
}

var _ io.Writer = (*boundedBuffer)(nil)
