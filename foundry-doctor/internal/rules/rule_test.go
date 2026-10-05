package rules_test

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type fake struct{}

func (fake) ID() string   { return "FND-CFG-001" }
func (fake) Version() int { return 1 }
func (f fake) Evaluate(context.Context, *model.Input) ([]rules.Result, error) {
	return []rules.Result{rules.SkipFor(f, rules.ReasonSyntheticInfrastructure, "", "compiled ARM template", sdk.ResourceRef{})}, nil
}

var _ rules.Rule = fake{}

func TestResultValidate(t *testing.T) {
	f := fake{}
	res, _ := f.Evaluate(context.Background(), &model.Input{})
	if err := res[0].Validate(); err != nil {
		t.Fatal(err)
	}
	bad := rules.Fail(sdk.Finding{RuleID: "x", Severity: sdk.SeverityError})
	if bad.Validate() == nil {
		t.Fatal("rule-set severity must be rejected")
	}
	if rules.Pass().Validate() != nil || rules.Fail(rules.NewFinding(f, sdk.ResourceRef{}, "e", "r")).Validate() != nil {
		t.Fatal("valid results rejected")
	}
	if got := rules.ReasonProfileKeyMissing(model.KeyTagsRequired); got != "profile-key-missing:policy.tags.required" {
		t.Fatal(got)
	}
}
