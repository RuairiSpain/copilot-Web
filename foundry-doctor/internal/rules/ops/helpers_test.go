package ops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azureyaml"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

const fixtureDir = "../../../test/fixtures/rules/ops"

// arm normalises a committed ARM JSON fixture with the real normaliser and keeps only the named
// resources (all of them when no name is given).
func arm(t *testing.T, file string, names ...string) *model.ARMTemplate {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, file))
	if err != nil {
		t.Fatal(err)
	}
	n, err := bicep.Normalise(raw, bicep.Options{File: "infra/main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) > 0 {
		var keep []model.Resource
		for _, r := range n.Template.Resources {
			if slices.Contains(names, r.Name) {
				keep = append(keep, r)
			}
		}
		if len(keep) != len(names) {
			t.Fatalf("fixture %s: wanted %v, found %d resources", file, names, len(keep))
		}
		n.Template.Resources = keep
	}
	return n.Template
}

// withTags replaces the tags of the named resource in a copy of the template.
func withTags(tpl *model.ARMTemplate, name string, tags map[string]any) *model.ARMTemplate {
	out := *tpl
	out.Resources = slices.Clone(tpl.Resources)
	for i, r := range out.Resources {
		if r.Name == name {
			body := map[string]any{}
			for k, v := range r.Body {
				body[k] = v
			}
			body["tags"] = tags
			out.Resources[i].Body = body
		}
	}
	return &out
}

func input(tpl *model.ARMTemplate) *model.Input {
	return &model.Input{
		Project: model.Project{Name: "demo", IaC: model.IaCBicep, AzureYAMLPath: "azure.yaml"},
		Profile: rules.ProfileProd,
		ARM:     tpl,
	}
}

func yamlInput(t *testing.T, src string) *model.Input {
	t.Helper()
	res, err := azureyaml.Parse([]byte(src), azureyaml.Options{File: "azure.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return &model.Input{
		Project:    model.Project{Name: "demo", IaC: model.IaCSynthetic, AzureYAMLPath: "azure.yaml"},
		Profile:    rules.ProfileProd,
		AzureYAML:  res.YAML,
		YAMLReport: res,
	}
}

func eval(t *testing.T, r rules.Rule, in *model.Input) []rules.Result {
	t.Helper()
	got, err := r.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range got {
		if err := x.Validate(); err != nil {
			t.Fatalf("invalid result %+v: %v", x, err)
		}
		if x.Finding != nil && (x.Finding.Key == "" || x.Finding.Resource.Name == "" || x.Finding.Resource.Kind == "" ||
			x.Finding.Evidence == "" || x.Finding.Recommendation == "" || x.Finding.Fix == "") {
			t.Fatalf("finding misses a field: %+v", x.Finding)
		}
	}
	return got
}

func fails(rs []rules.Result) []sdk.Finding { return byStatus(rs, sdk.StatusFail) }

func byStatus(rs []rules.Result, st sdk.Status) []sdk.Finding {
	var out []sdk.Finding
	for _, r := range rs {
		if r.Status == st {
			out = append(out, *r.Finding)
		}
	}
	return out
}

func keysOf(fs []sdk.Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	slices.Sort(out)
	return out
}

func skipReasons(rs []rules.Result) []string {
	var out []string
	for _, r := range rs {
		if r.Status == sdk.StatusSkipped {
			out = append(out, r.Skip.Reason)
		}
	}
	return out
}

func hasStatus(rs []rules.Result, st sdk.Status) bool {
	return slices.ContainsFunc(rs, func(r rules.Result) bool { return r.Status == st })
}

func wantKeys(t *testing.T, rs []rules.Result, st sdk.Status, want ...string) {
	t.Helper()
	got := keysOf(byStatus(rs, st))
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s keys = %v, want %v (skips %v)", st, got, want, skipReasons(rs))
	}
}

func wantSkip(t *testing.T, rs []rules.Result, reason string) {
	t.Helper()
	if !slices.Contains(skipReasons(rs), reason) {
		t.Fatalf("skip reasons = %v, want %q", skipReasons(rs), reason)
	}
}

func wantNoFindings(t *testing.T, rs []rules.Result) {
	t.Helper()
	if hasStatus(rs, sdk.StatusFail) || hasStatus(rs, sdk.StatusUncertain) {
		t.Fatalf("unexpected findings %v", keysOf(append(byStatus(rs, sdk.StatusFail), byStatus(rs, sdk.StatusUncertain)...)))
	}
}

func wantPass(t *testing.T, rs []rules.Result) {
	t.Helper()
	wantNoFindings(t, rs)
	if !hasStatus(rs, sdk.StatusPass) {
		t.Fatalf("no pass result; skips %v", skipReasons(rs))
	}
}

// scenarios maps "group/name" (the catalogue test scenarios) to a test body, per rule ID.
type scenarios map[string]func(t *testing.T)

func allScenarios() map[string]scenarios {
	return map[string]scenarios{
		"FND-OPS-004": scenarios004(),
		"FND-OPS-005": scenarios005(),
		"FND-OPS-007": scenarios007(),
		"FND-OPS-009": scenarios009(),
		"FND-OPS-010": scenarios010(),
	}
}

func TestScenarios(t *testing.T) {
	all := allScenarios()
	for _, id := range sortedKeys(all) {
		for _, name := range sortedKeys(all[id]) {
			t.Run(id+"/"+name, all[id][name])
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestScenarioCoverage fails when a catalogue test scenario of a rule in this package has no subtest.
func TestScenarioCoverage(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), ruledata.FS, ruledata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	all := allScenarios()
	impl := map[string]rules.Rule{}
	for _, r := range Rules() {
		impl[r.ID()] = r
	}
	for _, m := range cat {
		r, ok := impl[m.ID]
		if !ok {
			continue
		}
		if r.Version() != m.Version {
			t.Errorf("%s: version %d, catalogue %d", m.ID, r.Version(), m.Version)
		}
		want := map[string][]string{"positive": m.Tests.Positive, "negative": m.Tests.Negative, "skipped": m.Tests.Skipped, "uncertain": m.Tests.Uncertain}
		for group, names := range want {
			for _, n := range names {
				if all[m.ID][group+"/"+n] == nil {
					t.Errorf("%s: catalogue scenario %s/%s has no subtest", m.ID, group, n)
				}
			}
		}
	}
	for id := range all {
		if _, ok := impl[id]; !ok {
			t.Errorf("scenarios for %s but no implementation registered", id)
		}
	}
}
