// Package cli_test runs the built foundry-doctor binary: the contract a CI job sees (exit codes, flags,
// offline behaviour, link-time version). It needs the Go toolchain and nothing else.
package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const module = "github.com/ruairispain/copilot-web/foundry-doctor"

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fd-cli-")
	if err != nil {
		panic(err)
	}
	name := "foundry-doctor"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary = filepath.Join(dir, name)
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-X "+module+"/internal/buildinfo.Version=v9.9.9-test", "-o", binary, "./cmd/foundry-doctor")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		os.RemoveAll(dir)
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	src, _ := filepath.Abs(filepath.Join("..", "..", "fixtures", "project", name))
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// cleanEnv is the environment of a test run: PATH only, so a developer's Azure login cannot matter.
func cleanEnv(extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.TempDir()}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return append(env, extra...)
}

type outcome struct {
	code           int
	stdout, stderr string
}

func runBin(t *testing.T, dir string, env []string, args ...string) outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir, cmd.Env = dir, env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return outcome{code, out.String(), errb.String()}
}

func TestVersionIsSetByLdflags(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		r := runBin(t, t.TempDir(), cleanEnv(), args...)
		if r.code != 0 || !strings.Contains(r.stdout, "foundry-doctor v9.9.9-test") {
			t.Errorf("%v: %d %q", args, r.code, r.stdout)
		}
	}
}

func TestHelpHasExamples(t *testing.T) {
	for _, c := range []string{"doctor", "explain", "compare", "version"} {
		r := runBin(t, t.TempDir(), cleanEnv(), c, "--help")
		if r.code != 0 || !strings.Contains(r.stdout, "Examples:") {
			t.Errorf("%s: %d\n%s", c, r.code, r.stdout)
		}
	}
}

func TestOutputFlagIsRejected(t *testing.T) {
	r := runBin(t, fixture(t, "none"), cleanEnv(), "doctor", "--output", "x")
	if r.code != 2 || !strings.Contains(r.stderr, "unknown flag: --output") {
		t.Fatalf("%d %q", r.code, r.stderr)
	}
}

func TestRejectedFlagsExit2(t *testing.T) {
	for _, f := range []string{"--preflight", "--runtime", "--security", "--what-if", "--export-tool-config", "--llm-explain"} {
		r := runBin(t, fixture(t, "none"), cleanEnv(), "doctor", f)
		if r.code != 2 || !strings.Contains(r.stderr, "not available in this version") {
			t.Errorf("%s: %d %q", f, r.code, r.stderr)
		}
	}
}

func TestNoProjectExit2(t *testing.T) {
	r := runBin(t, t.TempDir(), cleanEnv(), "doctor")
	if r.code != 2 {
		t.Fatalf("%d %q", r.code, r.stderr)
	}
}

// garbageEnv sets every Azure, azd and proxy variable to a value that would break any client that read it.
func garbageEnv() []string {
	vars := []string{
		"AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET", "AZURE_TENANT_ID", "AZURE_SUBSCRIPTION_ID", "AZURE_AUTHORITY_HOST",
		"AZURE_ENV_NAME", "AZURE_LOCATION", "AZURE_CONFIG_DIR", "AZURE_TOKEN_CREDENTIALS", "AZURE_FEDERATED_TOKEN_FILE",
		"AZD_DEBUG_FORCE_NO_TTY", "AZD_CONFIG_DIR", "AZD_SERVER", "AZD_EXTENSION_ID", "AZD_FORCE_TTY",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"BICEP_PATH",
	}
	var env []string
	for _, v := range vars {
		val := "garbage-value-that-must-not-be-read"
		if strings.Contains(strings.ToLower(v), "proxy") && !strings.HasPrefix(v, "NO_") && !strings.HasPrefix(v, "no_") {
			val = "http://127.0.0.1:1"
		}
		env = append(env, v+"="+val)
	}
	return env
}

func TestDoctorRunsWithAzureEnvCleared(t *testing.T) {
	for _, name := range []string{"none", "synthetic-foundry", "bicep-basic"} {
		dir := fixture(t, name)
		// bicep-basic needs the Bicep CLI; make it optional so the test does not depend on the machine.
		if name == "bicep-basic" {
			cfg := filepath.Join(dir, ".foundry-doctor")
			_ = os.MkdirAll(cfg, 0o755)
			_ = os.WriteFile(filepath.Join(cfg, "config.yaml"), []byte("version: 1\nvalidation:\n  bicep: disabled\n"), 0o644)
		}
		clean := runBin(t, dir, cleanEnv(), "doctor", "--format", "json")
		dirty := runBin(t, dir, cleanEnv(garbageEnv()...), "doctor", "--format", "json")
		if clean.code != 0 {
			t.Fatalf("%s: clean run exit %d\n%s%s", name, clean.code, clean.stdout, clean.stderr)
		}
		if dirty.code != clean.code || dirty.stdout != clean.stdout {
			t.Errorf("%s: garbage Azure environment changed the result (exit %d)\n%s", name, dirty.code, dirty.stderr)
		}
		var rep map[string]any
		if err := json.Unmarshal([]byte(dirty.stdout), &rep); err != nil {
			t.Errorf("%s: not JSON: %v", name, err)
		}
	}
}

func TestCoreHasNoAzureSDKDeps(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", ".."))
	cmd := exec.Command("go", "list", "-deps", "./cmd/foundry-doctor")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(line)
		for _, bad := range []string{"azidentity", "azcore", "azure-dev", "azure-sdk-for-go", "github.com/azure/"} {
			if strings.Contains(l, bad) {
				t.Errorf("forbidden dependency: %s", line)
			}
		}
	}
}

func TestNonInteractive(t *testing.T) {
	// stdin is a pipe that is never written to or closed; a command that reads it would hang until the timeout.
	dir := fixture(t, "none")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "doctor", "--format", "json")
	cmd.Dir, cmd.Env = dir, cleanEnv()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	cmd.Stdin = pr
	if err := cmd.Run(); err != nil {
		t.Fatalf("doctor blocked or failed with an open stdin: %v (%v)", err, ctx.Err())
	}
}

func TestSarifOutFile(t *testing.T) {
	dir := fixture(t, "synthetic-foundry")
	r := runBin(t, dir, cleanEnv(), "doctor", "--format", "sarif", "--out", "reports/foundry-doctor.sarif")
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("%d %q %q", r.code, r.stdout, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(dir, "reports", "foundry-doctor.sarif"))
	if err != nil {
		t.Fatal(err)
	}
	var log map[string]any
	if err := json.Unmarshal(b, &log); err != nil || log["version"] != "2.1.0" {
		t.Fatalf("not SARIF 2.1.0: %v", err)
	}
}

func TestExplainBinary(t *testing.T) {
	r := runBin(t, t.TempDir(), cleanEnv(), "explain", "FND-CFG-001")
	if r.code != 0 || !strings.Contains(r.stdout, "FND-CFG-001") {
		t.Fatalf("%d %q", r.code, r.stderr)
	}
	if r := runBin(t, t.TempDir(), cleanEnv(), "explain", "FND-NOPE-1"); r.code != 2 {
		t.Fatalf("unknown rule: %d", r.code)
	}
}
