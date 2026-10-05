package catalog

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const goodVerified = `id: FND-SEC-001
version: 1
group: SEC
title: Foundry local authentication disabled
description: Local (key) authentication must be disabled on Foundry accounts.
status: verified
phases: ["1"]
inputs: [bicep-arm]
basis: [security]
category: must-have
pillar: security
severity: {dev: warning, test: error, prod: error}
compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: "2026-09-01"}
evidence: {description: "disableLocalAuth is true", confidence: certain}
recommendation: Disable local auth.
fix: Set the verified property in the deployment source.
sources:
  - url: https://learn.microsoft.com/example
    lastVerified: "2026-10-04"
overlap: {decision: native, coverage: partial, rationale: "No Foundry-specific check exists."}
implementation: {owner: native, package: internal/rules/sec}
tests: {positive: [local auth enabled], negative: [local auth disabled], skipped: [input unavailable]}
testability: Deterministic with the required input.
`

const seedSEC = "id: FND-SEC-001\nversion: 1\ngroup: SEC\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"

const seedProposed = "id: FND-CFG-001\nversion: 1\ngroup: CFG\ntitle: t\nstatus: proposed\nphases: [\"1\"]\n"

// with returns goodVerified with the first occurrence of old replaced by new, failing if old is absent.
func with(t *testing.T, old, new string) string {
	t.Helper()
	if !strings.Contains(goodVerified, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return strings.Replace(goodVerified, old, new, 1)
}

func mapFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func load(t *testing.T, files map[string]string) ([]Rule, error) {
	t.Helper()
	return Load(context.Background(), mapFS(files), "rules/catalog")
}

func one(content string) map[string]string {
	return map[string]string{"rules/catalog/sec/FND-SEC-001.yaml": content}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		opt     Options
		wantErr string // substring; empty means valid
	}{
		{"verified rule is valid", goodVerified, Options{}, ""},
		{"uncertain scenario is allowed", with(t, "skipped: [input unavailable]}", "skipped: [input unavailable], uncertain: [evidence is inconclusive]}"), Options{}, ""},
		{"proposed ok without gate", seedSEC, Options{}, ""},
		{"proposed fails phase0 gate", seedSEC, Options{Phase0Gate: true}, "still proposed"},
		{"verified without source", with(t, "sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", ""), Options{}, "needs at least one source"},
		{"malformed date", with(t, "2026-10-04", "20x6-1x-0y"), Options{}, "valid YYYY-MM-DD date"},
		{"impossible date", with(t, "2026-10-04", "2026-13-45"), Options{}, "valid YYYY-MM-DD date"},
		{"non-https source", with(t, "https://learn", "http://learn"), Options{}, "absolute https URL"},
		{"malformed source URL", with(t, "https://learn.microsoft.com/example", "https://"), Options{}, "absolute https URL"},
		{"source URL user information", with(t, "https://learn.microsoft.com/example", "https://user@learn.microsoft.com/example"), Options{}, "without user information"},
		{"unknown origin system", with(t, "overlap:", "origins: [{system: other, id: check-a}]\noverlap:"), Options{}, `origins[0].system must be "xf"`},
		{"missing origin id", with(t, "overlap:", "origins: [{system: xf, id: \"\"}]\noverlap:"), Options{}, "origins[0].id is required"},
		{"platform rule must be error everywhere", with(t, "basis: [security]", "basis: [platform]"), Options{}, "platform-basis rules must be error"},
		{"bad decision", with(t, "decision: native", "decision: maybe"), Options{}, "overlap.decision"},
		{"empty rationale", with(t, `rationale: "No Foundry-specific check exists."`, `rationale: ""`), Options{}, "overlap.rationale is required"},
		{"bad coverage", with(t, "coverage: partial", "coverage: lots"), Options{}, "overlap.coverage"},
		{"opinion needs opinion basis", with(t, "status: verified", "status: product-opinion"), Options{}, `basis "opinion"`},
		{"native decision needs native owner", with(t, "owner: native", "owner: psrule"), Options{}, "requires implementation.owner native"},
		{"wrap decision needs external owner", with(t, "decision: native", "decision: wrap"), Options{}, "requires an external"},
		{"group mismatch", with(t, "group: SEC", "group: NET"), Options{}, "does not match id group"},
		{"version zero", with(t, "version: 1", "version: 0"), Options{}, "version must be >= 1"},
		{"empty title", with(t, "title: Foundry local authentication disabled", `title: " "`), Options{}, "title is required"},
		{"unknown status", with(t, "status: verified", "status: maybe"), Options{}, "unknown status"},
		{"empty phases", with(t, `phases: ["1"]`, "phases: []"), Options{}, "phases is required"},
		{"unknown phase", with(t, `phases: ["1"]`, `phases: ["12"]`), Options{}, "unknown phase"},
		{"duplicate phase", with(t, `phases: ["1"]`, `phases: ["1", "1"]`), Options{}, "phases contains duplicate"},
		{"unknown input plane", with(t, "inputs: [bicep-arm]", "inputs: [telepathy]"), Options{}, "unknown input plane"},
		{"empty inputs", with(t, "inputs: [bicep-arm]", "inputs: []"), Options{}, "inputs is required"},
		{"unknown basis", with(t, "basis: [security]", "basis: [vibes]"), Options{}, "unknown basis"},
		{"unknown category", with(t, "category: must-have", "category: critical"), Options{}, "category must be"},
		{"unknown pillar", with(t, "pillar: security", "pillar: speed"), Options{}, "unknown pillar"},
		{"bad severity", with(t, "dev: warning", "dev: loud"), Options{}, "severity.dev"},
		{"bad confidence", with(t, "confidence: certain", "confidence: sure"), Options{}, "evidence.confidence"},
		{"empty evidence", with(t, `description: "disableLocalAuth is true"`, `description: ""`), Options{}, "evidence.description is required"},
		{"empty recommendation", with(t, "recommendation: Disable local auth.", `recommendation: ""`), Options{}, "recommendation is required"},
		{"empty fix", with(t, "fix: Set the verified property in the deployment source.", `fix: ""`), Options{}, "fix is required"},
		{"empty testability", with(t, "testability: Deterministic with the required input.", `testability: ""`), Options{}, "testability is required"},
		{"empty description", with(t, "description: Local (key) authentication must be disabled on Foundry accounts.", `description: ""`), Options{}, "description is required"},
		{"no positive test", with(t, "positive: [local auth enabled]", "positive: []"), Options{}, "tests.positive, tests.negative and tests.skipped"},
		{"no negative test", with(t, "negative: [local auth disabled]", "negative: []"), Options{}, "tests.positive, tests.negative and tests.skipped"},
		{"no skipped test", with(t, "skipped: [input unavailable]", "skipped: []"), Options{}, "tests.positive, tests.negative and tests.skipped"},
		{"no package", with(t, "package: internal/rules/sec", `package: ""`), Options{}, "implementation.package is required"},
		{"no compatibility", with(t, "compatibility:\n  apiVersions:\n    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: \"2026-09-01\"}", "compatibility: {}"), Options{}, "compatibility must name"},
		{"bad owner", with(t, "owner: native", "owner: nobody"), Options{}, "implementation.owner must be one of"},
		{"dropped needs drop decision", with(t, "status: verified", "status: dropped"), Options{}, "dropped rule must have overlap.decision drop"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := load(t, one(tc.content))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			err = Validate(rules, tc.opt)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateRejectsFutureLastVerified(t *testing.T) {
	rules, err := load(t, one(goodVerified))
	if err != nil {
		t.Fatal(err)
	}
	err = Validate(rules, Options{Now: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)})
	if err == nil || !strings.Contains(err.Error(), "must not be in the future") {
		t.Fatalf("error = %v", err)
	}
}

func TestVersionRangeSemantics(t *testing.T) {
	tests := []struct {
		name     string
		rng      VersionRange
		version  string
		valid    bool
		contains bool
	}{
		{"exact", VersionRange{Exact: "1.2.3"}, "1.2.3+build.4", true, true},
		{"inclusive prerelease endpoints", VersionRange{Minimum: "1.2.3-beta.1", Maximum: "1.2.3", IncludeMinimum: true, IncludeMaximum: true}, "1.2.3-beta.2", true, true},
		{"exclusive lower", VersionRange{Minimum: "1.2.3", Maximum: "2.0.0"}, "1.2.3", true, false},
		{"reversed", VersionRange{Minimum: "2.0.0", Maximum: "1.0.0"}, "1.5.0", false, false},
		{"equal excluded", VersionRange{Minimum: "1.0.0", Maximum: "1.0.0", IncludeMinimum: true}, "1.0.0", false, false},
		{"missing maximum", VersionRange{Minimum: "1.0.0"}, "1.0.0", false, false},
		{"invalid leading zero", VersionRange{Exact: "01.0.0"}, "1.0.0", false, false},
		{"invalid prerelease leading zero", VersionRange{Exact: "1.0.0-01"}, "1.0.0-1", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := validVersionRange(tc.rng); got != tc.valid {
				t.Fatalf("validVersionRange() = %v, want %v", got, tc.valid)
			}
			if got := tc.rng.Contains(tc.version); got != tc.contains {
				t.Fatalf("Contains(%q) = %v, want %v", tc.version, got, tc.contains)
			}
		})
	}
}

func TestStructuredVersionRangeLoadsAndUnknownFieldFails(t *testing.T) {
	structured := with(t, "compatibility:\n  apiVersions:\n    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: \"2026-09-01\"}", `compatibility:
  azd: {minimum: 1.2.3-beta.1, maximum: 1.2.3, includeMinimum: true, includeMaximum: true}
  preview: true`)
	rules, err := load(t, one(structured))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(rules, Options{}); err != nil || !rules[0].Compatibility.Azd.Contains("1.2.3-beta.2") {
		t.Fatalf("structured range: rules=%+v error=%v", rules, err)
	}
	_, err = load(t, one(strings.Replace(structured, "minimum:", "minimumTypo:", 1)))
	if err == nil || !strings.Contains(err.Error(), "minimumTypo") {
		t.Fatalf("unknown structured field error = %v", err)
	}
}

func TestAPIVersionValidation(t *testing.T) {
	const base = `compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: "2026-09-01"}`
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{"management", base, ""},
		{"management operation", `compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts/deployments, operation: listKeys, version: "2026-09-01"}`, ""},
		{"data", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes/documents, operation: query, version: "2026-09-01"}`, ""},
		{"preview", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "2026-09-01-preview"}
  preview: true`, ""},
		{"preview flag required", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "2026-09-01-preview"}`, "compatibility.preview must be true"},
		{"preview true remains allowed for stable", base + "\n  preview: true", ""},
		{"unknown plane", `compatibility:
  apiVersions:
    - {plane: control, provider: Microsoft.CognitiveServices, resourceType: accounts, version: "2026-09-01"}`, "plane must be management|data"},
		{"missing management provider", `compatibility:
  apiVersions:
    - {plane: management, resourceType: accounts, version: "2026-09-01"}`, "requires one provider"},
		{"missing management target", `compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.CognitiveServices, version: "2026-09-01"}`, "requires one resourceType"},
		{"management forbids data target", `compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, service: search, endpointFamily: indexes, version: "2026-09-01"}`, "forbids service and endpointFamily"},
		{"missing data service", `compatibility:
  apiVersions:
    - {plane: data, endpointFamily: indexes, version: "2026-09-01"}`, "requires one service"},
		{"missing data target", `compatibility:
  apiVersions:
    - {plane: data, service: search, version: "2026-09-01"}`, "requires one endpointFamily"},
		{"data forbids management target", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, provider: Microsoft.Search, resourceType: searchServices, version: "2026-09-01"}`, "forbids provider and resourceType"},
		{"free form provider", `compatibility:
  apiVersions:
    - {plane: management, provider: "Microsoft Search service", resourceType: accounts, version: "2026-09-01"}`, "requires one provider"},
		{"combined resources", `compatibility:
  apiVersions:
    - {plane: management, provider: Microsoft.Search, resourceType: "searchServices, indexes", version: "2026-09-01"}`, "requires one resourceType"},
		{"combined operations", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, operation: "create or update", version: "2026-09-01"}`, "one machine-readable operation"},
		{"range is not exact", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: ">=2026-09-01"}`, "version must be an exact"},
		{"malformed version", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "2026-9-1"}`, "version must be an exact"},
		{"impossible version", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "2026-02-30"}`, "version must be an exact"},
		{"zero year", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "0000-09-01"}`, "version must be an exact"},
		{"unknown suffix", `compatibility:
  apiVersions:
    - {plane: data, service: search, endpointFamily: indexes, version: "2026-09-01-beta"}`, "version must be an exact"},
		{"duplicate tuple", base + `
    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: "2026-09-01"}`, "duplicates apiVersions[0]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := load(t, one(with(t, base, tc.value)))
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			err = Validate(rules, Options{})
			if (tc.wantErr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestAPIVersionYAMLIsStrict(t *testing.T) {
	const prefix = "compatibility:\n  apiVersions:\n    - "
	tests := []struct {
		name    string
		mapping string
		want    string
	}{
		{"legacy scalar", `"Microsoft.CognitiveServices/accounts 2026-09-01"`, "cannot unmarshal"},
		{"bare version scalar", `"2026-09-01"`, "cannot unmarshal"},
		{"unknown nested field", `{plane: data, service: search, endpointFamily: indexes, apiVersion: "2026-09-01"}`, "field apiVersion not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, one(with(t,
				"compatibility:\n  apiVersions:\n    - {plane: management, provider: Microsoft.CognitiveServices, resourceType: accounts, version: \"2026-09-01\"}",
				prefix+tc.mapping)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSelectAPIVersionExact(t *testing.T) {
	management := APIVersion{
		Plane: APIPlaneManagement, Provider: "Microsoft.CognitiveServices",
		ResourceType: "accounts", Version: "2026-09-01",
	}
	managementOperation := management
	managementOperation.Operation = "listKeys"
	data := APIVersion{
		Plane: APIPlaneData, Service: "search", EndpointFamily: "indexes",
		Operation: "query", Version: "2026-09-01-preview",
	}
	versions := []APIVersion{management, managementOperation, data}
	tests := []struct {
		name   string
		target APIVersion
		want   bool
	}{
		{"management exact", management, true},
		{"operation exact", managementOperation, true},
		{"data exact", data, true},
		{"omitted operation is exact absence", management, true},
		{"operation is not wildcard", APIVersion{Plane: management.Plane, Provider: management.Provider, ResourceType: management.ResourceType, Operation: "delete", Version: management.Version}, false},
		{"plane mismatch", APIVersion{Plane: APIPlaneData, Service: "Microsoft.CognitiveServices", EndpointFamily: "accounts", Version: management.Version}, false},
		{"provider mismatch", APIVersion{Plane: management.Plane, Provider: "Microsoft.Search", ResourceType: management.ResourceType, Version: management.Version}, false},
		{"target mismatch", APIVersion{Plane: management.Plane, Provider: management.Provider, ResourceType: "accounts/deployments", Version: management.Version}, false},
		{"service mismatch", APIVersion{Plane: data.Plane, Service: "openai", EndpointFamily: data.EndpointFamily, Operation: data.Operation, Version: data.Version}, false},
		{"endpoint mismatch", APIVersion{Plane: data.Plane, Service: data.Service, EndpointFamily: "documents", Operation: data.Operation, Version: data.Version}, false},
		{"version mismatch", APIVersion{Plane: management.Plane, Provider: management.Provider, ResourceType: management.ResourceType, Version: "2026-08-01"}, false},
		{"partial target", APIVersion{Plane: APIPlaneManagement, Provider: management.Provider, Version: management.Version}, false},
		{"unknown plane", APIVersion{Plane: "control", Provider: management.Provider, ResourceType: management.ResourceType, Version: management.Version}, false},
		{"malformed version", APIVersion{Plane: management.Plane, Provider: management.Provider, ResourceType: management.ResourceType, Version: "latest"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SelectAPIVersion(versions, tc.target)
			if ok != tc.want {
				t.Fatalf("SelectAPIVersion() ok = %v, want %v (got %+v)", ok, tc.want, got)
			}
			if ok && got != tc.target {
				t.Fatalf("SelectAPIVersion() = %+v, want %+v", got, tc.target)
			}
			if methodGot, methodOK := (Compatibility{APIVersions: versions}).SelectAPIVersion(tc.target); methodOK != ok || methodGot != got {
				t.Fatalf("Compatibility.SelectAPIVersion() = (%+v, %v), want (%+v, %v)", methodGot, methodOK, got, ok)
			}
		})
	}
	if got, ok := SelectAPIVersion([]APIVersion{management, {}}, management); ok || got != (APIVersion{}) {
		t.Fatalf("invalid candidate must fail closed, got (%+v, %v)", got, ok)
	}
	if got, ok := SelectAPIVersion([]APIVersion{management, management}, management); ok || got != (APIVersion{}) {
		t.Fatalf("duplicate candidates must fail closed, got (%+v, %v)", got, ok)
	}
	if management.Matches(managementOperation) || !management.Matches(management) || (APIVersion{}).Matches(APIVersion{}) {
		t.Fatal("Matches must require valid, exact tuples including operation")
	}
}

func TestPerToolOverlapResearch(t *testing.T) {
	structured := with(t,
		"overlap: {decision: native, coverage: partial, rationale: \"No Foundry-specific check exists.\"}",
		`overlap:
  research:
    psrule: {state: searched-match, matches: [Azure.Example]}
    azurePolicy: {state: searched-none}
    defender: {state: unresearched}
    advisor: {state: unresearched}
    bicepLinter: {state: searched-none}
    checkov: {state: searched-none}
  decision: native
  provisional: true
  coverage: partial
  rationale: No Foundry-specific check exists.`)
	rules, err := load(t, one(structured))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(rules, Options{}); err != nil {
		t.Fatal(err)
	}
	o := rules[0].Overlap
	if got := o.ToolResearch("psrule"); got.State != ResearchSearchedMatch || !slices.Equal(got.Matches, []string{"Azure.Example"}) {
		t.Fatalf("psrule research = %+v", got)
	}
	if o.ToolResearch("defender").State != ResearchUnresearched ||
		o.ToolResearch("advisor").State != ResearchUnresearched || !o.DecisionIsProvisional() {
		t.Fatalf("empty Defender/Advisor must remain unresearched with a provisional decision: %+v", o)
	}
	matrix := OverlapMatrix(rules)
	for _, want := range []string{"Decision | Provisional |", "searched-match: `Azure.Example`", "searched-none", "unresearched"} {
		if !strings.Contains(matrix, want) {
			t.Fatalf("structured matrix lacks %q:\n%s", want, matrix)
		}
	}

	// The current rule packet remains loadable during migration. Only a
	// non-empty legacy list proves searched-match; an empty list is unknown.
	legacy := Overlap{Decision: "native", PSRule: []string{"Azure.Example"}}
	if got := legacy.ToolResearch("psrule"); got.State != ResearchSearchedMatch || len(got.Matches) != 1 {
		t.Fatalf("legacy match = %+v", got)
	}
	if legacy.ToolResearch("defender").State != ResearchUnresearched || !legacy.DecisionIsProvisional() {
		t.Fatalf("legacy empty tool state/provisional decision is wrong: %+v", legacy)
	}
}

func TestPerToolOverlapResearchRejectsInvalidAndUnknownInput(t *testing.T) {
	base := with(t,
		"overlap: {decision: native, coverage: partial, rationale: \"No Foundry-specific check exists.\"}",
		`overlap:
  research:
    psrule: {state: searched-none}
    azurePolicy: {state: searched-none}
    defender: {state: unresearched}
    advisor: {state: unresearched}
    bicepLinter: {state: searched-none}
    checkov: {state: searched-none}
  decision: native
  provisional: true
  coverage: partial
  rationale: No Foundry-specific check exists.`)
	tests := []struct {
		name, old, replacement, want string
	}{
		{"match missing IDs", "psrule: {state: searched-none}", "psrule: {state: searched-match}", "requires at least one match"},
		{"no-match has IDs", "psrule: {state: searched-none}", "psrule: {state: searched-none, matches: [x]}", "forbids matches"},
		{"unknown state", "psrule: {state: searched-none}", "psrule: {state: guessed}", "must be searched-match|searched-none|unresearched"},
		{"unknown nested field", "psrule: {state: searched-none}", "psrule: {state: searched-none, result: x}", "field result not found"},
		{"old scalar is rejected", `research:
    psrule: {state: searched-none}
    azurePolicy: {state: searched-none}
    defender: {state: unresearched}
    advisor: {state: unresearched}
    bicepLinter: {state: searched-none}
    checkov: {state: searched-none}`, "research: searched", "cannot unmarshal"},
		{"unresearched needs provisional decision", "  provisional: true\n", "", "provisional must be true"},
		{"new and old matches conflict", "  decision: native\n", "  psrule: [legacy]\n  decision: native\n", "legacy matches cannot be combined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := load(t, one(strings.Replace(base, tc.old, tc.replacement, 1)))
			if err == nil {
				err = Validate(rules, Options{})
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTestScenarioContractLoadsAllOutcomes(t *testing.T) {
	content := with(t, "tests: {positive: [local auth enabled], negative: [local auth disabled], skipped: [input unavailable]}",
		"tests: {positive: [violation produces finding], negative: [compliant produces no finding], skipped: [capability unavailable], uncertain: [evidence inconclusive]}")
	rules, err := load(t, one(content))
	if err != nil {
		t.Fatal(err)
	}
	got := rules[0].Tests
	if !slices.Equal(got.Positive, []string{"violation produces finding"}) ||
		!slices.Equal(got.Negative, []string{"compliant produces no finding"}) ||
		!slices.Equal(got.Skipped, []string{"capability unavailable"}) ||
		!slices.Equal(got.Uncertain, []string{"evidence inconclusive"}) {
		t.Fatalf("test outcome contract was not preserved: %+v", got)
	}
}

func TestOriginInventory(t *testing.T) {
	rules := []Rule{
		{ID: "FND-CFG-001", Origins: []Origin{{System: "xf", ID: "check-a"}}},
		{ID: "FND-CFG-002", Origins: []Origin{{System: "xf", ID: "check-b"}}},
	}
	if err := ValidateOriginInventory(rules, []Origin{{System: "xf", ID: "check-a"}, {System: "xf", ID: "check-b"}}); err != nil {
		t.Fatal(err)
	}
	err := ValidateOriginInventory(rules, []Origin{{System: "xf", ID: "check-a"}, {System: "xf", ID: "missing"}})
	if err == nil || !strings.Contains(err.Error(), "unreviewed origin xf:check-b") || !strings.Contains(err.Error(), "xf:missing is not mapped") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateOwnerRules(t *testing.T) {
	base := func(decision, owner string) string {
		return with(t, "decision: native", "decision: "+decision) // owner replaced next
	}
	tests := []struct {
		decision, owner string
		wantErr         string
	}{
		{"native", "native", ""},
		{"adapt", "native", ""},
		{"adapt", "psrule", "decision adapt requires implementation.owner native"},
		{"native", "psrule", "decision native requires implementation.owner native"},
		{"wrap", "bicep", ""},
		{"reuse", "psrule", ""},
		{"wrap", "native", "decision wrap requires an external"},
		{"reuse", "native", "decision reuse requires an external"},
	}
	for _, tc := range tests {
		t.Run(tc.decision+"/"+tc.owner, func(t *testing.T) {
			content := strings.Replace(base(tc.decision, tc.owner), "owner: native", "owner: "+tc.owner, 1)
			rules, err := load(t, one(content))
			if err != nil {
				t.Fatal(err)
			}
			err = Validate(rules, Options{})
			if (tc.wantErr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateStatusSpecific(t *testing.T) {
	// implemented needs a source just like verified
	noSource := strings.Replace(with(t, "status: verified", "status: implemented"),
		"sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", "", 1)
	rules, _ := load(t, one(noSource))
	if err := Validate(rules, Options{}); err == nil || !strings.Contains(err.Error(), "implemented rule needs at least one source") {
		t.Fatalf("error = %v", err)
	}
	// a dropped rule with decision drop is valid and skips the remaining checks
	dropped := "id: FND-SEC-001\nversion: 1\ngroup: SEC\ntitle: t\nstatus: dropped\nphases: [\"1\"]\n" +
		"overlap: {decision: drop, coverage: full, rationale: covered by an Azure Policy built-in}\n"
	rules, _ = load(t, one(dropped))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("dropped rule: %v", err)
	}
	// product-opinion needs no source
	opinion := strings.Replace(with(t, "status: verified", "status: product-opinion"), "basis: [security]", "basis: [opinion]", 1)
	opinion = strings.Replace(opinion, "sources:\n  - url: https://learn.microsoft.com/example\n    lastVerified: \"2026-10-04\"\n", "", 1)
	rules, _ = load(t, one(opinion))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("product-opinion rule: %v", err)
	}
	// a platform rule with error everywhere is valid
	platform := strings.Replace(with(t, "basis: [security]", "basis: [platform]"), "dev: warning", "dev: error", 1)
	rules, _ = load(t, one(platform))
	if err := Validate(rules, Options{}); err != nil {
		t.Fatalf("platform rule: %v", err)
	}
}

func TestValidateBadIDAndDuplicate(t *testing.T) {
	if err := Validate([]Rule{{ID: "nope"}}, Options{}); err == nil || !strings.Contains(err.Error(), "id must match") {
		t.Fatalf("error = %v", err)
	}
	r := Rule{ID: "FND-CFG-001", Version: 1, Group: "CFG", Title: "t", Status: StatusProposed, Phases: []string{"1"}}
	if err := Validate([]Rule{r, r}, Options{}); err == nil || !strings.Contains(err.Error(), "duplicate rule id") {
		t.Fatalf("error = %v, want duplicate rule id", err)
	}
	if err := Validate(nil, Options{}); err == nil {
		t.Fatal("empty catalogue must be an error")
	}
}

func TestValidateOutputIsDeterministic(t *testing.T) {
	bad := with(t, "severity: {dev: warning, test: error, prod: error}", "severity: {dev: x, test: y, prod: z}")
	rules, err := load(t, one(bad))
	if err != nil {
		t.Fatal(err)
	}
	first := Validate(rules, Options{}).Error()
	for i := 0; i < 50; i++ {
		if got := Validate(rules, Options{}).Error(); got != first {
			t.Fatalf("run %d differs:\n%s\n---\n%s", i, first, got)
		}
	}
	if !(strings.Index(first, "severity.dev") < strings.Index(first, "severity.test") && strings.Index(first, "severity.test") < strings.Index(first, "severity.prod")) {
		t.Fatalf("severity errors out of order:\n%s", first)
	}
}

func TestLoadInvalidContent(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"unknown field", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nbogus: 1\n"}, "field bogus not found"},
		{"duplicate key", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\nid: FND-CFG-001\n"}, "already defined"},
		{"file name mismatch", map[string]string{"rules/catalog/cfg/FND-CFG-002.yaml": "id: FND-CFG-001\n"}, "file name must equal rule id"},
		{"wrong directory", map[string]string{"rules/catalog/sec/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "must live in directory"},
		{"second document", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n---\nid: FND-CFG-002\n"}, "more than one YAML document"},
		{"empty file", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": ""}, "file is empty"},
		{"too deep", map[string]string{"rules/catalog/x/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "exactly <group>/<ID>.yaml"},
		{"too shallow", map[string]string{"rules/catalog/FND-CFG-001.yaml": "id: FND-CFG-001\n"}, "exactly <group>/<ID>.yaml"},
		{"stray file", map[string]string{"rules/catalog/cfg/notes.txt": "hello"}, "unexpected file"},
		{"yml extension", map[string]string{"rules/catalog/cfg/FND-CFG-001.yml": "id: FND-CFG-001\n"}, "unexpected file"},
		{"oversized file", map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: " + strings.Repeat("a", maxRuleFileBytes+1)}, "exceeds"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.files)
			var inv *InvalidError
			if err == nil || !strings.Contains(err.Error(), tc.want) || !errors.As(err, &inv) {
				t.Fatalf("error = %v (InvalidError=%v), want InvalidError containing %q", err, errors.As(err, &inv), tc.want)
			}
		})
	}
}

func TestLoadRejectsSymlink(t *testing.T) {
	m := mapFS(map[string]string{"rules/catalog/cfg/FND-CFG-001.yaml": "id: FND-CFG-001\n"})
	m["rules/catalog/cfg/FND-CFG-001.yaml"].Mode = fs.ModeSymlink
	_, err := Load(context.Background(), m, "rules/catalog")
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "symlinks are not allowed") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadIOErrorIsNotInvalidError(t *testing.T) {
	_, err := Load(context.Background(), fstest.MapFS{}, "rules/catalog")
	var inv *InvalidError
	if err == nil || errors.As(err, &inv) {
		t.Fatalf("missing root must be an I/O error, got %v", err)
	}
}

func TestLoadHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, mapFS(one(goodVerified)), "rules/catalog")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

type cancelAfterReadFS struct {
	fs.FS
	cancel context.CancelFunc
}

func (f cancelAfterReadFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return file, err
	}
	return &cancelAfterReadFile{File: file, cancel: f.cancel}, nil
}

type cancelAfterReadFile struct {
	fs.File
	cancel context.CancelFunc
}

func (f *cancelAfterReadFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.cancel()
	return n, err
}

func TestLoadHonoursCancellationAfterFileRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fsys := cancelAfterReadFS{FS: mapFS(one(goodVerified)), cancel: cancel}
	_, err := Load(ctx, fsys, "rules/catalog")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled after read", err)
	}
}

func TestLoadSortsByID(t *testing.T) {
	rules, err := load(t, map[string]string{
		"rules/catalog/sec/FND-SEC-001.yaml": goodVerified,
		"rules/catalog/cfg/FND-CFG-001.yaml": seedProposed,
	})
	if err != nil || len(rules) != 2 || rules[0].ID != "FND-CFG-001" {
		t.Fatalf("rules = %v, err = %v", rules, err)
	}
}

func TestParseID(t *testing.T) {
	for id, ok := range map[string]bool{"FND-CFG-001": true, "FND-COST-004": true, "FND-IQ-012": true,
		"FND-cfg-001": false, "FND-CFG-1": false, "XYZ-CFG-001": false, "FND-CFG-00A": false, "FND--001": false} {
		if _, got := parseID(id); got != ok {
			t.Errorf("parseID(%q) ok=%v, want %v", id, got, ok)
		}
	}
}
