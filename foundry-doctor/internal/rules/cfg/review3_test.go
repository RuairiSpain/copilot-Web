package cfg

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/baseline"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// namedEnv adds the optional selection views to fakeEnv.
type namedEnv struct {
	fakeEnv
	byName   bool
	expanded map[string]bool
}

func (n namedEnv) SelectedByName() bool          { return n.byName }
func (n namedEnv) ExpandedKeys() map[string]bool { return n.expanded }

func refYAML(ref string) string {
	return "services:\n  a:\n    host: azure.ai.agent\n    env:\n      X: " + ref + "\n"
}

// N2-8: azd-defined names are not guaranteed producers.
func TestCFG006StandardNamesAreUncertain(t *testing.T) {
	arm := armOutStub{[]string{"OTHER"}}
	plain := func(vals map[string]string) Providers { return Providers{Env: fakeEnv{true, vals}} }
	named := func(vals map[string]string, byName bool) Providers {
		return Providers{Env: namedEnv{fakeEnv: fakeEnv{true, vals}, byName: byName}}
	}
	runReviewCases(t, []reviewCase{
		{name: "subscription id absent is uncertain", id: "FND-CFG-006", yaml: refYAML("${AZURE_SUBSCRIPTION_ID}"), p: plain(nil), arm: arm, skip: uncertainPrefix + "${AZURE_SUBSCRIPTION_ID}"},
		{name: "resource group absent is uncertain", id: "FND-CFG-006", yaml: refYAML("${AZURE_RESOURCE_GROUP}"), p: plain(nil), arm: arm, skip: uncertainPrefix + "${AZURE_RESOURCE_GROUP}"},
		{name: "env name absent is uncertain without selection by name", id: "FND-CFG-006", yaml: refYAML("${AZURE_ENV_NAME}"), p: plain(nil), arm: arm, skip: uncertainPrefix + "${AZURE_ENV_NAME}"},
		{name: "env name absent is uncertain when selected by default", id: "FND-CFG-006", yaml: refYAML("${AZURE_ENV_NAME}"), p: named(nil, false), arm: arm, skip: uncertainPrefix + "${AZURE_ENV_NAME}"},
		{name: "env name absent passes when selected by name", id: "FND-CFG-006", yaml: refYAML("${AZURE_ENV_NAME}"), p: named(nil, true), arm: arm},
		{name: "env name empty stays uncertain even when selected by name", id: "FND-CFG-006", yaml: refYAML("${AZURE_ENV_NAME}"), p: named(map[string]string{"AZURE_ENV_NAME": ""}, true), arm: arm, skip: "with an empty value"},
		{name: "by name does not make subscription id a producer", id: "FND-CFG-006", yaml: refYAML("${AZURE_SUBSCRIPTION_ID}"), p: named(nil, true), arm: arm, skip: uncertainPrefix},
		{name: "present values pass", id: "FND-CFG-006", yaml: refYAML("${AZURE_SUBSCRIPTION_ID}-${AZURE_RESOURCE_GROUP}-${AZURE_ENV_NAME}"),
			p: plain(map[string]string{"AZURE_SUBSCRIPTION_ID": "s", "AZURE_RESOURCE_GROUP": "r", "AZURE_ENV_NAME": "e"}), arm: arm},
	})
}

// Task 4: only ${VAR} and the documented ${VAR:-default} are decided.
func TestCFG006UnusualSubstitutionForms(t *testing.T) {
	arm := armOutStub{[]string{"OTHER"}}
	p := Providers{Env: fakeEnv{true, nil}}
	runReviewCases(t, []reviewCase{
		{name: "error form is uncertain not undefined", id: "FND-CFG-006", yaml: refYAML("${NOPE:?must be set}"), p: p, arm: arm, skip: uncertainPrefix + "${NOPE:?...}"},
		{name: "alternate form is uncertain", id: "FND-CFG-006", yaml: refYAML("${NOPE:+x}"), p: p, arm: arm, skip: uncertainPrefix + "${NOPE:+...}"},
		{name: "assign form is uncertain", id: "FND-CFG-006", yaml: refYAML("${NOPE=x}"), p: p, arm: arm, skip: uncertainPrefix + "${NOPE=...}"},
		{name: "default form is decided and fine", id: "FND-CFG-006", yaml: refYAML("${NOPE:-x}"), p: p, arm: arm},
		{name: "empty default is decided and fine", id: "FND-CFG-006", yaml: refYAML("${NOPE:-}"), p: p, arm: arm},
		{name: "escaped form is not a reference", id: "FND-CFG-006", yaml: refYAML("$${NOPE:?x}"), p: p, arm: arm},
		{name: "server side expression is not a reference", id: "FND-CFG-006", yaml: refYAML("${{ NOPE:?x }}"), p: p, arm: arm},
		{name: "a real undefined name beside an unusual form still fails", id: "FND-CFG-006", yaml: refYAML("${NOPE:?x}-${GONE}"), p: p, arm: arm, want: 1, contains: "GONE"},
		{name: "error form never leaks its message", id: "FND-CFG-006", yaml: refYAML("${NOPE:?top-secret-text}"), p: p, arm: arm, skip: uncertainPrefix, notInSkip: "top-secret-text"},
	})
	for in, want := range map[string][]string{
		"${A:?m}":          {"A:?"},
		"${A:+x}${B-y}":    {"A:+", "B-"},
		"${A:-x}${B}":      nil,
		"${A:-}":           nil,
		"$${A:?x}":         nil,
		"${{A:?x}}":        nil,
		"${1A:?x}":         nil,
		"${A#x}${C%y}":     {"A#", "C%"},
		"${A:=x}":          {"A:="},
		"${A:?unfinished":  nil,
		"plain ${A:?m} ok": {"A:?"},
	} {
		got := unusualForms(in)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("unusualForms(%q) = %v want %v", in, got, want)
		}
	}
}

// Task 5: a value built by variable expansion is not judged.
func TestCFG012ExpandedValueIsUncertain(t *testing.T) {
	mk := func(v string, expanded bool) Providers {
		x := map[string]bool{}
		if expanded {
			x["AZURE_LOCATION"] = true
		}
		return Providers{Env: namedEnv{fakeEnv: fakeEnv{true, map[string]string{"AZURE_LOCATION": v}}, expanded: x}}
	}
	runReviewCases(t, []reviewCase{
		{name: "empty from an undefined expansion is uncertain", id: "FND-CFG-012", yaml: "services: {}\n", p: mk("", true), skip: uncertainPrefix + "AZURE_LOCATION is defined by variable expansion"},
		{name: "mixed case from expansion is uncertain", id: "FND-CFG-012", yaml: "services: {}\n", p: mk("EastUS", true), skip: uncertainPrefix + "AZURE_LOCATION is defined by variable expansion"},
		{name: "plain empty value is still a finding", id: "FND-CFG-012", yaml: "services: {}\n", p: mk("", false), want: 1, contains: "empty"},
		{name: "expansion that gives a lowercase region is still uncertain", id: "FND-CFG-012", yaml: "services: {}\n", p: mk("eastus", true), skip: uncertainPrefix},
		{name: "no expansion lowercase passes", id: "FND-CFG-012", yaml: "services: {}\n", p: mk("eastus", false)},
	})
}

// Task 1: a bare input-unavailable skip names the missing input.
func TestMissingInputSkipsNameTheInput(t *testing.T) {
	for _, r := range Register() {
		for name, in := range map[string]*sdk.Input{"nil input": nil, "no azure.yaml": {}} {
			res, err := r.Evaluate(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			if res.Skipped == nil {
				continue
			}
			if res.Skipped.Reason == sdk.SkipInputUnavailable {
				t.Errorf("%s %s: bare %q", r.ID(), name, res.Skipped.Reason)
			}
			if strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable) && !strings.HasPrefix(res.Skipped.Reason, sdk.SkipInputUnavailable+": ") {
				t.Errorf("%s %s: head not followed by a named input: %q", r.ID(), name, res.Skipped.Reason)
			}
		}
	}
}

// m5: azd decodes only the first YAML document.
func TestCFG001MultipleDocumentsAreNotAnError(t *testing.T) {
	y := "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    project: src\n    kind: hosted\n---\nname: other\n"
	res := run(t, find(Register(), "FND-CFG-001"), &sdk.Input{AzureYAML: doc(t, y)})
	if len(res.Findings) != 0 {
		t.Fatalf("a second document must not fail the check: %+v", res.Findings)
	}
	if res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "multiple YAML documents") || !strings.HasPrefix(res.Skipped.Reason, uncertainPrefix) {
		t.Fatalf("want an uncertain note about the extra document, got %+v", res.Skipped)
	}
	// A real finding is reported and wins over the note.
	res = run(t, find(Register(), "FND-CFG-001"), &sdk.Input{AzureYAML: doc(t, "name: my_app\n---\nname: x\n")})
	if len(res.Findings) == 0 {
		t.Fatal("name format finding lost")
	}
}

// Duplicate-key findings keep their identity when an unrelated duplicate is
// inserted above them.
func TestDuplicateKeyFingerprintsAreStable(t *testing.T) {
	base := "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    host: azure.ai.agent\n    project: x\n    kind: hosted\n  b:\n    host: azure.ai.agent\n    host: azure.ai.agent\n    project: x\n    kind: hosted\n"
	inserted := "name: app\nname: app\nservices:\n  z:\n    host: azure.ai.agent\n    host: azure.ai.agent\n    project: x\n    kind: hosted\n  a:\n    host: azure.ai.agent\n    host: azure.ai.agent\n    project: x\n    kind: hosted\n  b:\n    host: azure.ai.agent\n    host: azure.ai.agent\n    project: x\n    kind: hosted\n"
	collect := func(y string) map[string]string {
		out := map[string]string{}
		for _, f := range evalFindings(t, "FND-CFG-001", y, Providers{}, nil) {
			if !strings.HasPrefix(f.Key, "duplicate-key:") {
				continue
			}
			if _, dup := out[f.Key]; dup {
				t.Fatalf("two findings share key %q", f.Key)
			}
			out[f.Key] = fp(f, "FND-CFG-001")
		}
		return out
	}
	before, after := collect(base), collect(inserted)
	if len(before) != 2 || len(after) != 4 {
		t.Fatalf("before=%v after=%v", before, after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Errorf("fingerprint of %q changed when a duplicate was inserted above it", k)
		}
	}
}

// CFG-005 names: FILE_SECRET is a credential, SECRET_COUNT is a quantity.
func TestCFG005FileSecretAndSecretCount(t *testing.T) {
	env := func(k string) string {
		return "services:\n  a:\n    host: azure.ai.agent\n    env:\n      " + k + ": plainvalue\n"
	}
	runReviewCases(t, []reviewCase{
		{name: "FILE_SECRET names a credential", id: "FND-CFG-005", yaml: env("FILE_SECRET"), skip: uncertainPrefix + "a: env.FILE_SECRET"},
		{name: "SECRET_COUNT is a quantity", id: "FND-CFG-005", yaml: env("SECRET_COUNT")},
		{name: "SECRET_FILE is a path", id: "FND-CFG-005", yaml: env("SECRET_FILE")},
		{name: "FILE_SECRET_COUNT is a quantity", id: "FND-CFG-005", yaml: env("FILE_SECRET_COUNT")},
	})
}

// A fingerprint on an azure.yaml-located finding survives a line shift, and a
// baseline written before the shift still hides it.
func TestBaselineRoundTripAcrossLineShift(t *testing.T) {
	y := "name: app\nservices:\n  a:\n    host: azure.ai.agent\n    kind: prompt\n    env:\n      X: " + "http://localhost:8080/x" + "\n"
	shifted := "# a new leading comment\n# and another\n" + y
	get := func(src string) []sdk.Finding {
		var all []sdk.Finding
		for _, id := range []string{"FND-CFG-007", "FND-CFG-001", "FND-CFG-004"} {
			for _, f := range evalFindings(t, id, src, Providers{}, nil) {
				f.RuleID, f.RuleVersion = id, 1
				f.Fingerprint = fp(f, id)
				all = append(all, f)
			}
		}
		return all
	}
	before, after := get(y), get(shifted)
	if len(before) == 0 || len(before) != len(after) {
		t.Fatalf("before=%d after=%d", len(before), len(after))
	}
	if before[0].Location.Line == after[0].Location.Line {
		t.Fatal("the shift did not move the finding")
	}
	b := baseline.FromFindings(before)
	data, err := b.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := baseline.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	res := parsed.Apply(after)
	if res.Baselined != len(after) || len(res.Stale) != 0 {
		t.Fatalf("baselined=%d of %d stale=%v", res.Baselined, len(after), res.Stale)
	}
	for _, f := range after {
		if !f.Baselined {
			t.Errorf("finding %s %s not baselined after the line shift", f.RuleID, f.Key)
		}
	}
}
