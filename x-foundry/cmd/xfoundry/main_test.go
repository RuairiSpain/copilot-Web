package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
