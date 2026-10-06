package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssessHelp(t *testing.T) {
	code, out, _ := exec(t, "assess", "waf", "--help")
	if code != 0 {
		t.Fatalf("help exit=%d", code)
	}
	for _, want := range []string{"--audience", "--evidence-pack", "--format", "--out", "--llm-explain"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
}

func TestAssessEvidenceJSON(t *testing.T) {
	var out, errb bytes.Buffer
	code := run(context.Background(), []string{"assess", "waf", "--evidence-pack", "--format", "json", "--dir", sample("assess-dev")}, &out, &errb, noBicep(&errb))
	if code != 0 && code != 1 && code != 2 && code != 3 {
		t.Fatalf("exit=%d stderr=%s", code, errb.String())
	}
	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if v["framework"] != "azure-waf" {
		t.Fatalf("json=%v", v)
	}
}

func TestAssessInvalidYAMLExitsTwo(t *testing.T) {
	code, _, errs := exec(t, "assess", "waf", "--dir", sample("assess-test"))
	if code != 2 {
		t.Fatalf("exit=%d err=%q", code, errs)
	}
}

func TestAssessOwnerMarkdown(t *testing.T) {
	var errb bytes.Buffer
	code, out, errs := exec(t, "assess", "waf", "--dir", sample("assess-dev"))
	if code != 0 && code != 1 && code != 2 && code != 3 {
		t.Fatalf("exit=%d err=%q", code, errs)
	}
	if !strings.Contains(out, "WAF owner summary") || strings.Contains(out, "<script") {
		t.Fatalf("out=%s", out)
	}
	_ = errb
}

func TestAssessOutFile(t *testing.T) {
	dir := sample("assess-prod")
	out := filepath.Join(t.TempDir(), "assess.md")
	code, stdout, _ := exec(t, "assess", "waf", "--out", out, "--dir", dir)
	if code != 0 && code != 1 && code != 2 && code != 3 {
		t.Fatalf("exit=%d", code)
	}
	if stdout != "" {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestAssessLLMFallbackIsNonFatal(t *testing.T) {
	code, out, errs := exec(t, "assess", "waf", "--llm-explain", "--dir", sample("assess-dev"))
	if code != 0 && code != 1 && code != 2 && code != 3 {
		t.Fatalf("exit=%d out=%s err=%q", code, out, errs)
	}
	if !strings.Contains(out, "LLM advisory unavailable") {
		t.Fatalf("out=%s", out)
	}
}
