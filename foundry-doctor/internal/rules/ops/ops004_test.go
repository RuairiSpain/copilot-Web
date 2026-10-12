package ops

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func tagPolicy(in *model.Input) *model.Input {
	in.Policy.Tags.Required = []model.TagRequirement{
		{Name: "Env", Format: "^(dev|test|prod)$"},
		{Name: "CostCode", Format: "^CC-[0-9]{4}$"},
	}
	return in
}

func tagInput(t *testing.T, names ...string) *model.Input {
	t.Helper()
	return tagPolicy(input(arm(t, "tags.arm.json", names...)))
}

func scenarios004() scenarios {
	r := ops004{}
	return scenarios{
		"positive/account-without-required-tag": func(t *testing.T) {
			got := eval(t, r, tagInput(t, "acct-missing"))
			wantKeys(t, got, sdk.StatusFail, "missing-tag:env", "missing-tag:costcode")
			if f := fails(got)[0]; f.Confidence != sdk.ConfidenceLikely || f.Resource.Pointer == "" || f.Location.Pointer == "" {
				t.Fatalf("finding = %+v", f)
			}
		},
		"positive/tag-value-fails-configured-format": func(t *testing.T) {
			got := eval(t, r, tagInput(t, "acct-bad-format"))
			wantKeys(t, got, sdk.StatusFail, "format:env", "format:costcode")
		},
		"positive/tag-name-differs-only-by-case-is-accepted-but-wrong-value-fails": func(t *testing.T) {
			// ENV is accepted for Env, so no missing-tag finding; only the value is wrong.
			got := eval(t, r, tagInput(t, "acct-bad-format"))
			for _, f := range fails(got) {
				if strings.HasPrefix(f.Key, "missing-tag:") {
					t.Fatalf("case difference reported as missing: %s", f.Key)
				}
			}
			if !hasStatus(got, sdk.StatusFail) {
				t.Fatal("wrong value not reported")
			}
		},
		"positive/more-than-fifty-tags": func(t *testing.T) {
			many := map[string]any{"Env": "prod", "CostCode": "CC-1234"}
			for i := 0; i < 49; i++ {
				many["t"+strings.Repeat("x", i%5)+string(rune('A'+i/5))+string(rune('a'+i%26))] = "v"
			}
			if len(many) != 51 {
				t.Fatalf("setup: %d tags", len(many))
			}
			in := tagInput(t, "acct-ok")
			in.ARM = withTags(in.ARM, "acct-ok", many)
			wantKeys(t, eval(t, r, in), sdk.StatusFail, "limit:count")
		},
		"positive/storage-account-tag-name-over-128-characters": func(t *testing.T) {
			long := strings.Repeat("n", 129)
			tags := map[string]any{"Env": "prod", "CostCode": "CC-1234", long: "v"}
			in := tagInput(t, "stokay")
			in.ARM = withTags(in.ARM, "stokay", tags)
			wantKeys(t, eval(t, r, in), sdk.StatusFail, "limit:name-length")
			// The same name is within the 512 limit on a Foundry account.
			in = tagInput(t, "acct-ok")
			in.ARM = withTags(in.ARM, "acct-ok", tags)
			wantNoFindings(t, eval(t, r, in))
		},
		"negative/all-required-tags-present-on-every-in-scope-resource": func(t *testing.T) {
			got := eval(t, r, tagInput(t, "acct-ok", "stokay", "vnet-not-in-scope"))
			wantPass(t, got)
			if len(got) != 2 {
				t.Fatalf("out-of-scope resource produced a result: %d results", len(got))
			}
		},
		"negative/tags-supplied-through-union-of-common-tags": func(t *testing.T) {
			wantPass(t, eval(t, r, tagInput(t, "acct-union")))
		},
		"skipped/no-required-tags-in-profile": func(t *testing.T) {
			in := input(arm(t, "tags.arm.json", "acct-missing"))
			got := eval(t, r, in)
			wantSkip(t, got, "profile-key-missing:policy.tags.required")
			wantNoFindings(t, got)
			in.Policy.Tags.Required = []model.TagRequirement{}
			wantSkip(t, eval(t, r, in), "profile-key-missing:policy.tags.required")
		},
		"skipped/tags-depend-on-unresolved-parameter": func(t *testing.T) {
			got := eval(t, r, tagInput(t, "acct-param"))
			wantSkip(t, got, "unresolved-value:tags-unsupported-expression")
			wantNoFindings(t, got)
		},
		"skipped/resource-type-does-not-support-tags": func(t *testing.T) {
			in := tagInput(t, "11111111-1111-1111-1111-111111111111")
			in.Policy.Tags.ResourceTypes = []string{"microsoft.authorization/roleassignments"}
			got := eval(t, r, in)
			wantSkip(t, got, "not-applicable")
			wantNoFindings(t, got)
		},
	}
}

func TestOPS004Extras(t *testing.T) {
	r := ops004{}
	t.Run("synthetic", func(t *testing.T) {
		in := tagInput(t, "acct-ok")
		in.ARM = nil
		wantSkip(t, eval(t, r, in), "synthetic-infrastructure")
	})
	t.Run("invalid format regex", func(t *testing.T) {
		in := tagInput(t, "acct-ok")
		in.Policy.Tags.Required[0].Format = "("
		wantSkip(t, eval(t, r, in), "unresolved-value:invalid-tag-format")
	})
	t.Run("tag name expression may supply the tag", func(t *testing.T) {
		got := eval(t, r, tagInput(t, "acct-keyexpr"))
		wantNoFindings(t, got)
		wantSkip(t, got, "unresolved-value:tag-name-expression")
	})
	t.Run("tag value expression skips only the format", func(t *testing.T) {
		got := eval(t, r, tagInput(t, "acct-valexpr"))
		wantNoFindings(t, got)
		wantSkip(t, got, "unresolved-value:tag-value-expression")
	})
	t.Run("conditional resource lowers confidence", func(t *testing.T) {
		got := eval(t, r, tagInput(t, "acct-conditional"))
		f := fails(got)
		if len(f) != 2 || f[0].Confidence != sdk.ConfidenceUncertain {
			t.Fatalf("findings = %+v", f)
		}
	})
	t.Run("resource types restrict scope", func(t *testing.T) {
		in := tagInput(t, "acct-missing", "stokay")
		in.Policy.Tags.ResourceTypes = []string{"Microsoft.Storage/storageAccounts"}
		got := eval(t, r, in)
		wantPass(t, got)
	})
	t.Run("tags not an object", func(t *testing.T) {
		in := tagInput(t, "acct-ok")
		in.ARM = withTags(in.ARM, "acct-ok", nil)
		in.ARM.Resources[0].Body["tags"] = "plain"
		wantSkip(t, eval(t, r, in), "unresolved-value:tags-not-an-object")
	})
	t.Run("long value and non-string value", func(t *testing.T) {
		in := tagInput(t, "acct-ok")
		in.ARM = withTags(in.ARM, "acct-ok", map[string]any{"Env": "prod", "CostCode": "CC-1234", "x": strings.Repeat("v", 257)})
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "limit:value-length")
		in.ARM = withTags(in.ARM, "acct-ok", map[string]any{"Env": float64(5), "CostCode": "CC-1234"})
		wantKeys(t, eval(t, r, in), sdk.StatusFail, "format:env")
	})
	t.Run("evidence never carries a tag value", func(t *testing.T) {
		secret := "s3" + "cr3t-" + "canary"
		in := tagInput(t, "acct-ok")
		in.ARM = withTags(in.ARM, "acct-ok", map[string]any{"Env": secret, "CostCode": "CC-1234"})
		f := fails(eval(t, r, in))
		if len(f) != 1 {
			t.Fatalf("findings = %+v", f)
		}
		if strings.Contains(f[0].Evidence+f[0].Recommendation+f[0].Fix+f[0].Key, secret) {
			t.Fatal("tag value leaked")
		}
	})
}
