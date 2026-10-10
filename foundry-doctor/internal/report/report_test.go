package report

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var update = flag.Bool("update", false, "update golden files")

func fixture() ([]sdk.Finding, Run) {
	fs := []sdk.Finding{
		{RuleID: "FND-002", Severity: sdk.SeverityWarning, Pillar: "security", Evidence: "note=plain | `x` <script>", Recommendation: "Use *managed* identity", Location: sdk.Location{File: "infra/main.bicep", Line: 12, Column: 3}, DocsURL: "https://learn.microsoft.com/azure/foundry"},
		{RuleID: "FND-001", Severity: sdk.SeverityError, Evidence: "line1\nline2\x1b[31m", Location: sdk.Location{File: "C:\\abs\\secret\\azure.yaml", Line: 1}},
		{RuleID: "FND-003", Severity: sdk.SeverityInfo, Baselined: true, Location: sdk.Location{File: "../../etc/passwd"}},
		{RuleID: "FND-004", Severity: sdk.SeverityError, Suppressed: &sdk.Suppression{Reason: "accepted", Owner: "team"}},
	}
	run := Run{ToolVersion: "1.0.0", Profile: "default", ExitCode: 1, Skipped: []sdk.Skip{
		{RuleID: "FND-020", Reason: "az not signed in", Required: true},
		{RuleID: "FND-010", Reason: "optional scanner missing"},
	}}
	return fs, run
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", name, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("%s differs from golden:\n%s", name, got)
	}
}

func TestGolden(t *testing.T) {
	fs, run := fixture()
	for _, f := range []struct {
		fmt  Format
		file string
	}{{FormatConsole, "console.golden"}, {FormatJSON, "report.json.golden"}, {FormatMarkdown, "report.md.golden"}, {FormatSARIF, "report.sarif.golden"}} {
		var b bytes.Buffer
		if err := Write(&b, f.fmt, fs, run); err != nil {
			t.Fatal(err)
		}
		golden(t, f.file, b.Bytes())
	}
}

func TestEmptyAndNil(t *testing.T) {
	for _, f := range []Format{FormatConsole, FormatJSON, FormatMarkdown, FormatSARIF} {
		var b bytes.Buffer
		if err := Write(&b, f, nil, Run{}); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if b.Len() == 0 {
			t.Fatalf("%s empty", f)
		}
		if f == FormatJSON || f == FormatSARIF {
			var v map[string]any
			if err := json.Unmarshal(b.Bytes(), &v); err != nil {
				t.Fatalf("%s invalid json: %v", f, err)
			}
		}
	}
	golden(t, "empty.md.golden", func() []byte { var b bytes.Buffer; _ = Markdown(&b, nil, Run{}); return b.Bytes() }())
}

func TestUnknownFormat(t *testing.T) {
	if err := Write(&bytes.Buffer{}, Format("x"), nil, Run{}); err == nil {
		t.Fatal("want error")
	}
	if _, err := ParseFormat("nope"); err == nil {
		t.Fatal("want error")
	}
	if f, err := ParseFormat("MD"); err != nil || f != FormatMarkdown {
		t.Fatal(f, err)
	}
}

func TestDeterministicAndNoMutation(t *testing.T) {
	fs, run := fixture()
	rev := make([]sdk.Finding, len(fs))
	for i := range fs {
		rev[len(fs)-1-i] = fs[i]
	}
	orig := rev[0].Location.File
	for _, f := range []Format{FormatConsole, FormatJSON, FormatMarkdown, FormatSARIF} {
		var a, b bytes.Buffer
		_ = Write(&a, f, fs, run)
		_ = Write(&b, f, rev, run)
		if a.String() != b.String() {
			t.Errorf("%s not deterministic", f)
		}
	}
	if rev[0].Location.File != orig {
		t.Error("input mutated")
	}
}

func TestNoSecretsOrAbsolutePaths(t *testing.T) {
	fs, run := fixture()
	fs[0].Evidence = "token=ghp_" + strings.Repeat("a", 36)
	for _, f := range []Format{FormatConsole, FormatJSON, FormatMarkdown, FormatSARIF} {
		var b bytes.Buffer
		_ = Write(&b, f, fs, run)
		s := b.String()
		if strings.Contains(s, "ghp_aaaa") {
			t.Errorf("%s leaked secret", f)
		}
		if strings.Contains(s, "C:\\\\abs") || strings.Contains(s, "C:\\abs") || strings.Contains(s, "etc/passwd") && strings.Contains(s, "../") {
			t.Errorf("%s leaked path", f)
		}
	}
}

func TestSanitizePath(t *testing.T) {
	cases := map[string]string{
		"":                       "",
		"a/b.yaml":               "a/b.yaml",
		"./a//b":                 "a/b",
		"/etc/passwd":            "passwd",
		"C:\\x\\y.bicep":         "y.bicep",
		"..\\..\\z":              "z",
		"a/../../b":              "b",
		"\\\\srv\\share\\f.json": "f.json",
		"~/.ssh/id":              "id",
		"a\x00b/c\n":             "ab/c",
		"..":                     "",
	}
	for in, want := range cases {
		got := SanitizePath(in)
		if got != want {
			t.Errorf("SanitizePath(%q)=%q want %q", in, got, want)
		}
		if strings.HasPrefix(got, "/") || strings.Contains(got, "..") {
			t.Errorf("unsafe result %q", got)
		}
	}
}

func TestSafeRuleID(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	for _, in := range []string{"", "FND-001", "a b/c", "../x", "Ã©\n", "---"} {
		if g := SafeRuleID(in); !re.MatchString(g) {
			t.Errorf("%q -> %q", in, g)
		}
	}
}

func TestSARIFStructure(t *testing.T) {
	fs, run := fixture()
	var b bytes.Buffer
	if err := SARIF(&b, fs, run); err != nil {
		t.Fatal(err)
	}
	var log struct {
		Version string
		Runs    []struct {
			Tool struct {
				Driver struct{ Rules []struct{ ID string } }
			}
			Invocations []struct {
				ExecutionSuccessful        bool
				ToolExecutionNotifications []struct {
					Level      string
					Descriptor struct{ ID string }
				}
			}
			Results []struct {
				RuleID, BaselineState string
				RuleIndex             int
				PartialFingerprints   map[string]string
				Suppressions          []any
			}
		}
	}
	if err := json.Unmarshal(b.Bytes(), &log); err != nil {
		t.Fatal(err)
	}
	r := log.Runs[0]
	if log.Version != "2.1.0" || len(r.Results) != 4 {
		t.Fatalf("bad log %+v", log)
	}
	for _, res := range r.Results {
		if r.Tool.Driver.Rules[res.RuleIndex].ID != res.RuleID || res.PartialFingerprints[FingerprintKey] == "" {
			t.Errorf("bad result %+v", res)
		}
	}
	n := r.Invocations[0].ToolExecutionNotifications
	if len(n) != 2 || n[0].Descriptor.ID != "FND-010" || n[1].Level != "warning" {
		t.Errorf("notifications %+v", n)
	}
	if !r.Invocations[0].ExecutionSuccessful {
		t.Error("exit 1 is a successful execution")
	}
}

func TestMarkdownEscaping(t *testing.T) {
	f := []sdk.Finding{{RuleID: "R|1", Severity: sdk.SeverityError, Evidence: "a|b `` ` <img src=x onerror=1>\n# h", Recommendation: "[x](javascript:alert(1)) <b>", DocsURL: "javascript:alert(1)"}}
	var b bytes.Buffer
	_ = Markdown(&b, f, Run{ToolVersion: "<v>"})
	s := b.String()
	if strings.Contains(s, "<img") || strings.Contains(s, "<b>") || strings.Contains(s, "<v>") || strings.Contains(s, "](javascript") {
		t.Errorf("unescaped output:\n%s", s)
	}
	checkTableRows(t, s)
}

func checkTableRows(t *testing.T, s string) {
	t.Helper()
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "| Severity") {
			continue
		}
		if strings.HasPrefix(l, "| ") && strings.Contains(l, "FND") || strings.HasPrefix(l, "| error") {
			if n := len(regexp.MustCompile(`(^|[^\\])\|`).FindAllString(l, -1)); n != 8 {
				t.Errorf("row has %d unescaped pipes: %q", n, l)
			}
		}
	}
}

func FuzzMarkdown(f *testing.F) {
	f.Add("a|b", "`x`", "https://x.y")
	f.Add("<script>\n", "[a](b)", "javascript:1")
	f.Fuzz(func(t *testing.T, ev, rec, docs string) {
		var b bytes.Buffer
		if err := Markdown(&b, []sdk.Finding{{RuleID: ev, Severity: sdk.SeverityError, Evidence: ev, Recommendation: rec, DocsURL: docs, Resource: sdk.ResourceRef{Name: rec}}}, Run{}); err != nil {
			t.Fatal(err)
		}
		for _, l := range strings.Split(b.String(), "\n") {
			if strings.HasPrefix(l, "| error") {
				if strings.Contains(l, "<script") || strings.Contains(l, "<img") {
					t.Fatalf("raw html: %q", l)
				}
				if n := len(regexp.MustCompile(`(^|[^\\])\|`).FindAllString(l, -1)); n != 8 {
					t.Fatalf("row break %d: %q", n, l)
				}
			}
		}
	})
}

func FuzzSARIF(f *testing.F) {
	f.Add("FND-001", "C:\\a\\b", "msg")
	f.Add("../x y", "../../z", "\x00\n")
	re := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	f.Fuzz(func(t *testing.T, id, file, msg string) {
		var b bytes.Buffer
		fs := []sdk.Finding{{RuleID: id, Severity: sdk.SeverityWarning, Evidence: msg, Location: sdk.Location{File: file, Line: 1}}}
		if err := SARIF(&b, fs, Run{Skipped: []sdk.Skip{{RuleID: id, Reason: msg}}}); err != nil {
			t.Fatal(err)
		}
		var v struct {
			Version string
			Runs    []struct {
				Results []struct {
					RuleID    string
					Locations []struct {
						PhysicalLocation struct{ ArtifactLocation struct{ URI string } }
					}
				}
			}
		}
		if err := json.Unmarshal(b.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v.Version != "2.1.0" {
			t.Fatal("version")
		}
		for _, r := range v.Runs[0].Results {
			if !re.MatchString(r.RuleID) {
				t.Fatalf("bad ruleId %q", r.RuleID)
			}
			for _, l := range r.Locations {
				u := l.PhysicalLocation.ArtifactLocation.URI
				if strings.HasPrefix(u, "/") || hasDotDotSegment(u) || strings.Contains(u, ":") || strings.Contains(u, "\\") {
					t.Fatalf("unsafe uri %q", u)
				}
			}
		}
	})
}

func TestClassifyExit(t *testing.T) {
	errF := sdk.Finding{Severity: sdk.SeverityError}
	warnF := sdk.Finding{Severity: sdk.SeverityWarning}
	supp := sdk.Finding{Severity: sdk.SeverityError, Suppressed: &sdk.Suppression{Reason: "r"}}
	base := sdk.Finding{Severity: sdk.SeverityError, Baselined: true}
	opt := []sdk.Skip{{RuleID: "A"}}
	req := []sdk.Skip{{RuleID: "A", Required: true}}
	cases := []struct {
		name string
		o    Outcome
		want int
	}{
		{"clean", Outcome{}, 0},
		{"error finding", Outcome{Findings: []sdk.Finding{errF}}, 1},
		{"warning below default threshold", Outcome{Findings: []sdk.Finding{warnF}}, 0},
		{"warning at warning threshold", Outcome{Findings: []sdk.Finding{warnF}, FailOn: sdk.SeverityWarning}, 1},
		{"invalid failon defaults to error", Outcome{Findings: []sdk.Finding{warnF}, FailOn: "bogus"}, 0},
		{"suppressed ignored", Outcome{Findings: []sdk.Finding{supp}}, 0},
		{"baselined ignored", Outcome{Findings: []sdk.Finding{base}}, 0},
		{"optional skip keeps 0", Outcome{Skipped: opt}, 0},
		{"optional skip with findings is 1", Outcome{Skipped: opt, Findings: []sdk.Finding{errF}}, 1},
		{"strict skip", Outcome{Skipped: opt, Strict: true}, 3},
		{"strict without skip", Outcome{Strict: true}, 0},
		{"strict skip with findings is 3", Outcome{Skipped: opt, Strict: true, Findings: []sdk.Finding{errF}}, 3},
		{"required skip is 2", Outcome{Skipped: req}, 2},
		{"2 beats 1", Outcome{Skipped: req, Findings: []sdk.Finding{errF}}, 2},
		{"2 beats strict 3", Outcome{Skipped: req, Strict: true}, 2},
		{"cannot run", Outcome{CannotRun: true, Findings: []sdk.Finding{errF}}, 2},
		{"internal beats all", Outcome{InternalError: true, CannotRun: true, Skipped: req, Findings: []sdk.Finding{errF}}, 4},
	}
	for _, c := range cases {
		if got := ClassifyExit(c.o); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func hasDotDotSegment(u string) bool {
	for _, s := range strings.Split(u, "/") {
		if s == ".." {
			return true
		}
	}
	return false
}
