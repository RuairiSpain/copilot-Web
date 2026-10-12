package sec_test

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const id001 = "FND-SEC-001"

func hosting(props obj, extra ...obj) []obj {
	return append([]obj{account("acct", props), project("acct", "proj")}, extra...)
}

func cases001() []tcase {
	return []tcase{
		{"account with projects and disableLocalAuth true passes", func(t *testing.T) {
			out := eval(t, id001, inputOf(t, hosting(obj{"disableLocalAuth": true})))
			count(out).want(t, 1, 0, 0, 0)
		}},
		{"live account reports disableLocalAuth true", func(t *testing.T) {
			// The live half is not read offline: it is reported once as skipped by design.
			out := eval(t, id001, inputOf(t, hosting(obj{"disableLocalAuth": true})))
			if !liveSkipped(out) {
				t.Fatal("expected the control-plane-unavailable skip for the live half")
			}
		}},
		{"disableLocalAuth false or absent on an account with projects fails", func(t *testing.T) {
			for name, props := range map[string]obj{"false": {"disableLocalAuth": false}, "absent": {"kind": "x"}} {
				t.Run(name, func(t *testing.T) {
					c := count(eval(t, id001, inputOf(t, hosting(props))))
					c.want(t, 0, 1, 0, 0)
					f := c.findings[0]
					if f.Key != "disableLocalAuth" || f.Confidence != sdk.ConfidenceCertain || f.Resource.Pointer != "/resources/0" ||
						f.Location.Pointer != "/resources/0/properties/disableLocalAuth" || f.Recommendation == "" || f.Fix == "" {
						t.Fatalf("finding = %+v", f)
					}
					if f.Severity != "" || f.Fingerprint != "" || f.Profile != "" {
						t.Fatalf("rule set engine-owned fields: %+v", f)
					}
				})
			}
		}},
		{"hosting account in prod profile reports error", func(t *testing.T) {
			f := executeOne(t, id001, inputOf(t, hosting(obj{"disableLocalAuth": false})), rules.ProfileProd)
			if f.Severity != sdk.SeverityError {
				t.Fatalf("severity = %s", f.Severity)
			}
			if dev := executeOne(t, id001, inputOf(t, hosting(obj{"disableLocalAuth": false})), rules.ProfileDev); dev.Severity != sdk.SeverityWarning {
				t.Fatalf("dev severity = %s", dev.Severity)
			}
		}},
		{"disableLocalAuth driven by an unresolved parameter is skipped with reason", func(t *testing.T) {
			in := build(t, obj{"p": obj{"type": "bool"}}, nil,
				account("acct", obj{"disableLocalAuth": "[parameters('p')]"}), project("acct", "proj"))
			c := count(eval(t, id001, in))
			c.want(t, 0, 0, 0, 1)
			if !c.skipReason("unresolved-value:") {
				t.Fatalf("skips = %+v", c.skips)
			}
		}},
		{"no Reader permission in live mode is skipped", func(t *testing.T) {
			out := eval(t, id001, inputOf(t, hosting(obj{"disableLocalAuth": true})))
			for _, r := range out {
				if r.Status == sdk.StatusSkipped && r.Skip.Reason == rules.ReasonControlPlaneUnavailable {
					return
				}
			}
			t.Fatal("no control-plane-unavailable skip")
		}},
	}
}

func TestFND_SEC_001(t *testing.T) { runCases(t, cases001()) }

func TestSEC001_ParameterForms(t *testing.T) {
	params := obj{
		"withDefault": obj{"type": "bool", "defaultValue": true},
		"noDefault":   obj{"type": "bool"},
	}
	tests := []struct {
		name     string
		prop     any
		supplied obj
		want     [4]int // pass, fail, uncertain, skip
	}{
		{"literal true", true, nil, [4]int{1, 0, 0, 0}},
		{"parameter with default true", "[parameters('withDefault')]", nil, [4]int{1, 0, 0, 0}},
		{"parameter with default overridden false", "[parameters('withDefault')]", obj{"withDefault": false}, [4]int{0, 1, 0, 0}},
		{"parameter without default", "[parameters('noDefault')]", nil, [4]int{0, 0, 0, 1}},
		{"parameter without default supplied", "[parameters('noDefault')]", obj{"noDefault": true}, [4]int{1, 0, 0, 0}},
		{"string instead of bool", "true", nil, [4]int{0, 0, 0, 1}},
		{"mixed expression", "[concat('a', 'b')]", nil, [4]int{0, 0, 0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := build(t, params, tt.supplied, account("acct", obj{"disableLocalAuth": tt.prop}), project("acct", "proj"))
			c := count(eval(t, id001, in))
			c.want(t, tt.want[0], tt.want[1], tt.want[2], tt.want[3])
		})
	}
}

func TestSEC001_Scope(t *testing.T) {
	t.Run("account without projects or deployments is not applicable", func(t *testing.T) {
		c := count(eval(t, id001, inputOf(t, []obj{account("acct", obj{"disableLocalAuth": false})})))
		c.want(t, 0, 0, 0, 1)
		if !c.skipReason(rules.ReasonNotApplicable) {
			t.Fatalf("skips = %+v", c.skips)
		}
	})
	t.Run("deployments alone make the account a host", func(t *testing.T) {
		dep := res("Microsoft.CognitiveServices/accounts/deployments", "acct/gpt", obj{})
		c := count(eval(t, id001, inputOf(t, []obj{account("acct", nil), dep})))
		c.want(t, 0, 1, 0, 0)
	})
	t.Run("orphan project makes the result uncertain", func(t *testing.T) {
		c := count(eval(t, id001, inputOf(t, []obj{account("acct", nil), project("other", "p")})))
		c.want(t, 0, 0, 1, 0)
		if c.findings[0].Confidence != sdk.ConfidenceUncertain {
			t.Fatalf("confidence = %s", c.findings[0].Confidence)
		}
	})
	t.Run("condition false is not deployed", func(t *testing.T) {
		in := inputOf(t, hosting(nil))
		in.ARM.Resources[0].Meta.Conditional = true
		in.ARM.Resources[0].Meta.ConditionValue = model.Value{State: model.Literal, Lit: false}
		c := count(eval(t, id001, in))
		c.want(t, 0, 0, 0, 1)
	})
	t.Run("conditional account is uncertain", func(t *testing.T) {
		in := build(t, obj{"on": obj{"type": "bool"}}, nil,
			account("acct", nil, obj{"condition": "[parameters('on')]"}), project("acct", "proj"))
		c := count(eval(t, id001, in))
		c.want(t, 0, 0, 1, 0)
	})
	t.Run("conditional with literal true condition fails", func(t *testing.T) {
		in := build(t, obj{"on": obj{"type": "bool", "defaultValue": true}}, nil,
			account("acct", nil, obj{"condition": "[parameters('on')]"}), project("acct", "proj"))
		count(eval(t, id001, in)).want(t, 0, 1, 0, 0)
	})
	t.Run("looped account is uncertain", func(t *testing.T) {
		in := inputOf(t, []obj{account("acct", nil, obj{"copy": obj{"name": "c", "count": 2}}), project("acct", "proj")})
		count(eval(t, id001, in)).want(t, 0, 0, 1, 0)
	})
	t.Run("module-declared account is likely", func(t *testing.T) {
		inner := obj{"resources": []obj{account("acct", nil), project("acct", "proj")}}
		mod := res("Microsoft.Resources/deployments", "m", obj{"mode": "Incremental", "template": inner})
		c := count(eval(t, id001, inputOf(t, []obj{mod})))
		c.want(t, 0, 1, 0, 0)
		if c.findings[0].Confidence != sdk.ConfidenceLikely {
			t.Fatalf("confidence = %s", c.findings[0].Confidence)
		}
	})
}

func TestSEC001_V2SymbolicForm(t *testing.T) {
	// languageVersion 2.0: children are tied to the account by symbolic name.
	raw := []byte(`{"languageVersion":"2.0","resources":{
	  "acct":{"type":"Microsoft.CognitiveServices/accounts","apiVersion":"2025-06-01","name":"a","properties":{"disableLocalAuth":false}},
	  "acct::project":{"type":"Microsoft.CognitiveServices/accounts/projects","apiVersion":"2025-06-01","name":"[format('{0}/{1}', 'a', 'p')]","dependsOn":["acct"]}}}`)
	count(eval(t, id001, normalise(t, raw, nil))).want(t, 0, 1, 0, 0)
}

// executeOne runs the rule through the real executor with the catalogue entry and returns its only finding.
func executeOne(t *testing.T, id string, in *model.Input, profile string) sdk.Finding {
	t.Helper()
	out := execute(t, id, in, profile)
	if len(out.Findings) != 1 {
		t.Fatalf("findings = %+v, skipped = %+v, errors = %v", out.Findings, out.Skipped, out.Errors)
	}
	return out.Findings[0]
}

func TestSEC001_NilAndMissingInput(t *testing.T) {
	r := ruleByID(t, id001)
	for name, in := range map[string]*model.Input{
		"nil input":     nil,
		"synthetic":     {Project: model.Project{IaC: model.IaCSynthetic}},
		"compile fails": {Project: model.Project{IaC: model.IaCBicep}},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := r.Evaluate(context.Background(), in)
			if err != nil || len(out) != 1 || out[0].Status != sdk.StatusSkipped {
				t.Fatalf("out = %+v err = %v", out, err)
			}
			if name == "compile fails" {
				if out[0].Skip.Reason != "unresolved-value:compiled-arm-unavailable" {
					t.Fatalf("reason = %s", out[0].Skip.Reason)
				}
			} else if out[0].Skip.Reason != rules.ReasonSyntheticInfrastructure {
				t.Fatalf("reason = %s", out[0].Skip.Reason)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Evaluate(ctx, inputOf(t, nil)); err == nil {
		t.Fatal("expected the context error")
	}
}
