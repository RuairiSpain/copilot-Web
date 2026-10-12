//go:build spike

package azdspike

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const pinnedAzureDevCommit = "afe4b2b4d262bab4c11f4937e7a7942557ab0ecd"
const maxPinnedProcessOutput = 1 << 20

// TestPinnedMicrosoftSynthesisSourceIsExecuted is intentionally not a JSON-schema
// test. It executes Microsoft's azure.ai.projects synthesis package tests at the
// source revision used by this spike, then requires non-zero coverage of
// Synthesizer.Synthesize and validates both emitted-template assets. Dependencies
// must already be in the Go cache; GOPROXY=off keeps the contract fully offline.
//
// Run with:
//
//	AZURE_DEV_DIR=/absolute/path/to/azure-dev go test -tags spike ./test/spikes/azd
func TestPinnedMicrosoftSynthesisSourceIsExecuted(t *testing.T) {
	clone := os.Getenv("AZURE_DEV_DIR")
	const dependencyMessage = "required dependency: AZURE_DEV_DIR must name an offline Azure/azure-dev checkout at " + pinnedAzureDevCommit + " with its Go dependencies already cached"
	if clone == "" || !filepath.IsAbs(clone) {
		spikeDependencyUnavailable(t, dependencyMessage)
	}
	assertPinnedAzureDevCheckout(t, clone)

	synthesisDir := filepath.Join(clone, "cli", "azd", "extensions", "azure.ai.projects", "internal", "synthesis")
	if info, err := os.Stat(synthesisDir); err != nil || !info.IsDir() {
		spikeDependencyUnavailable(t, dependencyMessage+": synthesis package is absent")
	}
	coverage := filepath.Join(t.TempDir(), "synthesis.cover")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	output, err := runPinnedCommand(ctx, synthesisDir, "go", "test", "-count=1", "-coverprofile="+coverage, ".")
	if err != nil {
		if strings.Contains(string(output), "module lookup disabled by GOPROXY=off") ||
			strings.Contains(string(output), "missing go.sum entry") ||
			strings.Contains(string(output), "cannot find module providing package") {
			spikeDependencyUnavailable(t, dependencyMessage+": "+safePinnedOutput(output))
		}
		t.Fatalf("Microsoft synthesis package tests failed: %v\n%s", err, safePinnedOutput(output))
	}
	coverOutput := command(t, synthesisDir, 30*time.Second, "go", "tool", "cover", "-func="+coverage)
	assertCoveredFunction(t, coverOutput, "Synthesize", "Microsoft Synthesizer.Synthesize")

	// Provider tests exercise the branch that gives user-authored Bicep precedence
	// over synthesis. Locate the package from the pinned source rather than
	// assuming that this preview extension never moves the file.
	providerFile := findFile(t, filepath.Join(clone, "cli", "azd", "extensions", "azure.ai.projects"), "foundry_provisioning_provider.go")
	providerDir := filepath.Dir(providerFile)
	providerCoverage := filepath.Join(t.TempDir(), "provider.cover")
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	output, err = runPinnedCommand(ctx, providerDir, "go", "test", "-count=1", "-coverprofile="+providerCoverage, ".")
	if err != nil {
		if strings.Contains(string(output), "module lookup disabled by GOPROXY=off") ||
			strings.Contains(string(output), "missing go.sum entry") ||
			strings.Contains(string(output), "cannot find module providing package") {
			spikeDependencyUnavailable(t, dependencyMessage+": "+safePinnedOutput(output))
		}
		t.Fatalf("Microsoft provisioning-provider package tests failed: %v\n%s", err, safePinnedOutput(output))
	}
	providerCoverOutput := command(t, providerDir, 30*time.Second, "go", "tool", "cover", "-func="+providerCoverage)
	assertCoveredFunction(t, providerCoverOutput, `onDiskTemplatePresent`, "Microsoft onDiskTemplatePresent")

	templateDir := filepath.Join(clone, "cli", "azd", "extensions", "azure.ai.projects", "internal", "synthesis", "templates")
	assertARMTemplate(t, filepath.Join(templateDir, "main.arm.json"))
	assertARMTemplate(t, filepath.Join(templateDir, "existing-project.arm.json"))
}

// spikeDependencyUnavailable is the only permitted skip point in the external
// spike. Required mode deliberately turns every missing checkout, tool, source
// directory, or offline module-cache entry into a failure.
func spikeDependencyUnavailable(t *testing.T, message string) {
	t.Helper()
	if os.Getenv("REQUIRE_SPIKE_DEPS") == "1" {
		t.Fatal(message)
	}
	t.Skip(message)
}

func assertPinnedAzureDevCheckout(t *testing.T, clone string) {
	t.Helper()
	if info, err := os.Stat(clone); err != nil || !info.IsDir() {
		spikeDependencyUnavailable(t, "AZURE_DEV_DIR checkout is unavailable: "+clone)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = clone
	out, err := cmd.CombinedOutput()
	if err != nil {
		spikeDependencyUnavailable(t, "cannot inspect AZURE_DEV_DIR with git: "+err.Error()+": "+strings.TrimSpace(string(out)))
	}
	if got := strings.TrimSpace(string(out)); got != pinnedAzureDevCommit {
		t.Fatalf("AZURE_DEV_DIR must be exactly Azure/azure-dev commit %s; checkout is at %q", pinnedAzureDevCommit, got)
	}
}

func command(t *testing.T, dir string, timeout time.Duration, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	home := t.TempDir()
	cmd.Env = pinnedProcessEnvironment(home)
	out, err := boundedCombinedOutput(cmd)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("%s timed out", name)
	}
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, safePinnedOutput(out))
	}
	return string(out)
}

type boundedOutput struct {
	buffer    bytes.Buffer
	remaining int
}

func (w *boundedOutput) Write(payload []byte) (int, error) {
	original := len(payload)
	if len(payload) > w.remaining {
		payload = payload[:w.remaining]
	}
	if len(payload) > 0 {
		_, _ = w.buffer.Write(payload)
		w.remaining -= len(payload)
	}
	return original, nil
}

func boundedCombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	output := &boundedOutput{remaining: maxPinnedProcessOutput}
	cmd.Stdout = io.Writer(output)
	cmd.Stderr = io.Writer(output)
	err := cmd.Run()
	return output.buffer.Bytes(), err
}

func runPinnedCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("pinned third-party tests require Linux network-namespace isolation")
	}
	home, err := os.MkdirTemp("", "foundry-doctor-pinned-home-")
	if err != nil {
		return nil, fmt.Errorf("create isolated profile: %w", err)
	}
	defer os.RemoveAll(home)
	unshareArgs := append([]string{"--user", "--map-root-user", "--net", "--", name}, args...)
	cmd := exec.CommandContext(ctx, "unshare", unshareArgs...)
	cmd.Dir = dir
	cmd.Env = pinnedProcessEnvironment(home)
	return boundedCombinedOutput(cmd)
}

func pinnedProcessEnvironment(home string) []string {
	names := []string{
		"PATH", "SystemRoot", "TMP", "TEMP", "GOCACHE", "GOMODCACHE", "GOPATH",
		"GOENV", "GOTOOLCHAIN",
	}
	env := make([]string, 0, len(names)+12)
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"LOCALAPPDATA="+filepath.Join(home, "local"),
		"APPDATA="+filepath.Join(home, "roaming"),
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"GOPROXY=off",
		"GOSUMDB=off",
		"HTTP_PROXY=http://127.0.0.1:1",
		"HTTPS_PROXY=http://127.0.0.1:1",
		"ALL_PROXY=http://127.0.0.1:1",
		"NO_PROXY=",
	)
}

func safePinnedOutput(output []byte) string {
	value := strings.ToValidUTF8(string(output), "")
	value = regexp.MustCompile(`(?i)(authorization\s*:)\s*[^\r\n]+`).ReplaceAllString(value, "$1 [REDACTED]")
	value = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:password|token|secret|key)[a-z0-9_.-]*)\s*[:=]\s*[^\s,;]+`).ReplaceAllString(value, "$1=[REDACTED]")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, value)
	return strings.TrimSpace(value)
}

func assertCoveredFunction(t *testing.T, report, functionName, description string) {
	t.Helper()
	re := regexp.MustCompile(`(?m)(?:^|[.\s])` + regexp.QuoteMeta(functionName) + `\s+([0-9.]+)%`)
	match := re.FindStringSubmatch(report)
	if len(match) != 2 {
		t.Fatalf("%s is absent from coverage report; source contract changed:\n%s", description, report)
	}
	percent, err := strconv.ParseFloat(match[1], 64)
	if err != nil || percent == 0 {
		t.Fatalf("%s was not called (coverage %q)", description, match[1])
	}
}

func assertARMTemplate(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("required synthesized template asset %s: %v", path, err)
	}
	var template struct {
		Schema    string          `json:"$schema"`
		Resources json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal(raw, &template); err != nil {
		t.Fatalf("invalid synthesized ARM template %s: %v", path, err)
	}
	if template.Schema == "" || len(template.Resources) == 0 {
		t.Fatalf("synthesized ARM template %s has no schema/resources", path)
	}
	var arrayResources []json.RawMessage
	var symbolicResources map[string]json.RawMessage
	if err := json.Unmarshal(template.Resources, &arrayResources); err != nil {
		if err := json.Unmarshal(template.Resources, &symbolicResources); err != nil {
			t.Fatalf("synthesized ARM template %s has invalid resources: %v", path, err)
		}
	}
	if len(arrayResources) == 0 && len(symbolicResources) == 0 {
		t.Fatalf("synthesized ARM template %s has empty resources", path)
	}
}

func findFile(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == name {
			if found != "" {
				t.Fatalf("source contract found more than one %s", name)
			}
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("locate %s: %v", name, err)
	}
	if found == "" {
		t.Fatalf("pinned source contract requires %s", name)
	}
	return found
}
