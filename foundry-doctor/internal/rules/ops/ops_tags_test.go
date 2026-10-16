package ops

import (
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rulestest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// tagModel is an ARM model that can tell expression-valued tags from absent
// tags, as the contract in tagExpressionReporter describes.
type tagModel struct {
	model
	expr map[string]bool
}

func (m tagModel) TagsAreExpression(r sdk.ARMResource) bool { return m.expr[r.Name] }

func realPolicy() config.EffectivePolicy {
	return config.EffectivePolicy{
		"tags.required": []config.TagRequirement{{Name: "owner"}, {Name: "cost-center", Format: `^cc\d+$`}},
	}
}

func TestOPS004RealEffectivePolicyType(t *testing.T) {
	// The effective policy returns []config.TagRequirement, not []any.
	v, ok := realPolicy().Get("tags.required")
	if !ok {
		t.Fatal("policy key missing")
	}
	if _, isReq := v.([]config.TagRequirement); !isReq {
		t.Fatalf("Get returned %T", v)
	}
	acct := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "a", Tags: map[string]any{"owner": "x", "cost-center": "cc1"}}
	out := eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{acct}, Policy: realPolicy()})
	if out.Skipped != nil || len(out.Findings) != 0 {
		t.Fatalf("compliant resource must pass: %+v", out)
	}
	acct.Tags = map[string]any{"owner": "x", "cost-center": "nope"}
	out = eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{acct}, Policy: realPolicy()})
	if len(out.Findings) != 1 || out.Findings[0].Key != "format:cost-center" {
		t.Fatalf("format mismatch must be one keyed finding: %+v", out)
	}
	acct.Tags = map[string]any{"cost-center": "cc1"}
	out = eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{acct}, Policy: realPolicy()})
	if len(out.Findings) != 1 || out.Findings[0].Key != "missing:owner" {
		t.Fatalf("missing tag must be a keyed finding: %+v", out)
	}
}

func TestOPS004PolicyKeyMissingAndEmpty(t *testing.T) {
	acct := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "a"}
	for name, p := range map[string]sdk.Policy{
		"nil policy":   nil,
		"key absent":   config.EffectivePolicy{},
		"empty list":   config.EffectivePolicy{"tags.required": []config.TagRequirement{}},
		"blank names":  config.EffectivePolicy{"tags.required": []config.TagRequirement{{Name: " "}}},
		"wrong type":   config.EffectivePolicy{"tags.required": "owner"},
		"unusable any": config.EffectivePolicy{"tags.required": []any{1}},
	} {
		out := eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{acct}, Policy: p})
		if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, "profile-key-missing") {
			t.Errorf("%s: want profile-key-missing skip, got %+v", name, out)
		}
	}
	if out := eval(t, "FND-OPS-004", Providers{}, &sdk.Input{Policy: realPolicy()}); out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, sdk.SkipInputUnavailable+": ") {
		t.Errorf("no ARM: %+v", out)
	}
}

// Compiled goldens/tags.bicep: no tags, literal tags, expression-valued tags.
func TestOPS004CompiledNoTagsAndExpressionTags(t *testing.T) {
	m := rulestest.Golden(t, "tags")
	rs := m.Resources()
	byName := map[string]sdk.ARMResource{}
	for _, r := range rs {
		byName[r.Name] = r
	}
	if len(byName) != 3 || byName["literaltags"].Tags["owner"] != "team-a" {
		t.Fatalf("golden shape: %+v", byName)
	}
	// With a model that reports expression-valued tags: the untagged resource
	// fails for each required tag, the expression one is uncertain, the
	// literal one has cost-center in a format that does not match.
	tm := tagModel{model: model(rs), expr: map[string]bool{"exprtags": true}}
	out := eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: tm, Policy: realPolicy()})
	var keys []string
	for _, f := range out.Findings {
		keys = append(keys, f.Resource.Name+"|"+f.Key)
		if f.Resource.Name == "exprtags" {
			t.Errorf("expression-valued tags must not fail: %+v", f)
		}
	}
	want := []string{"notags|missing:cost-center", "notags|missing:owner"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("findings %v want %v", keys, want)
	}
	if !strings.Contains(out.Findings[0].Evidence, "exprtags: tags is an ARM expression") {
		t.Errorf("first finding must keep the expression-valued resource visible: %q", out.Findings[0].Evidence)
	}
	// Only the expression resource: uncertain skip, never a pass.
	only := tagModel{model: model{byName["exprtags"]}, expr: tm.expr}
	out = eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: only, Policy: realPolicy()})
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, "uncertain") || len(out.Findings) != 0 {
		t.Fatalf("want uncertain skip, got %+v", out)
	}
	// A model that cannot tell the two cases apart must not invent a failure.
	out = eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{byName["notags"]}, Policy: realPolicy()})
	if out.Skipped == nil || len(out.Findings) != 0 || !strings.Contains(out.Skipped.Reason, "does not distinguish") {
		t.Fatalf("want uncertain skip naming the model limitation, got %+v", out)
	}
}

func TestOPS004TagValueExpressionIsUncertain(t *testing.T) {
	r := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "a", Tags: map[string]any{"owner": "[parameters('o')]", "cost-center": "cc1"}}
	out := eval(t, "FND-OPS-004", Providers{}, &sdk.Input{ARM: model{r}, Policy: realPolicy()})
	if out.Skipped == nil || !strings.HasPrefix(out.Skipped.Reason, "uncertain") {
		t.Fatalf("got %+v", out)
	}
}

func TestOPS003And005FindingsHaveKeys(t *testing.T) {
	acc := sdk.ARMResource{Type: "Microsoft.CognitiveServices/accounts", Name: "BAD_NAME", Kind: "AIServices"}
	out := eval(t, "FND-OPS-003", Providers{}, &sdk.Input{ARM: model{acc}})
	if len(out.Findings) != 2 {
		t.Fatalf("want two OPS-003 findings, got %+v", out)
	}
	if out.Findings[0].Key == "" || out.Findings[1].Key == "" || out.Findings[0].Key == out.Findings[1].Key {
		t.Errorf("OPS-003 findings need distinct keys: %+v", out.Findings)
	}
	st := sdk.ARMResource{Type: "Microsoft.Storage/storageAccounts", Name: "Bad-Name"}
	out = eval(t, "FND-OPS-005", Providers{}, &sdk.Input{ARM: model{st}})
	if len(out.Findings) != 1 || out.Findings[0].Key == "" {
		t.Errorf("OPS-005 finding needs a key: %+v", out)
	}
}
