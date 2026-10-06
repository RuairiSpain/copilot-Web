package cost

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeBudget     = "Microsoft.Consumption/budgets"
	typeSearch     = "Microsoft.Search/searchServices"
	typeAPIM       = "Microsoft.ApiManagement/service"
	typeDeployment = "Microsoft.CognitiveServices/accounts/deployments"
)

// Register returns the currently implemented COST rules.
func Register() []sdk.Rule {
	return []sdk.Rule{
		rule{id: "FND-COST-001", fn: eval001},
		rule{id: "FND-COST-002", fn: eval002},
	}
}

type rule struct {
	id string
	fn func(*sdk.Input) sdk.Result
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if err := ctx.Err(); err != nil {
		return sdk.Result{}, fmt.Errorf("%s: %w", r.id, err)
	}
	if in == nil || in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}, nil
	}
	return r.fn(in), nil
}

type outcome struct {
	findings   []sdk.Finding
	unresolved bool
	skip       string
}

func (o *outcome) add(r sdk.ARMResource, evidence string) {
	o.findings = append(o.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
		Location: r.Location,
		Evidence: evidence,
	})
}

func (o *outcome) result() sdk.Result {
	if len(o.findings) > 0 {
		return sdk.Result{Findings: o.findings}
	}
	if o.unresolved {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "unresolved-expression"}}
	}
	if o.skip != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: o.skip}}
	}
	return sdk.Result{}
}

func eval001(in *sdk.Input) sdk.Result {
	o := &outcome{}
	budgets := byType(in.ARM, typeBudget)
	if len(budgets) == 0 {
		return sdk.Result{Findings: []sdk.Finding{{Evidence: "no Microsoft.Consumption/budgets resource was found in the compiled plan"}}}
	}
	valid := false
	for _, b := range budgets {
		props := mapAt(b.Properties, "properties")
		if props == nil {
			props = b.Properties
		}
		if category := str(props["category"]); category != "" && !strings.EqualFold(category, "Cost") {
			o.add(b, "budget category is not Cost")
			continue
		}
		if amount, ok := number(props["amount"]); ok && amount <= 0 {
			o.add(b, "budget amount must be positive")
		}
		if str(props["timeGrain"]) == "" {
			o.add(b, "budget timeGrain is missing")
		}
		if tp := mapAt(props, "timePeriod"); tp == nil || str(tp["startDate"]) == "" {
			o.add(b, "budget timePeriod.startDate is missing")
		}
		notifications := mapAt(props, "notifications")
		if notifications == nil {
			o.add(b, "budget has no notifications")
			continue
		}
		if len(notifications) > 5 {
			o.add(b, "budget has more than five notifications")
		}
		var enabled, forecasted bool
		for _, raw := range notifications {
			n, _ := raw.(map[string]any)
			if n == nil {
				continue
			}
			if strings.HasPrefix(str(n["enabled"]), "[") || strings.HasPrefix(str(n["thresholdType"]), "[") {
				o.unresolved = true
				continue
			}
			if truthy(n["enabled"]) && str(n["operator"]) != "" && hasContact(n) {
				enabled = true
				if strings.EqualFold(str(n["thresholdType"]), "Forecasted") {
					forecasted = true
				}
			}
		}
		if !enabled {
			o.add(b, "budget has no enabled notification with a contact")
			continue
		}
		if !forecasted {
			o.add(b, "budget has no forecasted-cost notification")
			continue
		}
		valid = true
	}
	if valid && len(o.findings) == 0 {
		return sdk.Result{}
	}
	return o.result()
}

func eval002(in *sdk.Input) sdk.Result {
	if !isDev(in) {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "environment-not-classified-as-dev"}}
	}
	o := &outcome{}
	limit, hasLimit := cosmosLimit(in)
	exemptions := exemptionSet(in)
	for _, r := range in.ARM.Resources() {
		if exempt(exemptions, r.Name) {
			continue
		}
		switch {
		case strings.EqualFold(r.Type, typeDeployment):
			switch r.SKUName {
			case "GlobalProvisionedManaged", "DataZoneProvisionedManaged", "ProvisionedManaged":
				o.add(r, "development environment uses provisioned throughput model capacity")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
		case strings.EqualFold(r.Type, typeSearch):
			switch strings.ToLower(r.SKUName) {
			case "standard2", "standard3", "storage_optimized_l1", "storage_optimized_l2":
				o.add(r, "development environment uses a production-sized Search tier")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
			if v, ok := intAt(r.Properties, "replicaCount"); ok {
				if v > 1 {
					o.add(r, "development environment uses more than one Search replica")
				}
			} else if propUnresolved(r.Properties, "replicaCount") {
				o.unresolved = true
			}
			if v, ok := intAt(r.Properties, "partitionCount"); ok {
				if v > 1 {
					o.add(r, "development environment uses more than one Search partition")
				}
			} else if propUnresolved(r.Properties, "partitionCount") {
				o.unresolved = true
			}
		case strings.EqualFold(r.Type, typeAPIM):
			switch r.SKUName {
			case "Premium", "Isolated":
				o.add(r, "development environment uses a production-sized API Management tier")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
			if v, ok := skuCapacity(r); ok {
				if v > 1 {
					o.add(r, "development environment uses more than one API Management unit")
				}
			} else if r.SKU != nil && propUnresolved(r.SKU, "capacity") {
				o.unresolved = true
			}
		case strings.HasSuffix(strings.ToLower(r.Type), "/throughputsettings"):
			if throughput, ok := cosmosThroughput(r); ok {
				if hasLimit && throughput > float64(max(limit, 3000)) {
					o.add(r, "development environment Cosmos DB throughput exceeds the configured policy limit")
				}
			} else if propThroughputUnresolved(r) {
				o.unresolved = true
			}
			if autoscale, ok := cosmosAutoscale(r); ok {
				if hasLimit && autoscale > max(limit, 3000) {
					o.add(r, "development environment Cosmos DB autoscale maxThroughput exceeds the configured policy limit")
				}
			} else if propAutoscaleUnresolved(r) {
				o.unresolved = true
			}
		}
	}
	if !hasLimit {
		o.skip = sdk.SkipMissingPolicyKey("policy.cost.devMaxCosmosThroughput")
	}
	return o.result()
}

func byType(m sdk.ARMModel, typ string) []sdk.ARMResource {
	var out []sdk.ARMResource
	if m == nil {
		return nil
	}
	for _, r := range m.Resources() {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, r)
		}
	}
	return out
}

func isDev(in *sdk.Input) bool {
	switch strings.ToLower(strings.TrimSpace(in.Profile)) {
	case "dev", "foundry-dev":
		return true
	case "test", "foundry-test", "prod", "foundry-prod":
		return false
	}
	if in.Policy != nil {
		if v, ok := in.Policy.Get("policy.environments.development"); ok {
			for _, name := range stringsFrom(v) {
				if strings.EqualFold(name, in.Environment) {
					return true
				}
			}
		}
	}
	return false
}

func cosmosLimit(in *sdk.Input) (int, bool) {
	if in == nil || in.Policy == nil {
		return 0, false
	}
	v, ok := in.Policy.Get("policy.cost.devMaxCosmosThroughput")
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case float64:
		return int(x), true
	}
	return 0, false
}

func exemptionSet(in *sdk.Input) map[string]bool {
	out := map[string]bool{}
	if in == nil || in.Policy == nil {
		return out
	}
	if v, ok := in.Policy.Get("policy.cost.productionSizedSkuExemptions"); ok {
		for _, s := range stringsFrom(v) {
			out[strings.ToLower(s)] = true
		}
	}
	return out
}

func exempt(set map[string]bool, name string) bool {
	return set[strings.ToLower(strings.TrimSpace(name))]
}

func stringsFrom(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func mapAt(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return ""
	}
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	default:
		return false
	}
}

func hasContact(m map[string]any) bool {
	for _, k := range []string{"contactEmails", "contactRoles", "contactGroups"} {
		if xs := stringsFrom(m[k]); len(xs) > 0 {
			return true
		}
	}
	return false
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		n, err := strconv.ParseFloat(x, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func intAt(m map[string]any, key string) (int, bool) {
	n, ok := number(m[key])
	return int(n), ok
}

func propUnresolved(m map[string]any, key string) bool {
	s, ok := m[key].(string)
	return ok && strings.HasPrefix(s, "[")
}

func skuUnresolved(r sdk.ARMResource) bool {
	if r.SKU == nil {
		return false
	}
	return propUnresolved(r.SKU, "name")
}

func skuCapacity(r sdk.ARMResource) (float64, bool) {
	if r.SKU == nil {
		return 0, false
	}
	return number(r.SKU["capacity"])
}

func cosmosThroughput(r sdk.ARMResource) (float64, bool) {
	resource := mapAt(r.Properties, "resource")
	if resource == nil {
		return 0, false
	}
	return number(resource["throughput"])
}

func cosmosAutoscale(r sdk.ARMResource) (int, bool) {
	resource := mapAt(r.Properties, "resource")
	if resource == nil {
		return 0, false
	}
	auto := mapAt(resource, "autoscaleSettings")
	if auto == nil {
		return 0, false
	}
	n, ok := number(auto["maxThroughput"])
	return int(n), ok
}

func propThroughputUnresolved(r sdk.ARMResource) bool {
	resource := mapAt(r.Properties, "resource")
	return propUnresolved(resource, "throughput")
}

func propAutoscaleUnresolved(r sdk.ARMResource) bool {
	resource := mapAt(r.Properties, "resource")
	if resource == nil {
		return false
	}
	auto := mapAt(resource, "autoscaleSettings")
	return propUnresolved(auto, "maxThroughput")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
