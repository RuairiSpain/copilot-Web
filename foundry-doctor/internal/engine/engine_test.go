package engine_test

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type stamper struct{}

func (stamper) Fingerprint(f sdk.Finding) string {
	return "fp:" + f.RuleID + "|" + f.Resource.Name + "|" + f.Key
}
func (stamper) Redact(s string) string { return strings.ReplaceAll(s, "SECRET", "[redacted]") }

func fnd(rule string, sev sdk.Severity, name string) sdk.Finding {
	f := sdk.Finding{RuleID: rule, RuleVersion: 1, Severity: sev, Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "T", Name: name}, Evidence: "e-" + name}
	f.Fingerprint = stamper{}.Fingerprint(f)
	return f
}

func TestExitCodePrecedence(t *testing.T) {
	errF := []sdk.Finding{fnd("FND-SEC-001", sdk.SeverityError, "a")}
	warnF := []sdk.Finding{fnd("FND-SEC-001", sdk.SeverityWarning, "a")}
	suppressed := fnd("FND-SEC-001", sdk.SeverityError, "a")
	suppressed.Suppressed = &sdk.Suppression{Reason: "r", Expires: "2099-01-01"}
	baselined := fnd("FND-SEC-001", sdk.SeverityError, "a")
	baselined.Baselined = true
	tests := []struct {
		name                        string
		findings                    []sdk.Finding
		skipped                     int
		failOn                      sdk.Severity
		strict, cannotRun, internal bool
		want                        int
	}{
		{"clean", nil, 0, "", false, false, false, 0},
		{"error finding", errF, 0, "", false, false, false, 1},
		{"warning below default threshold", warnF, 0, "", false, false, false, 0},
		{"warning with fail-on warning", warnF, 0, sdk.SeverityWarning, false, false, false, 1},
		{"suppressed ignored", []sdk.Finding{suppressed}, 0, "", false, false, false, 0},
		{"baselined ignored", []sdk.Finding{baselined}, 0, "", false, false, false, 0},
		{"skipped alone is fine", nil, 3, "", false, false, false, 0},
		{"skipped strict", nil, 3, "", true, false, false, 3},
		{"strict without skips", nil, 0, "", true, false, false, 0},
		{"findings beat strict", errF, 3, "", true, false, false, 1},
		{"cannot run beats findings", errF, 3, "", true, true, false, 2},
		{"internal beats all", errF, 3, "", true, true, true, 4},
		{"invalid fail-on falls back to error", warnF, 0, "bogus", false, false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.ExitCode(tc.findings, tc.skipped, tc.failOn, tc.strict, tc.cannotRun, tc.internal); got != tc.want {
				t.Fatalf("exit %d want %d", got, tc.want)
			}
		})
	}
}

func TestAssemble(t *testing.T) {
	sup := fnd("FND-SEC-003", sdk.SeverityError, "s")
	sup.Suppressed = &sdk.Suppression{Reason: "r", Expires: "2099-01-01"}
	base := fnd("FND-SEC-004", sdk.SeverityInfo, "b")
	base.Baselined = true
	in := engine.AssembleInput{
		Tool: sdk.ToolInfo{Name: "foundry-doctor", Version: "0.0.0"}, Profile: rules.ProfileProd,
		Findings: []sdk.Finding{fnd("FND-SEC-002", sdk.SeverityWarning, "w"), fnd("FND-SEC-001", sdk.SeverityInfo, "i"), fnd("FND-SEC-001", sdk.SeverityError, "e"), sup, base},
		Skipped: []sdk.SkippedCheck{
			{RuleID: "FND-NET-001", RuleVersion: 1, Reason: rules.ReasonSyntheticInfrastructure},
			{RuleID: "FND-CFG-001", RuleVersion: 1, Reason: rules.ReasonSyntheticInfrastructure},
			{RuleID: "FND-OPS-004", RuleVersion: 1, Reason: rules.ReasonProfileKeyMissing(model.KeyTagsRequired)},
		},
		Passed: 7, MinSeverity: sdk.SeverityWarning,
		Tools: []sdk.ToolStatus{{Name: "psrule", Path: `C:\tools\bin\psrule.exe`, State: sdk.ToolAvailable}, {Name: "bicep", Path: "/usr/local/bin/bicep", State: sdk.ToolAvailable}},
	}
	r := engine.Assemble(in)
	if r.SchemaVersion != sdk.ReportSchemaVersion || r.Profile != rules.ProfileProd || r.ExitCode != 1 {
		t.Errorf("header %+v", r)
	}
	var order []string
	for _, f := range r.Findings {
		order = append(order, f.RuleID+"/"+f.Resource.Name)
	}
	if want := []string{"FND-SEC-001/e", "FND-SEC-002/w", "FND-SEC-003/s"}; !slices.Equal(order, want) {
		t.Errorf("findings %v want %v (info filtered, sorted)", order, want)
	}
	wantSum := sdk.Summary{Warning: 1, Error: 1, Suppressed: 1, Baselined: 1, Skipped: 3, Passed: 7, Hidden: 1,
		SkippedByReason: map[string]int{"synthetic-infrastructure": 2, "profile-key-missing:policy.tags.required": 1}}
	if !reflect.DeepEqual(r.Summary, wantSum) {
		t.Errorf("summary %+v\nwant %+v", r.Summary, wantSum)
	}
	if r.Skipped[0].RuleID != "FND-CFG-001" || r.Skipped[2].RuleID != "FND-OPS-004" {
		t.Errorf("skipped not sorted: %v", r.Skipped)
	}
	if r.Tools[0].Name != "bicep" || r.Tools[0].Path != "bicep" || r.Tools[1].Path != "psrule.exe" {
		t.Errorf("tools %+v", r.Tools)
	}
	if r.EffectivePolicy == nil {
		t.Error("effective policy must be non-nil")
	}
	// Determinism: same input, byte-identical JSON; and the input is not reordered in place.
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(engine.Assemble(in))
	if string(a) != string(b) {
		t.Error("report not deterministic")
	}
}

func TestAssembleEmptyIsNonNil(t *testing.T) {
	r := engine.Assemble(engine.AssembleInput{Profile: rules.ProfileDev})
	b, _ := json.Marshal(r)
	for _, want := range []string{`"findings":[]`, `"skipped":[]`, `"tools":[]`, `"skippedByReason":{}`, `"effectivePolicy":{}`, `"exitCode":0`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON lacks %s: %s", want, b)
		}
	}
}

func TestAssembleExitFromToolsAndFlags(t *testing.T) {
	missing := engine.AssembleInput{Tools: []sdk.ToolStatus{{Name: "psrule", State: sdk.ToolMissing, Required: true}}}
	if got := engine.Assemble(missing).ExitCode; got != sdk.ExitCannotRun {
		t.Errorf("required missing tool: exit %d", got)
	}
	optional := engine.AssembleInput{Tools: []sdk.ToolStatus{{Name: "psrule", State: sdk.ToolMissing}}}
	if got := engine.Assemble(optional).ExitCode; got != 0 {
		t.Errorf("optional missing tool: exit %d", got)
	}
	strict := engine.AssembleInput{Strict: true, Skipped: []sdk.SkippedCheck{{RuleID: "FND-NET-001", Reason: "x"}}}
	if got := engine.Assemble(strict).ExitCode; got != sdk.ExitSkippedStrict {
		t.Errorf("strict skip: exit %d", got)
	}
	if got := engine.Assemble(engine.AssembleInput{Internal: true, CannotRun: true}).ExitCode; got != sdk.ExitInternal {
		t.Errorf("internal: exit %d", got)
	}
	// --min-severity does not change the exit code.
	hidden := engine.AssembleInput{MinSeverity: sdk.SeverityError, FailOn: sdk.SeverityInfo, Findings: []sdk.Finding{fnd("FND-SEC-001", sdk.SeverityInfo, "a")}}
	if r := engine.Assemble(hidden); r.ExitCode != 1 || len(r.Findings) != 0 || r.Summary.Hidden != 1 {
		t.Errorf("hidden finding must still fail --fail-on info: %+v", r)
	}
}

func TestEffectivePolicy(t *testing.T) {
	empty := engine.EffectivePolicy(model.Policy{})
	if empty[model.KeyResourceScope] != "same-resource-group" || empty[model.KeyNetworkPublicAccess] != "forbidden" ||
		empty[model.KeyMonitoringPublicTelemetry] != false || empty[model.KeyLogRetentionMinimumDays] != nil ||
		empty[model.KeyTagsRequired] != nil || empty[model.KeyTagsResourceTypes] != "rule-default" {
		t.Errorf("baselines: %v", empty)
	}
	if got, ok := empty[model.KeyAllowedExternalScopes].([]string); !ok || len(got) != 0 {
		t.Errorf("allowedExternalScopes baseline %v", empty[model.KeyAllowedExternalScopes])
	}
	days, rc, thr := 90, true, 400
	scope := model.ResidencyGeography
	pa := model.PublicEntraOnly
	set := engine.EffectivePolicy(model.Policy{
		LogRetention:         model.PolicyLogRetention{MinimumDays: &days},
		DataResidency:        model.PolicyDataResidency{Scope: &scope, DeploymentSkus: []string{"Standard"}},
		Network:              model.PolicyNetwork{PublicAccess: &pa},
		Tags:                 model.PolicyTags{Required: []model.TagRequirement{{Name: "owner"}, {Name: "env", Format: "^p"}}, ResourceTypes: []string{"a/b"}},
		Knowledge:            model.PolicyKnowledge{RequireDocumentLevelAccess: &rc},
		Cost:                 model.PolicyCost{DevMaxCosmosThroughput: &thr},
		ManagedByAzurePolicy: []model.ManagedBy{model.ManagedDiagnosticSettings},
	})
	if set[model.KeyLogRetentionMinimumDays] != 90 || set[model.KeyDataResidencyScope] != "geography" || set[model.KeyNetworkPublicAccess] != "entra-only" ||
		set[model.KeyCostDevMaxCosmosThroughput] != 400 || set[model.KeyKnowledgeRequireDocLevelAccess] != true {
		t.Errorf("set values: %v", set)
	}
	req := set[model.KeyTagsRequired].([]any)
	if len(req) != 2 || req[1].(map[string]any)["format"] != "^p" {
		t.Errorf("tags %v", req)
	}
	if !reflect.DeepEqual(set[model.KeyManagedByAzurePolicy], []string{"diagnostic-settings"}) || !reflect.DeepEqual(set[model.KeyTagsResourceTypes], []string{"a/b"}) ||
		!reflect.DeepEqual(set[model.KeyDataResidencyDeploymentSkus], []string{"Standard"}) {
		t.Errorf("lists %v", set)
	}
	if _, err := json.Marshal(set); err != nil {
		t.Fatal(err)
	}
}

func TestSystemDiagnosticsTable(t *testing.T) {
	ds := engine.SystemDiagnostics()
	if len(ds) != 6 {
		t.Fatalf("ADR-009 lists 6 engine IDs, have %d", len(ds))
	}
	var ids []string
	for _, d := range ds {
		ids = append(ids, d.ID)
		if !strings.HasPrefix(d.ID, "FND-SYS-") || d.Meaning == "" || !d.Severity.Valid() {
			t.Errorf("bad entry %+v", d)
		}
	}
	if !slices.IsSorted(ids) {
		t.Error("table must be in ID order")
	}
	for id, want := range map[string]sdk.Severity{
		engine.DiagSuppressionExpired: sdk.SeverityError, engine.DiagSuppressionInvalid: sdk.SeverityError, engine.DiagInvalidAzureYAML: sdk.SeverityError,
		engine.DiagUnsupportedHost: sdk.SeverityWarning, engine.DiagAdapterUnavailable: sdk.SeverityInfo, engine.DiagDuplicateFingerprint: sdk.SeverityWarning,
	} {
		if got, ok := engine.SystemSeverity(id); !ok || got != want {
			t.Errorf("%s: %s %v", id, got, ok)
		}
	}
	if _, ok := engine.SystemSeverity("FND-SYS-INTERNAL-ERROR"); ok {
		t.Error("unlisted ID must not be known")
	}
	// The catalogue validator keeps rejecting FND-SYS (ADR-009 decision 2).
	if err := catalog.Validate([]catalog.Rule{{ID: "FND-SYS-EXPIRED", Version: 1, Group: "SYS", Title: "t", Status: catalog.StatusProposed, Phases: []string{"1"}}}, catalog.Options{}); err == nil {
		t.Error("catalogue accepted an FND-SYS id")
	}
}

func TestNewDiagnostic(t *testing.T) {
	st := stamper{}
	f, err := engine.NewDiagnostic(st, rules.ProfileProd, engine.Diagnostic{ID: engine.DiagUnsupportedHost, Severity: sdk.SeverityError, Evidence: "host SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	if f.Severity != sdk.SeverityWarning || f.Category != "" || f.Basis != nil || f.Evidence != "host [redacted]" || f.Fingerprint == "" || f.Confidence != sdk.ConfidenceCertain || f.Profile != rules.ProfileProd {
		t.Errorf("%+v", f)
	}
	b, err := engine.NewDiagnostic(st, rules.ProfileDev, engine.Diagnostic{ID: "bicep/BCP035", Severity: sdk.SeverityInfo})
	if err != nil || b.Severity != sdk.SeverityInfo {
		t.Errorf("bicep: %+v %v", b, err)
	}
	for _, d := range []engine.Diagnostic{
		{ID: "FND-SYS-MADE-UP"}, {ID: "FND-SEC-001"}, {ID: "bicep/"}, {ID: "bicep/BCP1", Severity: "loud"},
	} {
		if _, err := engine.NewDiagnostic(st, rules.ProfileDev, d); err == nil {
			t.Errorf("%q accepted", d.ID)
		}
	}
	if _, err := engine.NewDiagnostic(nil, rules.ProfileDev, engine.Diagnostic{ID: engine.DiagUnsupportedHost}); err == nil {
		t.Error("nil stamper accepted")
	}
}

func TestFromModelDiagnostic(t *testing.T) {
	st := stamper{}
	f, err := engine.FromModelDiagnostic(st, rules.ProfileDev, model.Diagnostic{Source: "bicep", Code: "BCP035", Severity: sdk.SeverityWarning, Message: "m", File: "infra/main.bicep", Pos: model.Pos{Line: 3, Column: 4}})
	if err != nil || f.RuleID != "bicep/BCP035" || f.Severity != sdk.SeverityWarning || f.Location.Line != 3 || f.Location.File != "infra/main.bicep" {
		t.Errorf("bicep: %+v %v", f, err)
	}
	y, err := engine.FromModelDiagnostic(st, rules.ProfileDev, model.Diagnostic{Source: "azureyaml", Code: "invalid-azure-yaml", Severity: sdk.SeverityInfo})
	if err != nil || y.RuleID != engine.DiagInvalidAzureYAML || y.Severity != sdk.SeverityError {
		t.Errorf("yaml: %+v %v", y, err)
	}
	for _, d := range []model.Diagnostic{{Source: "bicep"}, {Source: "azureyaml", Code: "weird"}} {
		if _, err := engine.FromModelDiagnostic(st, rules.ProfileDev, d); err == nil {
			t.Errorf("%+v accepted", d)
		}
	}
}

func TestSuppressionDiagnostics(t *testing.T) {
	today := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		s     sdk.Suppression
		state engine.SuppressionState
		id    string
	}{
		{"active", sdk.Suppression{Reason: "r", Expires: "2026-12-01"}, engine.SuppressionActive, ""},
		{"active on expiry day", sdk.Suppression{Reason: "r", Expires: "2026-10-05"}, engine.SuppressionActive, ""},
		{"expired yesterday", sdk.Suppression{Reason: "r", Expires: "2026-10-04", Source: ".foundry-doctor/suppressions.yaml"}, engine.SuppressionExpired, engine.DiagSuppressionExpired},
		{"no reason", sdk.Suppression{Reason: " ", Expires: "2026-12-01"}, engine.SuppressionInvalid, engine.DiagSuppressionInvalid},
		{"no expiry", sdk.Suppression{Reason: "r"}, engine.SuppressionInvalid, engine.DiagSuppressionInvalid},
		{"malformed expiry", sdk.Suppression{Reason: "r", Expires: "5/10/2026"}, engine.SuppressionInvalid, engine.DiagSuppressionInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if st, _ := engine.CheckSuppression(tc.s, today); st != tc.state {
				t.Fatalf("state %v want %v", st, tc.state)
			}
			f, ok, err := engine.SuppressionDiagnostic(stamper{}, rules.ProfileProd, tc.s, "FND-SEC-001@fp1:abc", today)
			if err != nil {
				t.Fatal(err)
			}
			if tc.id == "" {
				if ok {
					t.Fatal("active suppression produced a diagnostic")
				}
				return
			}
			if !ok || f.RuleID != tc.id || f.Severity != sdk.SeverityError || f.Key != "FND-SEC-001@fp1:abc" || f.Resource.Name != tc.s.Source {
				t.Errorf("%+v", f)
			}
		})
	}
	if _, _, err := engine.SuppressionDiagnostic(nil, rules.ProfileProd, sdk.Suppression{}, "k", today); err == nil {
		t.Error("nil stamper accepted")
	}
}

func TestMergeFingerprints(t *testing.T) {
	a := fnd("FND-SEC-001", sdk.SeverityError, "a")
	dup := a // identical evidence: merged silently
	clash := a
	clash.Evidence = "different evidence"
	other := fnd("FND-SEC-002", sdk.SeverityError, "b")

	merged, diags, err := engine.MergeFingerprints(stamper{}, rules.ProfileDev, []sdk.Finding{other, a, dup})
	if err != nil || len(merged) != 2 || len(diags) != 0 {
		t.Fatalf("identical findings must merge: %d %d %v", len(merged), len(diags), err)
	}
	merged, diags, err = engine.MergeFingerprints(stamper{}, rules.ProfileDev, []sdk.Finding{a, clash, other})
	if err != nil || len(merged) != 3 || len(diags) != 1 {
		t.Fatalf("distinct findings must stay with a diagnostic: %d %d %v", len(merged), len(diags), err)
	}
	if diags[0].RuleID != engine.DiagDuplicateFingerprint || diags[0].Severity != sdk.SeverityWarning || diags[0].Key != a.Fingerprint {
		t.Errorf("diag %+v", diags[0])
	}
}

func adapterFinding(adapter, id, name string) sdk.Finding {
	return sdk.Finding{RuleID: id, RuleVersion: 1, Adapter: adapter, Resource: sdk.ResourceRef{Kind: "arm-resource", Type: "t", Name: name}}
}

func TestDedupAdapter(t *testing.T) {
	cat := []catalog.Rule{
		{ID: "FND-SEC-001", Overlap: catalog.Overlap{PSRule: []string{"Azure.AI.DisableLocalAuth"}, Checkov: []string{"CKV_AZURE_236"}}},
		{ID: "FND-SEC-002", Overlap: catalog.Overlap{PSRule: []string{"Azure.Other"}}},
	}
	idx := engine.NewOverlapIndex(cat)
	if got := idx.Lookup("psrule", "azure.ai.disablelocalauth"); !slices.Equal(got, []string{"FND-SEC-001"}) {
		t.Errorf("lookup is case-insensitive: %v", got)
	}
	if got := idx.Lookup("checkov", "Azure.AI.DisableLocalAuth"); len(got) != 0 {
		t.Errorf("a known adapter searches only its own list: %v", got)
	}
	if got := idx.Lookup("unknown-tool", "CKV_AZURE_236"); !slices.Equal(got, []string{"FND-SEC-001"}) {
		t.Errorf("an unknown adapter searches every list: %v", got)
	}

	native := []sdk.Finding{fnd("FND-SEC-001", sdk.SeverityError, "acct")}
	native[0].Resource.Type = "T" // type compares case-insensitively
	tests := []struct {
		name     string
		adapter  []sdk.Finding
		kept     int
		absorbed int
	}{
		{"same resource is absorbed", []sdk.Finding{adapterFinding("psrule", "Azure.AI.DisableLocalAuth", "acct")}, 0, 1},
		{"other resource is kept", []sdk.Finding{adapterFinding("psrule", "Azure.AI.DisableLocalAuth", "other")}, 1, 0},
		{"rule without native finding is kept", []sdk.Finding{adapterFinding("psrule", "Azure.Other", "acct")}, 1, 0},
		{"unmapped adapter rule is kept", []sdk.Finding{adapterFinding("psrule", "Azure.Unmapped", "acct")}, 1, 0},
		{"catalogue id from the adapter", []sdk.Finding{adapterFinding("psrule", "FND-SEC-001", "acct")}, 0, 1},
		{"no adapter findings", nil, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kept, absorbed := engine.DedupAdapter(native, tc.adapter, idx)
			if len(kept) != tc.kept || len(absorbed) != tc.absorbed {
				t.Fatalf("kept %d absorbed %d", len(kept), len(absorbed))
			}
			if tc.absorbed == 1 && (absorbed[0].NativeRuleID != "FND-SEC-001" || absorbed[0].NativeFingerprint != native[0].Fingerprint) {
				t.Errorf("provenance %+v", absorbed[0])
			}
		})
	}
	// An adapter finding never absorbs another adapter finding.
	viaAdapter := []sdk.Finding{adapterFinding("psrule", "FND-SEC-001", "acct")}
	if kept, _ := engine.DedupAdapter(viaAdapter, viaAdapter, idx); len(kept) != 1 {
		t.Error("adapter findings must not count as native")
	}
}

func TestDefaultCatalogue(t *testing.T) {
	c, err := engine.DefaultCatalogue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Registry.Catalog()) != 108 || len(c.Packs) != 1 {
		t.Errorf("catalogue %d packs %d", len(c.Registry.Catalog()), len(c.Packs))
	}
	// Pack resolution works end to end on the real catalogue once rules exist; with none it must fail loudly.
	if _, err := c.Registry.Resolve(c.Packs, rules.Selection{}, rules.Selector{}); len(c.Registry.Executable()) == 0 && err == nil {
		t.Error("an empty implementation set must not silently resolve the default pack")
	}
}

func TestNewCatalogueRejectsWrongImplementation(t *testing.T) {
	bad := []rules.Rule{wrongRule{}}
	if _, err := engine.NewCatalogue(context.Background(), bad); err == nil {
		t.Fatal("implementation of a non-phase-1 rule accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.NewCatalogue(ctx, nil); err == nil {
		t.Fatal("cancelled context ignored")
	}
}

type wrongRule struct{}

func (wrongRule) ID() string   { return "FND-DEP-001" }
func (wrongRule) Version() int { return 1 }
func (wrongRule) Evaluate(context.Context, *model.Input) ([]rules.Result, error) {
	return nil, nil
}
