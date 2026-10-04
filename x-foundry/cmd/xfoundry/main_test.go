package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const minimal = "../../examples/standalone-minimal.yaml"

func exec(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func write(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "azure.yaml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidateOK(t *testing.T) {
	code, out, _ := exec("validate", minimal)
	if code != 0 || !strings.Contains(out, "valid (") {
		t.Fatalf("code %d, out %q", code, out)
	}
}

func TestValidateReportsErrors(t *testing.T) {
	code, _, errOut := exec("validate", write(t, "x-foundry:\n  topology: {mode: standalone}\n"))
	if code != 1 || !strings.Contains(errOut, "XF102") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestValidateJSON(t *testing.T) {
	code, out, _ := exec("validate", write(t, "name: x\n"), "--json")
	var ds []map[string]string
	if code != 1 || json.Unmarshal([]byte(out), &ds) != nil || ds[0]["code"] != "XF101" {
		t.Fatalf("code %d, out %q", code, out)
	}
	// Flags may also come before the file.
	if code, _, _ := exec("validate", "--json", minimal); code != 0 {
		t.Fatalf("flag before file: %d", code)
	}
}

func TestValidatePrintsWarnings(t *testing.T) {
	file := write(t, "x-foundry:\n  topology: {mode: standalone}\n  security: {roles: {admins: []}}\n  projects: [{name: finance}]\n")
	code, out, errOut := exec("validate", file)
	if code != 0 || !strings.Contains(errOut, "XF114") || !strings.Contains(out, "1 warning(s)") {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = exec("validate", file, "--json")
	var ds []map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &ds) != nil || ds[0]["code"] != "XF114" {
		t.Fatalf("code %d, out %q", code, out)
	}
	code, out, _ = exec("validate", minimal, "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("no warnings should print []: %q", out)
	}
}

func TestPlan(t *testing.T) {
	code, out, _ := exec("plan", "../../examples/hub-spoke.yaml")
	if code != 0 || !strings.HasPrefix(out, "step 1: resource-group") {
		t.Fatalf("code %d, out %q", code, out)
	}
	code, out, _ = exec("plan", "../../examples/hub-spoke.yaml", "--json")
	var p struct{ Nodes []struct{ ID string } }
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil || p.Nodes[0].ID != "resource-group" {
		t.Fatalf("code %d", code)
	}
}

func TestSchema(t *testing.T) {
	code, out, _ := exec("schema")
	var s struct {
		Defs map[string]any `json:"$defs"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil || s.Defs["xFoundry"] == nil {
		t.Fatalf("code %d", code)
	}
}

func TestUsageAndErrors(t *testing.T) {
	if code, _, errOut := exec(); code != 2 || !strings.Contains(errOut, "usage") {
		t.Fatalf("no args: %d %q", code, errOut)
	}
	if code, out, _ := exec("help"); code != 0 || !strings.Contains(out, "usage") {
		t.Fatalf("help: %d", code)
	}
	if code, _, errOut := exec("frobnicate"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown: %d %q", code, errOut)
	}
	if code, _, errOut := exec("validate"); code != 2 || !strings.Contains(errOut, "exactly one file") {
		t.Fatalf("no file: %d %q", code, errOut)
	}
	if code, _, _ := exec("validate", "a", "b"); code != 2 {
		t.Fatalf("two files: %d", code)
	}
	if code, _, _ := exec("validate", "--nope", minimal); code != 2 {
		t.Fatalf("bad flag: %d", code)
	}
	if code, _, errOut := exec("validate", "/nonexistent/azure.yaml"); code != 1 && !strings.Contains(errOut, "XF100") {
		t.Fatalf("missing file: %d %q", code, errOut)
	}
}

func TestEnvironmentFlag(t *testing.T) {
	code, out, errOut := exec("validate", minimal, "--environment", "prod")
	if code != 0 || !strings.Contains(out, "[environment: prod]") || !strings.Contains(errOut, "XF310") || strings.Contains(out, "note:") {
		t.Fatalf("code %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = exec("validate", minimal)
	if code != 0 || !strings.Contains(out, "[environment: dev]") || !strings.Contains(out, "--environment test") {
		t.Fatalf("dev should hint at the stricter profiles: %q", out)
	}
	code, out, _ = exec("validate", minimal, "--environment", "test", "--json")
	var ds []map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &ds) != nil || len(ds) == 0 || ds[0]["pillar"] == "" {
		t.Fatalf("code %d, out %q", code, out)
	}
	if code, _, errOut := exec("validate", minimal, "--environment", "staging"); code != 1 || !strings.Contains(errOut, "unknown environment") {
		t.Fatalf("an unknown environment must fail: %d %q", code, errOut)
	}
	if code, _, _ := exec("plan", minimal, "--environment", "prod"); code != 0 {
		t.Fatalf("plan --environment: %d", code)
	}
}

func TestGenerateWritesTheBicepProject(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "infra")
	code, out, errOut := exec("generate", minimal, "--out", dir)
	if code != 0 || !strings.Contains(out, "wrote ") {
		t.Fatalf("code %d, out %q, stderr %q", code, out, errOut)
	}
	for _, f := range []string{"main.bicep", "resources.bicep", "main.parameters.json", "README.md", "modules/foundry-account.bicep"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	// Unsupported parts of the plan are listed on stdout.
	_, out, _ = exec("generate", "../../examples/enterprise.yaml", "--out", t.TempDir())
	if !strings.Contains(out, "not generated yet: gateway") {
		t.Fatalf("out %q", out)
	}
}

func TestGenerateFailures(t *testing.T) {
	if code, _, errOut := exec("generate", write(t, "name: x\n")); code != 1 || !strings.Contains(errOut, "XF101") {
		t.Fatalf("invalid file: %d %q", code, errOut)
	}
	if code, _, _ := exec("generate"); code != 2 {
		t.Fatalf("missing file: %d", code)
	}
	// The output directory cannot be created below a regular file.
	blocker := write(t, "x")
	if code, _, errOut := exec("generate", minimal, "--out", filepath.Join(blocker, "infra")); code != 1 || errOut == "" {
		t.Fatalf("blocked output: %d %q", code, errOut)
	}
	// A directory where a file should go.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "main.bicep"), 0o750); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := exec("generate", minimal, "--out", dir); code != 1 || errOut == "" {
		t.Fatalf("blocked file: %d %q", code, errOut)
	}
}

const deployDoc = `name: demo
x-foundry:
  topology: {mode: standalone}
  security: {roles: {admins: [Admins]}}
  models: {default: gpt-5, allowed: [gpt-5]}
  projects: [{name: finance}]
  mcps: [{name: graph, endpoint: "https://graph.example/mcp", allowedTools: [search]}]
  toolboxes: [{name: search, tools: [{name: gt, type: mcp, reference: graph}]}]
  agents: [{name: bot, instructions: hi, toolboxes: [search]}, {name: extra, instructions: more}]
`

// foundryServer is a project data plane that remembers versions and records calls.
type foundryServer struct {
	mu       sync.Mutex
	versions map[string]int // "agents/bot" -> latest version
	calls    []string
	last     map[string]string // path -> last request body
}

func newFoundryServer(t *testing.T) (*foundryServer, string) {
	t.Helper()
	f := &foundryServer{versions: map[string]int{}, last: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/projects/finance/")
		f.calls = append(f.calls, r.Method+" "+path)
		kind, name, _ := strings.Cut(path, "/")
		name = strings.TrimSuffix(name, "/versions")
		key := kind + "/" + name
		switch r.Method {
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			f.last[key] = string(body)
			f.versions[key]++
			_, _ = fmt.Fprintf(w, `{"name":%q,"version":"%d"}`, name, f.versions[key])
		case http.MethodGet:
			if v, ok := f.versions[key]; ok {
				_, _ = fmt.Fprintf(w, `{"name":%q,"default_version":"%d"}`, name, v)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodDelete:
			delete(f.versions, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv.URL + "/api/projects"
}

func TestDeployDryRunNeedsNoAzure(t *testing.T) {
	code, out, _ := exec("deploy", minimal, "--dry-run", "--state", filepath.Join(t.TempDir(), "state.json"))
	if code != 0 || !strings.Contains(out, "nothing to do") {
		t.Fatalf("code %d, out %q", code, out)
	}
	state := filepath.Join(t.TempDir(), "state.json")
	code, out, _ = exec("deploy", write(t, deployDoc), "--dry-run", "--state", state)
	for _, want := range []string{"+ finance/toolbox/search", "+ finance/agent/bot", "+ finance/agent/extra", "3 to create [environment: dev]"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("code %d, missing %q in %q", code, want, out)
		}
	}
	if _, err := os.Stat(state); err == nil {
		t.Fatal("a dry run must not write the state")
	}
}

func TestDeployCreatesUpdatesAndGuardsDeletes(t *testing.T) {
	t.Setenv("XFOUNDRY_ACCESS_TOKEN", "test-token")
	f, base := newFoundryServer(t)
	file := write(t, deployDoc)
	state := filepath.Join(t.TempDir(), "state", "dev.json")
	args := []string{"deploy", file, "--endpoint-base", base, "--state", state}

	code, out, errOut := exec(args...)
	if code != 0 || !strings.Contains(out, "deployed; state saved to") {
		t.Fatalf("first deploy: %d %q %q", code, out, errOut)
	}
	if !strings.Contains(f.last["agents/bot"], "/toolboxes/search/versions/1/mcp") || !strings.Contains(f.last["toolboxes/search"], `"server_label":"graph"`) {
		t.Fatalf("bodies: %v", f.last)
	}
	if data, _ := os.ReadFile(state); !strings.Contains(string(data), `"name": "bot"`) {
		t.Fatalf("state = %s", data)
	}

	// Nothing changed: each item is only checked.
	f.calls = nil
	code, out, _ = exec(args...)
	if code != 0 || !strings.Contains(out, "3 unchanged") || strings.Contains(strings.Join(f.calls, ","), "POST") {
		t.Fatalf("second deploy: %d %q %v", code, out, f.calls)
	}

	// Removing an agent from the file needs approval before anything is deleted.
	smaller := strings.Replace(deployDoc, ", {name: extra, instructions: more}", "", 1)
	if err := os.WriteFile(file, []byte(smaller), 0o600); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	code, _, errOut = exec(args...)
	if code != 1 || !strings.Contains(errOut, "XF025") || len(f.calls) != 0 {
		t.Fatalf("blocked deploy: %d %q %v", code, errOut, f.calls)
	}
	if code, _, errOut = exec(append(args, "--dry-run")...); code != 1 || !strings.Contains(errOut, "XF025") {
		t.Fatalf("a dry run reports the destructive change too: %d %q", code, errOut)
	}
	code, out, errOut = exec(append(args, "--allow-destroy")...)
	if code != 0 || !strings.Contains(out, "delete finance/agent/extra") || errOut != "" {
		t.Fatalf("approved deploy: %d %q %q", code, out, errOut)
	}
	if _, still := f.versions["agents/extra"]; still {
		t.Fatal("the agent should be deleted")
	}
}

func TestDeployFailures(t *testing.T) {
	file := write(t, deployDoc)
	state := filepath.Join(t.TempDir(), "state.json")
	if code, _, errOut := exec("deploy", file, "--state", state); code != 2 || !strings.Contains(errOut, "--account") {
		t.Fatalf("missing account: %d %q", code, errOut)
	}
	t.Setenv("XFOUNDRY_ACCESS_TOKEN", "wrong-token")
	_, base := newFoundryServer(t)
	if code, _, errOut := exec("deploy", file, "--endpoint-base", base, "--state", state); code != 1 || !strings.Contains(errOut, "401") {
		t.Fatalf("rejected token: %d %q", code, errOut)
	}
	if code, _, errOut := exec("deploy", write(t, "name: x\n")); code != 1 || !strings.Contains(errOut, "XF101") {
		t.Fatalf("invalid file: %d %q", code, errOut)
	}
	// A state file of another environment is refused.
	other := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(other, []byte(`{"schemaVersion":1,"environment":"prod","items":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := exec("deploy", file, "--dry-run", "--state", other); code != 1 || !strings.Contains(errOut, "belongs to environment") {
		t.Fatalf("wrong environment: %d %q", code, errOut)
	}
}

func TestDeployDefaultsAndEndpointBase(t *testing.T) {
	o := deployOptions{file: filepath.Join("a", "azure.yaml"), account: "acct"}
	if o.projectEndpointBase() != "https://acct.services.ai.azure.com/api/projects" || o.stateFile("dev") != filepath.Join("a", ".xfoundry", "dev.state.json") {
		t.Fatalf("%s %s", o.projectEndpointBase(), o.stateFile("dev"))
	}
	o.endpointBase, o.statePath = "http://x/base/", "mine.json"
	if o.projectEndpointBase() != "http://x/base" || o.stateFile("dev") != "mine.json" {
		t.Fatal("overrides")
	}
}

func TestTokenSource(t *testing.T) {
	t.Setenv("XFOUNDRY_ACCESS_TOKEN", "ready-made")
	get, err := tokenSource()
	if err != nil {
		t.Fatal(err)
	}
	if tok, err := get(context.Background()); err != nil || tok != "ready-made" {
		t.Fatalf("%q %v", tok, err)
	}
	// Without a ready-made token the default credential chain is built; it is only used on demand.
	t.Setenv("XFOUNDRY_ACCESS_TOKEN", "")
	if get, err := tokenSource(); err != nil || get == nil {
		t.Fatalf("%v %v", get, err)
	}
}
