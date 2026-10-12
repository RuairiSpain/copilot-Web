package ops

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func nameInput(t *testing.T, env map[string]string, names ...string) *model.Input {
	t.Helper()
	in := input(arm(t, "names.arm.json", names...))
	if env != nil {
		e := model.NewEnvironment("dev", env)
		in.Environment = &e
	}
	return in
}

func oneNameFail(t *testing.T, name, contains string) {
	t.Helper()
	got := eval(t, ops005{}, nameInput(t, nil, name))
	wantKeys(t, got, sdk.StatusFail, "name")
	f := fails(got)[0]
	if !strings.Contains(f.Evidence, contains) || f.Confidence != sdk.ConfidenceCertain || f.Location.File == "" {
		t.Fatalf("finding = %+v", f)
	}
}

func scenarios005() scenarios {
	r := ops005{}
	return scenarios{
		"positive/storage-account-name-with-hyphen-and-25-characters": func(t *testing.T) {
			got := eval(t, r, nameInput(t, nil, "stor-account-name-over-25"))
			f := fails(got)[0]
			if !strings.Contains(f.Evidence, "length 25 exceeds the maximum 24") || !strings.Contains(f.Evidence, "lowercase letters and digits") {
				t.Fatalf("evidence = %s", f.Evidence)
			}
		},
		"positive/search-service-name-with-trailing-dash": func(t *testing.T) {
			oneNameFail(t, "search-svc-", "ends with a dash")
		},
		"positive/cosmos-account-name-with-uppercase": func(t *testing.T) {
			oneNameFail(t, "MyCosmos", "lowercase")
		},
		"positive/key-vault-name-with-consecutive-hyphens": func(t *testing.T) {
			oneNameFail(t, "kv--vault", "consecutive hyphens")
		},
		"positive/foundry-project-name-starting-with-hyphen": func(t *testing.T) {
			oneNameFail(t, "acct-ok/-proj", "does not start with a letter or digit")
		},
		"positive/foundry-account-name-single-character": func(t *testing.T) {
			oneNameFail(t, "a", "below the minimum 2")
		},
		"positive/name-built-from-long-azure-env-name": func(t *testing.T) {
			env := strings.Repeat("e", 60)
			got := eval(t, r, nameInput(t, map[string]string{"AZURE_ENV_NAME": env}, "[concat(parameters('environmentName'), '-foundry-account-with-a-long-suffix-aaaaaaa')]"))
			wantKeys(t, got, sdk.StatusFail, "name")
			f := fails(got)[0]
			if !strings.Contains(f.Evidence, "exceeds the maximum 64") || strings.Contains(f.Evidence, env) {
				t.Fatalf("evidence = %s", f.Evidence)
			}
		},
		"negative/all-names-within-limits": func(t *testing.T) {
			got := eval(t, r, nameInput(t, map[string]string{"AZURE_ENV_NAME": "dev"}, "storageokay123", "search-svc", "my-foundry-account-1",
				"[toLower(concat('ok-', parameters('environmentName')))]"))
			wantPass(t, got)
		},
		"negative/foundry-account-name-with-hyphens-two-to-sixty-four-characters": func(t *testing.T) {
			wantPass(t, eval(t, r, nameInput(t, nil, "my-foundry-account-1")))
		},
		"skipped/name-from-unconstrained-parameter": func(t *testing.T) {
			got := eval(t, r, nameInput(t, nil, "[parameters('storageName')]"))
			wantSkip(t, got, "unresolved-value:unsupported-expression")
			wantNoFindings(t, got)
		},
		"skipped/name-from-uniqueString-without-known-length": func(t *testing.T) {
			got := eval(t, r, nameInput(t, nil, "[concat('acct-', uniqueString(resourceGroup().id))]"))
			wantSkip(t, got, "unresolved-value:uniquestring-length-unknown")
			wantNoFindings(t, got)
		},
		"skipped/resource-type-not-in-table": func(t *testing.T) {
			got := eval(t, r, nameInput(t, nil, "my-foundry-account-1/chat", "VNET_not_in_table"))
			wantSkip(t, got, "not-applicable")
			if len(got) != 1 {
				t.Fatalf("results = %d, want only the deployment skip", len(got))
			}
		},
	}
}

func TestOPS005Table(t *testing.T) {
	// One row per table entry that the scenarios above do not already violate.
	for name, contains := range map[string]string{
		"ai-%bad.": "contains one of",
		"1apim-":   "does not start with a letter",
		"acr-x":    "letters and digits",
		"-ws":      "does not start and end",
		"App1":     "lowercase",
		"cosmos-name-with-fifty-one-characters-abcdefghijklmnop": "maximum of every source",
	} {
		t.Run(name, func(t *testing.T) {
			got := eval(t, ops005{}, nameInput(t, nil, name))
			if len(fails(got)) != 1 || !strings.Contains(fails(got)[0].Evidence, contains) {
				t.Fatalf("results = %+v", got)
			}
		})
	}
	t.Run("cosmos 45 to 50 characters is uncertain", func(t *testing.T) {
		got := eval(t, ops005{}, nameInput(t, nil, "cosmos-name-with-forty-seven-characters-abcdefg"))
		wantKeys(t, got, sdk.StatusUncertain, "name")
		if got[0].Finding.Confidence != sdk.ConfidenceUncertain {
			t.Fatal("confidence")
		}
	})
	t.Run("environment name unavailable", func(t *testing.T) {
		got := eval(t, ops005{}, nameInput(t, map[string]string{"X": "1"}, "[toLower(concat('ok-', parameters('environmentName')))]"))
		wantSkip(t, got, "unresolved-value:azd-environment-name-unavailable")
		got = eval(t, ops005{}, nameInput(t, nil, "[toLower(concat('ok-', parameters('environmentName')))]"))
		wantSkip(t, got, "unresolved-value:unsupported-expression")
	})
	t.Run("synthetic", func(t *testing.T) {
		in := nameInput(t, nil, "a")
		in.ARM = nil
		wantSkip(t, eval(t, ops005{}, in), "synthetic-infrastructure")
	})
	t.Run("lowered confidence in conditional module", func(t *testing.T) {
		in := nameInput(t, nil, "a")
		in.ARM.Resources[0].Meta.MayNotDeploy = true
		if f := fails(eval(t, ops005{}, in)); f[0].Confidence != sdk.ConfidenceLikely {
			t.Fatalf("confidence = %s", f[0].Confidence)
		}
	})
}

func TestExprEvaluator(t *testing.T) {
	env := model.NewEnvironment("e", map[string]string{"AZURE_ENV_NAME": "dev"})
	for _, tc := range []struct {
		expr   string
		want   any
		reason string
	}{
		{"[concat('a', 'b''c')]", "ab'c", ""},
		{"[toUpper('ab')]", "AB", ""},
		{"[ concat( 'a' , parameters('environmentName') ) ]", "adev", ""},
		{"[union(createObject('a','1'), createObject('b','2'))]", map[string]any{"a": "1", "b": "2"}, ""},
		{"[uniqueString(resourceGroup().id)]", nil, reasonUnique},
		{"[concat('a', uniqueString('x'))]", nil, reasonUnique},
		{"[parameters('other')]", nil, reasonUnsupported},
		{"[resourceGroup().id]", nil, reasonUnsupported},
		{"[concat('a'", nil, reasonUnsupported},
		{"[concat('a' 'b')]", nil, reasonUnsupported},
		{"[concat('a')x]", nil, reasonUnsupported},
		{"[concat('unterminated)]", nil, reasonUnsupported},
		{"[createObject('a')]", nil, reasonUnsupported},
		{"[createObject(concat('a'), 'b')]", map[string]any{"a": "b"}, ""},
		{"[union('a')]", nil, reasonUnsupported},
		{"[toLower('a','b')]", nil, reasonUnsupported},
		{"[concat(createObject())]", nil, reasonUnsupported},
		{"[bogus()]", nil, reasonUnsupported},
		{"[]", nil, reasonUnsupported},
		{"[(]", nil, reasonUnsupported},
		{"plain", nil, reasonUnsupported},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got, _, err := evalARM(tc.expr, &env)
			if tc.reason != "" {
				if err == nil || reasonOf(err) != tc.reason {
					t.Fatalf("err = %v, want reason %s", err, tc.reason)
				}
				return
			}
			if err != nil || !equalAny(got, tc.want) {
				t.Fatalf("got %v, %v want %v", got, err, tc.want)
			}
		})
	}
	if reasonOf(errNilReason{}) != reasonUnsupported {
		t.Fatal("foreign error")
	}
}

type errNilReason struct{}

func (errNilReason) Error() string { return "x" }

func equalAny(a, b any) bool {
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !equalAny(v, y[k]) {
				return false
			}
		}
		return true
	}
	return false
}
