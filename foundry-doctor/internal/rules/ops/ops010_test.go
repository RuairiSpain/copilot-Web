package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func modelInput(t *testing.T, allow, deny []string, names ...string) *model.Input {
	t.Helper()
	in := input(arm(t, "models.arm.json", names...))
	in.Policy.Models = model.PolicyModels{Allow: allow, Deny: deny}
	return in
}

func scenarios010() scenarios {
	r := ops010{}
	return scenarios{
		"positive/model-in-both-allow-and-deny": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI/gpt-x"}, []string{"OpenAI/gpt-x"}, "acct/chat"))
			if !contains(keysOf(fails(got)), "allow-deny-conflict:OpenAI/gpt-x") {
				t.Fatalf("keys = %v", keysOf(fails(got)))
			}
		},
		"positive/deployed-model-not-in-allow-list": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI/gpt-x"}, nil, "acct/chat", "acct/mistral"))
			wantKeys(t, got, sdk.StatusFail, "not-allowed:mistral")
		},
		"positive/deployed-model-on-deny-list": func(t *testing.T) {
			got := eval(t, r, modelInput(t, nil, []string{"Mistral"}, "acct/chat", "acct/mistral"))
			wantKeys(t, got, sdk.StatusFail, "denied:mistral")
			if fails(got)[0].Confidence != sdk.ConfidenceLikely {
				t.Fatal("confidence")
			}
		},
		"positive/profile-allow-list-differs-from-policy-parameters": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI"}, nil, "pa-diff"))
			wantKeys(t, got, sdk.StatusFail, "policy-not-in-profile:Mistral")
			got = eval(t, r, modelInput(t, []string{"OpenAI", "Cohere"}, nil, "pa-match"))
			wantKeys(t, got, sdk.StatusFail, "profile-not-in-policy:Cohere")
		},
		"positive/publisher-case-differs-from-policy": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI"}, nil, "pa-case"))
			wantKeys(t, got, sdk.StatusFail, "publisher-case:OpenAI")
		},
		"positive/policy-deny-effect-with-empty-allow-lists": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI/gpt-x"}, nil, "pa-deny-empty"))
			wantKeys(t, got, sdk.StatusFail, "policy-deny-empty")
		},
		"negative/deployed-models-all-allowed-and-lists-match-policy": func(t *testing.T) {
			wantPass(t, eval(t, r, modelInput(t, []string{"OpenAI"}, []string{"Mistral/mistral-large/9"}, "acct/chat", "pa-match")))
		},
		"skipped/no-allow-or-deny-list-in-profile": func(t *testing.T) {
			got := eval(t, r, modelInput(t, nil, nil, "acct/chat"))
			wantSkip(t, got, "profile-key-missing:policy.models.allow")
			if len(got) != 1 {
				t.Fatal("must be a single skip")
			}
		},
		"skipped/policy-assignment-not-in-repository": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"OpenAI"}, nil, "acct/chat", "pa-other"))
			wantSkip(t, got, "not-applicable")
			if hasStatus(got, sdk.StatusPass) {
				t.Fatal("a skipped half must not pass")
			}
		},
		"skipped/model-router-deployment-before-phase-8": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"Mistral"}, nil, "acct/router"))
			wantSkip(t, got, "not-applicable")
			wantNoFindings(t, got)
		},
		"skipped/deployment-model-name-is-unresolved-parameter": func(t *testing.T) {
			got := eval(t, r, modelInput(t, []string{"Mistral"}, nil, "acct/param"))
			wantSkip(t, got, "unresolved-value:deployment-model")
			wantNoFindings(t, got)
		},
	}
}

func TestOPS010Extras(t *testing.T) {
	r := ops010{}
	t.Run("azure.yaml deployments without ARM", func(t *testing.T) {
		in := yamlInput(t, `name: demo
services:
  ai:
    host: azure.ai.project
    deployments:
      - name: chat
        model: { format: OpenAI, name: gpt-x, version: "1" }
      - name: other
        model: { format: Mistral, name: m, version: "2" }
`)
		in.Policy.Models.Allow = []string{"OpenAI/gpt-x/1"}
		got := eval(t, r, in)
		wantKeys(t, got, sdk.StatusFail, "not-allowed:other")
		wantSkip(t, got, "synthetic-infrastructure")
		if f := fails(got)[0]; f.Resource.Kind != "service" || f.Location.Line == 0 {
			t.Fatalf("finding = %+v", f)
		}
	})
	t.Run("versioned entry with unknown version is skipped", func(t *testing.T) {
		in := modelInput(t, []string{"OpenAI/gpt-x/1"}, nil, "acct/chat")
		in.ARM.Resources[0].Body["properties"] = map[string]any{"model": map[string]any{"format": "OpenAI", "name": "gpt-x"}}
		wantSkip(t, eval(t, r, in), "unresolved-value:deployment-model-version")
	})
	t.Run("version mismatch and three-part entries", func(t *testing.T) {
		wantKeys(t, eval(t, r, modelInput(t, []string{"OpenAI/gpt-x/2"}, nil, "acct/chat")), sdk.StatusFail, "not-allowed:chat")
		if matches("a/b/c/d", "a", "b", "c") {
			t.Fatal("four-part entry must not match")
		}
	})
	t.Run("unresolved policy parameter", func(t *testing.T) {
		got := eval(t, r, modelInput(t, []string{"OpenAI"}, nil, "pa-unresolved"))
		wantSkip(t, got, "unresolved-value:policy-parameters")
		wantNoFindings(t, got)
	})
	t.Run("multi-segment profile entries are not compared with publishers", func(t *testing.T) {
		wantNoFindings(t, eval(t, r, modelInput(t, []string{"OpenAI/gpt-x"}, nil, "pa-diff")))
	})
	t.Run("control plane half is always skipped", func(t *testing.T) {
		wantSkip(t, eval(t, r, modelInput(t, []string{"OpenAI"}, nil, "acct/chat")), "control-plane-unavailable")
	})
	t.Run("policy definition id as expression", func(t *testing.T) {
		in := modelInput(t, []string{"OpenAI"}, nil, "pa-match")
		in.ARM.Resources[0].Body["properties"].(map[string]any)["policyDefinitionId"] = "[tenantResourceId('Microsoft.Authorization/policyDefinitions','aafe3651-cb78-4f68-9f81-e7e41509110f')]"
		wantPass(t, eval(t, r, in))
	})
	t.Run("list values that are not strings", func(t *testing.T) {
		if _, ok := stringList(model.Value{State: model.Literal, Lit: []any{1.0}}); ok {
			t.Fatal("non-string accepted")
		}
		if _, ok := stringList(model.Value{State: model.Literal, Lit: "x"}); ok {
			t.Fatal("scalar accepted")
		}
	})
}

// richInputs return one input per rule that produces findings, for the cross-cutting tests.
func richInputs(t *testing.T) map[string]*model.Input {
	t.Helper()
	i4 := tagInput(t, "acct-missing", "acct-bad-format", "acct-ok")
	i5 := nameInput(t, nil, "stor-account-name-over-25", "search-svc-", "MyCosmos", "a")
	i9 := resInput(t, model.ResidencyDatazoneEU, "acct-us", "acct-eu/dep-gs", "acct-eu/dep-dev", "search-far")
	i10 := modelInput(t, []string{"OpenAI"}, []string{"OpenAI"}, "acct/chat", "acct/mistral", "pa-diff", "pa-case")
	i7 := repoInput(t, rules.ProfileProd, nil, "")
	return map[string]*model.Input{"FND-OPS-004": i4, "FND-OPS-005": i5, "FND-OPS-007": i7, "FND-OPS-009": i9, "FND-OPS-010": i10}
}

func TestCrossCutting(t *testing.T) {
	inputs := richInputs(t)
	for _, r := range Rules() {
		t.Run(r.ID(), func(t *testing.T) {
			in := inputs[r.ID()]
			first := eval(t, r, in)
			if !hasStatus(first, sdk.StatusFail) {
				t.Fatal("rich input produced no failure")
			}
			// Determinism.
			if second := eval(t, r, in); !reflect.DeepEqual(first, second) {
				t.Fatal("results differ between runs")
			}
			seen := map[string]bool{}
			for _, f := range append(byStatus(first, sdk.StatusFail), byStatus(first, sdk.StatusUncertain)...) {
				if f.RuleID != r.ID() || f.RuleVersion != r.Version() || f.Severity != "" || f.Profile != "" || f.Fingerprint != "" {
					t.Fatalf("stamped fields wrong: %+v", f)
				}
				fp := findings.Fingerprint(f)
				if seen[fp] {
					t.Fatalf("two findings share fingerprint %s (key %s)", fp, f.Key)
				}
				seen[fp] = true
				// Fingerprints ignore position and prose.
				moved := f
				moved.Location.Line += 10
				moved.Evidence += " changed"
				if findings.Fingerprint(moved) != fp {
					t.Fatal("fingerprint depends on line or prose")
				}
			}
			// A cancelled context does not matter: rules do no I/O beyond the input.
			if len(eval(t, r, in)) != len(first) {
				t.Fatal("unstable result count")
			}
		})
	}
}

func TestRegistrationAndVersions(t *testing.T) {
	ids := map[string]bool{}
	for i, r := range Rules() {
		if r.Version() != 1 || ids[r.ID()] {
			t.Fatalf("rule %s", r.ID())
		}
		ids[r.ID()] = true
		if i > 0 && Rules()[i-1].ID() >= r.ID() {
			t.Fatal("rules not in ID order")
		}
	}
	for _, id := range []string{"FND-OPS-004", "FND-OPS-005", "FND-OPS-007", "FND-OPS-009", "FND-OPS-010"} {
		if !ids[id] {
			t.Fatalf("%s not registered", id)
		}
	}
}

func TestSyntheticGuard(t *testing.T) {
	for _, r := range []rules.Rule{ops004{}, ops005{}} {
		in := &model.Input{Project: model.Project{IaC: model.IaCSynthetic, Name: "p"}, Profile: rules.ProfileProd}
		in.Policy.Tags.Required = []model.TagRequirement{{Name: "a"}}
		got := eval(t, r, in)
		if len(got) != 1 || got[0].Skip.Reason != rules.ReasonSyntheticInfrastructure || got[0].Skip.MissingCapability == "" {
			t.Fatalf("%s: %+v", r.ID(), got)
		}
	}
}

func TestEvidenceRedactionCanary(t *testing.T) {
	// A secret-shaped value in an azd environment value, tag value or template name must not reach any text field.
	secret := "Account" + "Key=" + strings.Repeat("Zx", 8) + "/canary"
	env := model.NewEnvironment("dev", map[string]string{"AZURE_ENV_NAME": secret, "AZURE_LOCATION": secret})
	in := nameInput(t, nil, "[concat(parameters('environmentName'), '-foundry-account-with-a-long-suffix-aaaaaaa')]")
	in.Environment = &env
	for _, f := range fails(eval(t, ops005{}, in)) {
		if strings.Contains(f.Evidence+f.Recommendation+f.Fix+f.Key, secret) {
			t.Fatal("environment value leaked into the finding")
		}
	}
	in9 := resInput(t, model.ResidencyDatazoneEU, "acct-eu")
	in9.Environment = &env
	got := fails(eval(t, ops009{}, in9))
	if len(got) != 1 || got[0].Key != "env-location" {
		t.Fatalf("findings = %+v", got)
	}
	if strings.Contains(got[0].Evidence+got[0].Key, secret) {
		t.Fatal("AZURE_LOCATION value leaked")
	}
}
