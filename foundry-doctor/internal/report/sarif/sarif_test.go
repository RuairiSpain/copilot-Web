package sarif_test

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/reporttest"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/sarif"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func render(t *testing.T, opts sarif.Options, r *sdk.Report) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := sarif.New(opts).Write(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := stdjson.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestGolden(t *testing.T) {
	reporttest.Golden(t, "report.golden.sarif", render(t, sarif.Options{}, reporttest.FullReport()))
}

func TestFormat(t *testing.T) {
	if sarif.New(sarif.Options{}).Format() != "sarif" {
		t.Fatal("format")
	}
}

// ---- GitHub required-field checker, implemented from docs/development/phase-1-tooling-facts.md ----

var hashShape = regexp.MustCompile(`^[0-9a-f]+:[0-9]+$`)

func str(m map[string]any, path ...string) (string, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur = mm[p]
	}
	s, ok := cur.(string)
	return s, ok && s != ""
}

func checkGitHub(log map[string]any) []string {
	var errs []string
	bad := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if _, ok := str(log, "$schema"); !ok {
		bad("$schema missing or empty")
	}
	if v, _ := str(log, "version"); v != "2.1.0" {
		bad("version = %q, want 2.1.0", v)
	}
	runs, ok := log["runs"].([]any)
	if !ok || len(runs) == 0 {
		return append(errs, "runs[] missing")
	}
	if len(runs) > 20 {
		bad("more than 20 runs")
	}
	for ri, r := range runs {
		run := r.(map[string]any)
		if _, ok := str(run, "tool", "driver", "name"); !ok {
			bad("runs[%d].tool.driver.name missing", ri)
		}
		tool, _ := run["tool"].(map[string]any)
		driver, _ := tool["driver"].(map[string]any)
		rules, ok := driver["rules"].([]any)
		if !ok {
			bad("runs[%d].tool.driver.rules[] missing", ri)
		}
		if len(rules) > 25000 {
			bad("too many rules")
		}
		for i, ru := range rules {
			rule, _ := ru.(map[string]any)
			for _, p := range [][]string{{"id"}, {"shortDescription", "text"}, {"fullDescription", "text"}, {"help", "text"}} {
				if _, ok := str(rule, p...); !ok {
					bad("rules[%d].%s missing or empty", i, strings.Join(p, "."))
				}
			}
			for _, p := range []string{"shortDescription", "fullDescription"} {
				s, _ := str(rule, p, "text")
				if len(utf16.Encode([]rune(s))) > 1024 {
					bad("rules[%d].%s over 1024 characters", i, p)
				}
			}
			if n, _ := str(rule, "name"); len(utf16.Encode([]rune(n))) > 255 {
				bad("rules[%d].name over 255", i)
			}
			if l, _ := str(rule, "defaultConfiguration", "level"); l != "note" && l != "warning" && l != "error" {
				bad("rules[%d] default level %q", i, l)
			}
			props, _ := rule["properties"].(map[string]any)
			if tags, _ := props["tags"].([]any); len(tags) > 10 {
				bad("rules[%d] has %d tags", i, len(tags))
			}
		}
		results, ok := run["results"].([]any)
		if !ok {
			bad("runs[%d].results missing", ri)
		}
		if len(results) > 25000 {
			bad("more than 25000 results")
		}
		for i, re := range results {
			res, _ := re.(map[string]any)
			if _, ok := str(res, "message", "text"); !ok {
				bad("results[%d].message.text missing", i)
			}
			if id, _ := str(res, "ruleId"); id == "" {
				bad("results[%d].ruleId missing", i)
			} else if idx, ok := res["ruleIndex"].(float64); !ok || int(idx) >= len(rules) || rules[int(idx)].(map[string]any)["id"] != id {
				bad("results[%d].ruleIndex does not point at rule %q", i, id)
			}
			if l, _ := str(res, "level"); l != "note" && l != "warning" && l != "error" {
				bad("results[%d].level %q", i, l)
			}
			pf, ok := res["partialFingerprints"].(map[string]any)
			if !ok {
				bad("results[%d].partialFingerprints missing", i)
			} else if h, _ := pf["primaryLocationLineHash"].(string); !hashShape.MatchString(h) {
				bad("results[%d] primaryLocationLineHash %q is not hash:occurrence", i, h)
			}
			locs, ok := res["locations"].([]any)
			if !ok || len(locs) == 0 {
				bad("results[%d].locations[] missing", i)
				continue
			}
			l0, _ := locs[0].(map[string]any)
			pl, _ := l0["physicalLocation"].(map[string]any)
			uri, ok := str(pl, "artifactLocation", "uri")
			if !ok || strings.HasPrefix(uri, "/") || strings.Contains(uri, "://") || strings.Contains(uri, "\\") {
				bad("results[%d] artifact uri %q missing or not relative", i, uri)
			}
			region, _ := pl["region"].(map[string]any)
			for _, k := range []string{"startLine", "startColumn", "endLine", "endColumn"} {
				if v, ok := region[k].(float64); !ok || v < 1 {
					bad("results[%d].region.%s = %v", i, k, region[k])
				}
			}
		}
	}
	return errs
}

func TestGitHubRequiredFields(t *testing.T) {
	for name, r := range map[string]*sdk.Report{"full": reporttest.FullReport(), "empty": {}, "huge": reporttest.Huge(300)} {
		if errs := checkGitHub(decode(t, render(t, sarif.Options{}, r))); len(errs) > 0 {
			t.Errorf("%s: %s", name, strings.Join(errs, "\n"))
		}
	}
}

func TestCheckerDetectsProblems(t *testing.T) {
	bad := decode(t, []byte(`{"$schema":"","version":"2.0","runs":[{"tool":{"driver":{"name":"x","rules":[{"id":"a"}]}},"results":[{"ruleId":"a","ruleIndex":0,"level":"fatal","message":{"text":""},"locations":[{"physicalLocation":{"artifactLocation":{"uri":"/abs"},"region":{"startLine":0}}}]}]}]}`))
	if errs := checkGitHub(bad); len(errs) < 8 {
		t.Errorf("checker too lax, found only %v", errs)
	}
}

// ---- behaviour ----

func firstRun(t *testing.T, b []byte) map[string]any {
	t.Helper()
	return decode(t, b)["runs"].([]any)[0].(map[string]any)
}

func TestSkippedAreNotificationsNeverResults(t *testing.T) {
	run := firstRun(t, render(t, sarif.Options{}, reporttest.FullReport()))
	inv := run["invocations"].([]any)[0].(map[string]any)
	notes := inv["toolExecutionNotifications"].([]any)
	if len(notes) != 3 {
		t.Fatalf("notifications = %d", len(notes))
	}
	for _, n := range notes {
		nm := n.(map[string]any)
		if nm["level"] != "note" {
			t.Errorf("level = %v", nm["level"])
		}
	}
	first := notes[0].(map[string]any)["message"].(map[string]any)["text"].(string)
	if !strings.Contains(first, "FND-IDN-004") && !strings.Contains(first, "FND-IDN-005") && !strings.Contains(first, "FND-NET-009") {
		t.Errorf("notification text = %q", first)
	}
	for _, res := range run["results"].([]any) {
		id := res.(map[string]any)["ruleId"].(string)
		if id == "FND-NET-009" || id == "FND-IDN-004" {
			t.Errorf("skipped check %s emitted as result", id)
		}
	}
	for _, r := range run["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any) {
		if id := r.(map[string]any)["id"]; id == "FND-NET-009" {
			t.Errorf("skipped rule in rules[]")
		}
	}
}

func TestResultMapping(t *testing.T) {
	run := firstRun(t, render(t, sarif.Options{}, reporttest.FullReport()))
	results := run["results"].([]any)
	if len(results) != 5 {
		t.Fatalf("results = %d", len(results))
	}
	byFP := map[string]map[string]any{}
	for _, r := range results {
		m := r.(map[string]any)
		byFP[m["partialFingerprints"].(map[string]any)["foundryDoctor/v1"].(string)] = m
	}
	main := byFP["fp1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
	if main["level"] != "error" {
		t.Errorf("level = %v", main["level"])
	}
	pl := main["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
	if got := pl["region"]; fmt.Sprint(got) != "map[endColumn:2 endLine:14 startColumn:5 startLine:12]" {
		t.Errorf("region = %v", got)
	}
	if al := pl["artifactLocation"].(map[string]any); al["uri"] != "infra/main.bicep" || al["uriBaseId"] != "%SRCROOT%" {
		t.Errorf("artifactLocation = %v", al)
	}
	props := main["properties"].(map[string]any)
	if props["confidence"] != "certain" || props["profile"] != "foundry-prod" || fmt.Sprint(props["basis"]) != "[platform docs]" {
		t.Errorf("properties = %v", props)
	}
	if _, ok := main["suppressions"]; ok {
		t.Error("active finding has suppressions")
	}

	sup := byFP["fp1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]["suppressions"].([]any)
	if s := sup[0].(map[string]any); s["kind"] != "external" || s["status"] != "accepted" || s["justification"] != "accepted risk" {
		t.Errorf("suppression = %v", s)
	}
	base := byFP["fp1:cccccccccccccccccccccccccccccccc"]["suppressions"].([]any)
	if len(base) != 1 || base[0].(map[string]any)["properties"].(map[string]any)["source"] != "baseline" {
		t.Errorf("baseline suppression = %v", base)
	}
	// Info maps to note.
	if byFP["fp1:cccccccccccccccccccccccccccccccc"]["level"] != "note" {
		t.Error("info is not note")
	}
	// Empty File gets the repository root.
	root := byFP["fp1:cccccccccccccccccccccccccccccccc"]["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
	if root["artifactLocation"].(map[string]any)["uri"] != "." || fmt.Sprint(root["region"]) != "map[endColumn:1 endLine:1 startColumn:1 startLine:1]" {
		t.Errorf("root location = %v", root)
	}
	// Known file, unknown line: whole-file defaults.
	yaml := byFP["fp1:dddddddddddddddddddddddddddddddd"]["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)
	if yaml["artifactLocation"].(map[string]any)["uri"] != "azure.yaml" {
		t.Errorf("yaml = %v", yaml)
	}
}

func TestOccurrenceAndFingerprint(t *testing.T) {
	f := sdk.Finding{RuleID: "FND-A-001", Severity: sdk.SeverityError, Evidence: "e", Fingerprint: "fp1:0000", Location: sdk.Location{File: "a.yaml", Line: 1}}
	g := f
	g.Location.Line = 2
	h := f
	h.Fingerprint = ""
	r := &sdk.Report{Findings: []sdk.Finding{f, g, h}}
	run := firstRun(t, render(t, sarif.Options{}, r))
	var hashes []string
	for _, res := range run["results"].([]any) {
		pf := res.(map[string]any)["partialFingerprints"].(map[string]any)
		hashes = append(hashes, pf["primaryLocationLineHash"].(string))
		if !strings.HasPrefix(pf["foundryDoctor/v1"].(string), "fp1:") && !strings.HasPrefix(pf["foundryDoctor/v1"].(string), "derived:") {
			t.Errorf("foundryDoctor/v1 = %v", pf["foundryDoctor/v1"])
		}
	}
	if len(hashes) != 3 {
		t.Fatal(hashes)
	}
	// The two findings sharing a fingerprint get occurrence 1 and 2 of the same hash.
	count := map[string][]string{}
	for _, h := range hashes {
		parts := strings.Split(h, ":")
		count[parts[0]] = append(count[parts[0]], parts[1])
	}
	found := false
	for _, occ := range count {
		if len(occ) == 2 && occ[0] == "1" && occ[1] == "2" {
			found = true
		}
	}
	if !found {
		t.Errorf("occurrence numbering wrong: %v", hashes)
	}
}

func TestTruncation(t *testing.T) {
	r := reporttest.Huge(30)
	out := render(t, sarif.Options{MaxResults: 10}, r)
	run := firstRun(t, out)
	results := run["results"].([]any)
	if len(results) != 10 {
		t.Fatalf("results = %d", len(results))
	}
	for _, res := range results {
		if res.(map[string]any)["level"] != "error" {
			t.Errorf("truncation kept a lower severity: %v", res.(map[string]any)["level"])
		}
	}
	notes := run["invocations"].([]any)[0].(map[string]any)["toolExecutionNotifications"].([]any)
	last := notes[len(notes)-1].(map[string]any)
	if last["level"] != "note" || !strings.Contains(last["message"].(map[string]any)["text"].(string), "truncated") {
		t.Errorf("truncation notification missing: %v", last)
	}
	if errs := checkGitHub(decode(t, out)); len(errs) > 0 {
		t.Error(errs)
	}
	// No truncation, no notification.
	run = firstRun(t, render(t, sarif.Options{MaxResults: 100}, r))
	if n := run["invocations"].([]any)[0].(map[string]any)["toolExecutionNotifications"].([]any); len(n) != 0 {
		t.Errorf("unexpected notifications: %v", n)
	}
}

func TestTruncationPrefersActive(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{
		{RuleID: "A", Severity: sdk.SeverityError, Baselined: true, Evidence: "b", Fingerprint: "fp1:1"},
		{RuleID: "B", Severity: sdk.SeverityInfo, Evidence: "a", Fingerprint: "fp1:2"},
	}}
	results := firstRun(t, render(t, sarif.Options{MaxResults: 1}, r))["results"].([]any)
	if len(results) != 1 || results[0].(map[string]any)["ruleId"] != "B" {
		t.Errorf("results = %v", results)
	}
}

func TestRuleLimits(t *testing.T) {
	long := strings.Repeat("\U0001F600", 2000) // 4000 UTF-16 units
	r := &sdk.Report{Findings: []sdk.Finding{{
		RuleID: strings.Repeat("R", 400), Severity: sdk.SeverityWarning, Recommendation: long, Evidence: "e",
		Pillar: "p", Category: sdk.CategoryMustHave, Basis: []string{"b1", "b2", "b3", "b4", "b5", "b6", "b7", "b8", "b9", "b10", "b11", "b12"},
		Fingerprint: "fp1:x",
	}}}
	log := decode(t, render(t, sarif.Options{}, r))
	if errs := checkGitHub(log); len(errs) > 0 {
		t.Fatal(errs)
	}
	rule := log["runs"].([]any)[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	if tags := rule["properties"].(map[string]any)["tags"].([]any); len(tags) != 10 {
		t.Errorf("tags = %d", len(tags))
	}
	if _, ok := rule["help"].(map[string]any)["markdown"]; !ok {
		t.Error("help.markdown missing")
	}
}

func TestRuleInfoOverride(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "FND-A-001", Severity: sdk.SeverityError, Evidence: "e", DocsURL: "https://example.com/d", Fingerprint: "fp1:z"}}}
	opts := sarif.Options{Rules: map[string]sarif.RuleInfo{"FND-A-001": {Title: "Title", Description: "Long <b>text</b>", Help: "Do *this*", HelpURI: "https://example.com/help"}}}
	rule := firstRun(t, render(t, opts, r))["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	if rule["shortDescription"].(map[string]any)["text"] != "Title" || rule["helpUri"] != "https://example.com/help" {
		t.Errorf("rule = %v", rule)
	}
	md := rule["help"].(map[string]any)["markdown"].(string)
	if !strings.Contains(md, `Do \*this\*`) || !strings.Contains(md, "(https://example.com/help)") {
		t.Errorf("markdown = %q", md)
	}
}

func TestEmptyReport(t *testing.T) {
	for _, r := range []*sdk.Report{nil, {}} {
		run := firstRun(t, render(t, sarif.Options{}, r))
		if len(run["results"].([]any)) != 0 || len(run["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)) != 0 {
			t.Errorf("run = %v", run)
		}
		if run["tool"].(map[string]any)["driver"].(map[string]any)["name"] != "foundry-doctor" {
			t.Error("default driver name")
		}
	}
}

func TestInvocationFailure(t *testing.T) {
	inv := firstRun(t, render(t, sarif.Options{}, &sdk.Report{ExitCode: sdk.ExitCannotRun}))["invocations"].([]any)[0].(map[string]any)
	if inv["executionSuccessful"] != false || inv["exitCode"] != float64(2) {
		t.Errorf("invocation = %v", inv)
	}
}

func TestSanitisedMessage(t *testing.T) {
	r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "A", Severity: sdk.SeverityInfo, Evidence: "x\x1b[31m y\x00z", Fingerprint: "fp1:q",
		Location: sdk.Location{File: `C:\abs\dir\a b.yaml`}}}}
	out := render(t, sarif.Options{}, r)
	res := firstRun(t, out)["results"].([]any)[0].(map[string]any)
	if res["message"].(map[string]any)["text"] != "x yz" {
		t.Errorf("message = %v", res["message"])
	}
	if uri := res["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["artifactLocation"].(map[string]any)["uri"]; uri != "a%20b.yaml" {
		t.Errorf("absolute path leaked or unescaped: %v", uri)
	}
}

func TestLevelAndRegionDefaults(t *testing.T) {
	if sarif.Level(sdk.SeverityError) != "error" || sarif.Level(sdk.SeverityWarning) != "warning" || sarif.Level(sdk.SeverityInfo) != "note" || sarif.Level("") != "warning" {
		t.Error("level mapping")
	}
	tests := []struct {
		loc  sdk.Location
		want sarif.Region
	}{
		{sdk.Location{}, sarif.Region{1, 1, 1, 1}},
		{sdk.Location{Line: 7}, sarif.Region{7, 1, 7, 2}},
		{sdk.Location{Line: 7, Column: 4}, sarif.Region{7, 4, 7, 5}},
		{sdk.Location{Line: 7, Column: 4, EndLine: 9}, sarif.Region{7, 4, 9, 1}},
		{sdk.Location{Line: 7, Column: 4, EndLine: 7, EndColumn: 2}, sarif.Region{7, 4, 7, 4}},
		{sdk.Location{Line: 7, Column: 1, EndLine: 3, EndColumn: 9}, sarif.Region{7, 1, 7, 9}},
	}
	for _, tc := range tests {
		r := &sdk.Report{Findings: []sdk.Finding{{RuleID: "A", Evidence: "e", Location: sdk.Location{File: "f", Line: tc.loc.Line, Column: tc.loc.Column, EndLine: tc.loc.EndLine, EndColumn: tc.loc.EndColumn}}}}
		res := firstRun(t, render(t, sarif.Options{}, r))["results"].([]any)[0].(map[string]any)
		reg := res["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["region"].(map[string]any)
		got := sarif.Region{int(reg["startLine"].(float64)), int(reg["startColumn"].(float64)), int(reg["endLine"].(float64)), int(reg["endColumn"].(float64))}
		if got != tc.want {
			t.Errorf("%+v: region %+v, want %+v", tc.loc, got, tc.want)
		}
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteError(t *testing.T) {
	if sarif.New(sarif.Options{}).Write(failWriter{}, &sdk.Report{}) == nil {
		t.Fatal("want error")
	}
}
