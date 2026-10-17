package cost

import (
	"context"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func run(t *testing.T, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	res, err := find(Register(), id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func kind(res sdk.Result) string {
	switch {
	case len(res.Findings) > 0:
		return "finding"
	case res.Skipped != nil:
		return "skipped"
	}
	return "pass"
}

func notification(thresholdType string, contacts map[string]any) map[string]any {
	n := map[string]any{"enabled": true, "operator": "GreaterThan", "threshold": float64(80), "thresholdType": thresholdType}
	for k, v := range contacts {
		n[k] = v
	}
	return n
}

func budget(notifs map[string]any) sdk.ARMResource {
	return sdk.ARMResource{Type: typeBudget, Name: "b", Properties: map[string]any{"properties": map[string]any{
		"category":      "Cost",
		"amount":        float64(500),
		"timeGrain":     "Monthly",
		"timePeriod":    map[string]any{"startDate": "2026-11-01T00:00:00Z"},
		"notifications": notifs,
	}}}
}

func TestCost001CompiledBicepShapes(t *testing.T) {
	email := map[string]any{"contactEmails": []any{"finops@example.com"}}
	paramEmails := map[string]any{"contactEmails": "[parameters('budgetContactEmails')]"}
	good := map[string]any{"a": notification("Actual", email), "f": notification("Forecasted", email)}
	withCond := func(r sdk.ARMResource, cond string) sdk.ARMResource {
		r.Conditional, r.Condition = true, cond
		return r
	}
	cases := []struct {
		name string
		arm  model
		want string
	}{
		{"valid budget", model{budget(good)}, "pass"},
		{"contactEmails from parameters() is unresolved, not empty", model{budget(map[string]any{"a": notification("Actual", paramEmails), "f": notification("Forecasted", paramEmails)})}, "skipped"},
		{"contactEmails list holding an expression is unresolved", model{budget(map[string]any{"a": notification("Actual", map[string]any{"contactEmails": []any{"[parameters('mail')]"}}), "f": notification("Forecasted", map[string]any{"contactEmails": []any{"[parameters('mail')]"}})})}, "skipped"},
		{"escaped [[ is a literal contact", model{budget(map[string]any{"a": notification("Actual", map[string]any{"contactEmails": "[[x@example.com"}), "f": notification("Forecasted", map[string]any{"contactEmails": []any{"[[x@example.com"}})})}, "pass"},
		{"empty contact list is a finding", model{budget(map[string]any{"a": notification("Actual", map[string]any{"contactEmails": []any{}})})}, "finding"},
		{"no forecasted notification", model{budget(map[string]any{"a": notification("Actual", email)})}, "finding"},
		{"thresholdType from a parameter is unresolved", model{budget(map[string]any{"a": notification("[parameters('tt')]", email)})}, "skipped"},
		{"no notifications object", model{budget(nil)}, "finding"},
		{"literal false budget does not satisfy the rule", model{withCond(budget(good), "[false()]")}, "skipped"},
		{"literal false budget alongside a bad one", model{withCond(budget(good), "false"), budget(map[string]any{})}, "finding"},
		{"parameter-driven budget is uncertain", model{withCond(budget(good), "[parameters('createBudget')]")}, "skipped"},
		{"looped budget is uncertain", model{func() sdk.ARMResource { r := budget(good); r.Looped = true; return r }()}, "skipped"},
		{"no budget in template is externally managed, skipped", model{}, "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "FND-COST-001", &sdk.Input{ARM: tc.arm})
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	t.Run("expression notifications object", func(t *testing.T) {
		b := budget(nil)
		b.Properties["properties"].(map[string]any)["notifications"] = "[variables('notifications')]"
		if got := run(t, "FND-COST-001", &sdk.Input{ARM: model{b}}); kind(got) != "skipped" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("missing ARM", func(t *testing.T) {
		if got := run(t, "FND-COST-001", &sdk.Input{}); kind(got) != "skipped" {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("skip reason names external management", func(t *testing.T) {
		got := run(t, "FND-COST-001", &sdk.Input{ARM: model{}})
		if got.Skipped == nil || got.Skipped.Reason != "budget-managed-outside-repository" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestCost002CaseConditionsAndOptions(t *testing.T) {
	dev := func(limit int, rs ...sdk.ARMResource) *sdk.Input {
		return &sdk.Input{Profile: "foundry-dev", Policy: pol{"policy.cost.devMaxCosmosThroughput": limit}, ARM: model(rs)}
	}
	cond := func(r sdk.ARMResource, c string) sdk.ARMResource { r.Conditional, r.Condition = true, c; return r }
	ptu := sdk.ARMResource{Type: typeDeployment, Name: "[format('{0}/{1}', 'acct', 'chat')]", SKUName: "globalprovisionedmanaged"}
	sqlDB := func(options map[string]any) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases", Name: "[format('{0}/{1}', 'acct', 'db')]", Properties: map[string]any{"options": options}}
	}
	container := func(options map[string]any) sdk.ARMResource {
		return sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/containers", Name: "acct/db/c", Properties: map[string]any{"options": options}}
	}
	cases := []struct {
		name string
		in   *sdk.Input
		want string
	}{
		{"lower-case PTU deployment SKU", dev(4000, ptu), "finding"},
		{"mixed-case APIM Premium", dev(4000, sdk.ARMResource{Type: typeAPIM, Name: "a", SKUName: "premium", SKU: map[string]any{"capacity": float64(1)}}), "finding"},
		{"literal false resource is not deployed", dev(4000, cond(ptu, "[false()]")), "pass"},
		{"parameter condition is uncertain", dev(4000, cond(ptu, "[parameters('usePtu')]")), "skipped"},
		{"database options.throughput above limit", dev(4000, sqlDB(map[string]any{"throughput": float64(10000)})), "finding"},
		{"database options.throughput at limit", dev(4000, sqlDB(map[string]any{"throughput": float64(4000)})), "pass"},
		{"container options.autoscaleSettings above limit", dev(4000, container(map[string]any{"autoscaleSettings": map[string]any{"maxThroughput": float64(9000)}})), "finding"},
		{"options.throughput from a parameter", dev(4000, sqlDB(map[string]any{"throughput": "[parameters('ru')]"})), "skipped"},
		{"policy limit below 3000 is used as configured", dev(1000, sqlDB(map[string]any{"throughput": float64(2000)})), "finding"},
		{"policy limit of 3000 allows 3000", dev(3000, sqlDB(map[string]any{"throughput": float64(3000)})), "pass"},
		{"throughputSettings above limit", dev(1000, sdk.ARMResource{Type: "Microsoft.DocumentDB/databaseAccounts/sqlDatabases/throughputSettings", Name: "t", Properties: map[string]any{"resource": map[string]any{"throughput": float64(2000)}}}), "finding"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "FND-COST-002", tc.in)
			if k := kind(got); k != tc.want {
				t.Fatalf("got %s want %s: %+v", k, tc.want, got)
			}
		})
	}
	t.Run("no policy limit skips", func(t *testing.T) {
		in := dev(0, sqlDB(map[string]any{"throughput": float64(10000)}))
		in.Policy = pol{}
		if got := run(t, "FND-COST-002", in); kind(got) != "skipped" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestCost004ConditionalAndCase(t *testing.T) {
	ptu := sdk.ARMResource{Type: typeDeployment, Name: "d", SKUName: "provisionedmanaged"}
	if got := run(t, "FND-COST-004", &sdk.Input{ARM: model{ptu}}); kind(got) != "finding" {
		t.Fatalf("got %+v", got)
	}
	ptu.Conditional, ptu.Condition = true, "false"
	if got := run(t, "FND-COST-004", &sdk.Input{ARM: model{ptu}}); kind(got) != "skipped" {
		t.Fatalf("got %+v", got)
	}
	ptu.Condition = "[parameters('x')]"
	got := run(t, "FND-COST-004", &sdk.Input{ARM: model{ptu}})
	if got.Skipped == nil || got.Skipped.Reason != "conditional-deployment-unresolved" {
		t.Fatalf("got %+v", got)
	}
}

func TestExprHelpers(t *testing.T) {
	for s, want := range map[string]bool{"[parameters('a')]": true, "[[x": false, "x": false, "": false} {
		if isExpr(s) != want {
			t.Errorf("isExpr(%q) != %v", s, want)
		}
	}
	for _, tc := range []struct {
		r    sdk.ARMResource
		want deployState
	}{
		{sdk.ARMResource{}, stateLive},
		{sdk.ARMResource{Conditional: true, Condition: "[false()]"}, stateAbsent},
		{sdk.ARMResource{Conditional: true, Condition: "false"}, stateAbsent},
		{sdk.ARMResource{Conditional: true, Condition: "[true()]"}, stateLive},
		{sdk.ARMResource{Conditional: true, Condition: "[parameters('p')]"}, stateUncertain},
		{sdk.ARMResource{Conditional: true}, stateUncertain},
		{sdk.ARMResource{Looped: true}, stateUncertain},
		{sdk.ARMResource{Conditional: true, Condition: "[false()]", Looped: true}, stateAbsent},
	} {
		if got := stateOf(tc.r); got != tc.want {
			t.Errorf("stateOf(%+v)=%v want %v", tc.r, got, tc.want)
		}
	}
}
