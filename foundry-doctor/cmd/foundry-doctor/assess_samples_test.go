package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateAssessSamples = flag.Bool("update-assess-samples", false, "regenerate samples/assess-*/reports from the real catalogue and engine")

// assessSampleOutputs lists every audience and format rendered per sample.
var assessSampleOutputs = []struct {
	file string
	args []string
}{
	{"owner.md", []string{"--audience", "owner"}},
	{"owner.html", []string{"--audience", "owner", "--format", "html"}},
	{"developer.md", []string{"--audience", "developer"}},
	{"developer.html", []string{"--audience", "developer", "--format", "html"}},
	{"evidence.md", []string{"--evidence-pack"}},
	{"evidence.json", []string{"--evidence-pack", "--format", "json"}},
}

// TestAssessSampleReports renders samples/assess-{dev,test,prod} with the real
// catalogue, mapping and rule engine (no Bicep CLI, fixed clock) and compares
// the output with the committed reports. While samples/assess-*/reports/
// HAND-DERIVED exists, the committed reports were written without a Go
// toolchain and the test fails until they are regenerated with
// -update-assess-samples and reviewed.
func TestAssessSampleReports(t *testing.T) {
	for _, name := range []string{"assess-dev", "assess-test", "assess-prod"} {
		t.Run(name, func(t *testing.T) {
			dir := sample(name)
			reports := filepath.Join(dir, "reports")
			rendered := map[string][]byte{}
			for _, o := range assessSampleOutputs {
				rendered[o.file] = renderAssessSample(t, dir, o.args)
			}
			checkAssessInvariants(t, rendered)
			marker := filepath.Join(reports, "HAND-DERIVED")
			if *updateAssessSamples {
				if err := os.MkdirAll(reports, 0o755); err != nil {
					t.Fatal(err)
				}
				for file, data := range rendered {
					if err := os.WriteFile(filepath.Join(reports, file), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				return
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("%s holds hand-derived reports; regenerate with: go test ./cmd/foundry-doctor -run TestAssessSampleReports -update-assess-samples, then review and commit", reports)
			}
			for file, got := range rendered {
				want, err := os.ReadFile(filepath.Join(reports, file))
				if err != nil {
					t.Fatalf("missing sample report %s: %v", file, err)
				}
				if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
					t.Errorf("%s/%s differs from the rendered report:\n%s", name, file, got)
				}
			}
		})
	}
}

func renderAssessSample(t *testing.T, dir string, args []string) []byte {
	t.Helper()
	for _, k := range []string{"FOUNDRY_DOCTOR_ENVIRONMENT", "AZURE_ENV_NAME", "AZURE_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}
	var out, errb bytes.Buffer
	svc := noBicep(&errb)
	svc.Now = func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }
	code := run(context.Background(), append([]string{"assess", "waf", "--dir", dir}, args...), &out, &errb, svc)
	if code == 2 || code == 4 {
		t.Fatalf("assess %v exit=%d stderr=%s", args, code, errb.String())
	}
	return out.Bytes()
}

func checkAssessInvariants(t *testing.T, r map[string][]byte) {
	t.Helper()
	if bytes.Equal(r["owner.html"], r["developer.html"]) || bytes.Equal(r["owner.md"], r["developer.md"]) {
		t.Fatal("owner and developer views must differ")
	}
	for file, data := range r {
		s := string(data)
		if strings.Contains(s, "<script") {
			t.Errorf("%s contains script", file)
		}
		if file != "evidence.json" && !strings.Contains(s, "Limitations") {
			t.Errorf("%s has no limitations section", file)
		}
	}
	var pack struct {
		Limitations []string `json:"limitations"`
		Controls    map[string][]struct {
			RuleID    string `json:"ruleId"`
			Evaluated bool   `json:"evaluated"`
			Basis     string `json:"basis"`
		} `json:"controls"`
	}
	if err := json.Unmarshal(r["evidence.json"], &pack); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(pack.Limitations, " "), "were not imported") {
		t.Error("limitations must disclose that PSRule/Advisor evidence was not imported")
	}
	for _, c := range pack.Controls["pass"] {
		if !c.Evaluated || c.Basis != "deterministic" {
			t.Errorf("%s is PASS without a deterministic evaluation", c.RuleID)
		}
	}
	for _, c := range pack.Controls["attested"] {
		if c.Basis != "attested" {
			t.Errorf("%s ATTESTED without attested basis", c.RuleID)
		}
	}
}
