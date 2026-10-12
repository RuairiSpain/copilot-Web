package rel

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

func search(t *testing.T, names ...string) *model.Input {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../../test/fixtures/rules/rel", "search.arm.json"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := bicep.Normalise(raw, bicep.Options{File: "infra/main.bicep"})
	if err != nil {
		t.Fatal(err)
	}
	var keep []model.Resource
	for _, r := range n.Template.Resources {
		if slices.Contains(names, r.Name) {
			keep = append(keep, r)
		}
	}
	if len(keep) != len(names) {
		t.Fatalf("wanted %v, found %d", names, len(keep))
	}
	n.Template.Resources = keep
	return &model.Input{Project: model.Project{Name: "demo", IaC: model.IaCBicep}, Profile: rules.ProfileProd, ARM: n.Template}
}

func eval(t *testing.T, in *model.Input) []rules.Result {
	t.Helper()
	got, err := rel005{}.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func keys(rs []rules.Result, st sdk.Status) []string {
	var out []string
	for _, r := range rs {
		if r.Status == st {
			out = append(out, r.Finding.Key)
		}
	}
	slices.Sort(out)
	return out
}

func wantFail(t *testing.T, names string, want ...string) {
	t.Helper()
	got := eval(t, search(t, names))
	slices.Sort(want)
	if k := keys(got, sdk.StatusFail); !slices.Equal(k, want) {
		t.Fatalf("fail keys = %v, want %v", k, want)
	}
	for _, r := range got {
		if r.Status == sdk.StatusPass {
			t.Fatal("pass alongside failure")
		}
	}
}

func wantPass(t *testing.T, name string) {
	t.Helper()
	got := eval(t, search(t, name))
	if len(got) != 1 || got[0].Status != sdk.StatusPass {
		t.Fatalf("results = %+v", got)
	}
}

func scenarios() map[string]func(t *testing.T) {
	return map[string]func(t *testing.T){
		"positive/standard, replicaCount 3, partitionCount 2 passes":          func(t *testing.T) { wantPass(t, "std-3x2") },
		"positive/standard, replicaCount 12, partitionCount 3 (36 SU) passes": func(t *testing.T) { wantPass(t, "std-12x3") },
		"positive/standard3 HighDensity with partitionCount 3 passes":         func(t *testing.T) { wantPass(t, "s3hd-3") },
		"negative/partitionCount 5 fails":                                     func(t *testing.T) { wantFail(t, "std-p5", "partition-count") },
		"negative/replicaCount 13 fails":                                      func(t *testing.T) { wantFail(t, "std-r13", "replica-count") },
		"negative/basic with replicaCount 4 fails":                            func(t *testing.T) { wantFail(t, "basic-r4", "replica-count-basic") },
		"negative/standard replicaCount 4 and partitionCount 12 (48 SU) fails": func(t *testing.T) {
			wantFail(t, "std-4x12", "search-units")
		},
		"negative/standard HighDensity fails":                        func(t *testing.T) { wantFail(t, "std-hd", "high-density-sku") },
		"negative/standard3 HighDensity with partitionCount 4 fails": func(t *testing.T) { wantFail(t, "s3hd-4", "high-density-partitions") },
		"skipped/replicaCount and partitionCount absent: defaults apply, nothing to check": func(t *testing.T) {
			got := eval(t, search(t, "defaults"))
			if len(got) != 1 || got[0].Status != sdk.StatusSkipped || got[0].Skip.Reason != "not-applicable" {
				t.Fatalf("results = %+v", got)
			}
		},
		"skipped/free or serverless SKU: documented N/A, skipped with reason": func(t *testing.T) {
			got := eval(t, search(t, "free-1", "serverless-1"))
			if len(got) != 2 || got[0].Skip == nil || got[1].Skip == nil || got[0].Skip.Reason != "not-applicable" {
				t.Fatalf("results = %+v", got)
			}
		},
		"uncertain/basic with partitionCount above 1: depends on service creation date and region, reported uncertain": func(t *testing.T) {
			got := eval(t, search(t, "basic-p3"))
			if k := keys(got, sdk.StatusUncertain); !slices.Equal(k, []string{"basic-partitions"}) || len(keys(got, sdk.StatusFail)) != 0 {
				t.Fatalf("results = %+v", got)
			}
			if got[0].Finding.Confidence != sdk.ConfidenceUncertain {
				t.Fatal("confidence")
			}
		},
		"uncertain/values supplied by unresolved parameter expressions": func(t *testing.T) {
			got := eval(t, search(t, "std-param"))
			if k := keys(got, sdk.StatusUncertain); !slices.Equal(k, []string{"unresolved-values"}) {
				t.Fatalf("results = %+v", got)
			}
		},
	}
}

func TestScenarios(t *testing.T) {
	for name, fn := range scenarios() {
		t.Run(name, fn)
	}
}

func TestScenarioCoverage(t *testing.T) {
	cat, err := rules.LoadCatalog(context.Background(), ruledata.FS, ruledata.CatalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range cat {
		if m.ID != "FND-REL-005" {
			continue
		}
		found = true
		if (rel005{}).Version() != m.Version {
			t.Errorf("version %d, catalogue %d", rel005{}.Version(), m.Version)
		}
		sc := scenarios()
		for group, names := range map[string][]string{"positive": m.Tests.Positive, "negative": m.Tests.Negative, "skipped": m.Tests.Skipped, "uncertain": m.Tests.Uncertain} {
			for _, n := range names {
				if sc[group+"/"+n] == nil {
					t.Errorf("catalogue scenario %s/%s has no subtest", group, n)
				}
			}
		}
	}
	if !found {
		t.Fatal("FND-REL-005 not in catalogue")
	}
}

func TestRegistration(t *testing.T) {
	rs := Rules()
	if len(rs) != 1 || rs[0].ID() != "FND-REL-005" || rs[0].Version() != 1 {
		t.Fatalf("rules = %v", rs)
	}
}

func TestSyntheticGuard(t *testing.T) {
	in := &model.Input{Project: model.Project{Name: "p", IaC: model.IaCSynthetic}}
	got := eval(t, in)
	if len(got) != 1 || got[0].Skip.Reason != rules.ReasonSyntheticInfrastructure {
		t.Fatalf("results = %+v", got)
	}
}

func TestNoSearchResourcesNoResults(t *testing.T) {
	in := search(t, "std-3x2")
	in.ARM.Resources = nil
	if got := eval(t, in); len(got) != 0 {
		t.Fatalf("results = %+v", got)
	}
}

func TestMultipleFindingsDeterministicAndStable(t *testing.T) {
	in := search(t, "std-p5", "std-4x12", "basic-r4", "std-cond")
	first := eval(t, in)
	if !reflect.DeepEqual(first, eval(t, in)) {
		t.Fatal("not deterministic")
	}
	seen := map[string]bool{}
	for _, r := range first {
		f := r.Finding
		if f == nil {
			continue
		}
		if f.Severity != "" || f.Profile != "" || f.Fingerprint != "" || f.Key == "" || f.Resource.Pointer == "" || f.Location.Pointer == "" || f.Fix == "" {
			t.Fatalf("finding = %+v", f)
		}
		fp := findings.Fingerprint(*f)
		if seen[fp] {
			t.Fatalf("duplicate fingerprint for %s", f.Key)
		}
		seen[fp] = true
		moved := *f
		moved.Location.Line += 5
		moved.Evidence = "other prose"
		if findings.Fingerprint(moved) != fp {
			t.Fatal("fingerprint unstable")
		}
		if f.Resource.Name == "std-cond" && f.Confidence != sdk.ConfidenceLikely {
			t.Fatalf("conditional resource confidence = %s", f.Confidence)
		}
	}
}

func TestOtherShapes(t *testing.T) {
	t.Run("missing sku still checks generic limits", func(t *testing.T) {
		in := search(t, "std-p5")
		in.ARM.Resources[0].Body["sku"] = nil
		delete(in.ARM.Resources[0].Body, "sku")
		got := eval(t, in)
		if k := keys(got, sdk.StatusFail); !slices.Equal(k, []string{"partition-count"}) {
			t.Fatalf("results = %+v", got)
		}
	})
	t.Run("replica count below one", func(t *testing.T) {
		in := search(t, "std-3x2")
		in.ARM.Resources[0].Body["properties"].(map[string]any)["replicaCount"] = float64(0)
		if k := keys(eval(t, in), sdk.StatusFail); !slices.Equal(k, []string{"replica-count"}) {
			t.Fatalf("keys = %v", k)
		}
	})
	t.Run("non-integer value is not asserted", func(t *testing.T) {
		in := search(t, "std-3x2")
		in.ARM.Resources[0].Body["properties"].(map[string]any)["partitionCount"] = 2.5
		got := eval(t, in)
		if len(keys(got, sdk.StatusFail)) != 0 || len(keys(got, sdk.StatusUncertain)) != 1 {
			t.Fatalf("results = %+v", got)
		}
	})
	t.Run("hostingMode is case-insensitive", func(t *testing.T) {
		in := search(t, "std-hd")
		in.ARM.Resources[0].Body["properties"].(map[string]any)["hostingMode"] = "highDensity"
		if k := keys(eval(t, in), sdk.StatusFail); !slices.Equal(k, []string{"high-density-sku"}) {
			t.Fatalf("keys = %v", k)
		}
	})
	t.Run("evidence does not echo unrelated content", func(t *testing.T) {
		for _, r := range eval(t, search(t, "std-p5")) {
			if strings.Contains(r.Finding.Evidence, "westeurope") {
				t.Fatal("unexpected content in evidence")
			}
		}
	})
}
