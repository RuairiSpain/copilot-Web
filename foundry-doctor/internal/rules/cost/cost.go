package cost

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	runtimegw "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/gateway"
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
		rule{id: "FND-COST-003", fn: eval003},
		rule{id: "FND-COST-004", fn: eval004},
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
	// cond is set when a conditional or looped resource that the rule
	// inspects could not be classified as deployed or not.
	cond bool
	skip string
}

func (o *outcome) add(r sdk.ARMResource, evidence string) {
	o.findings = append(o.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
		Location: r.Location,
		Evidence: armexpr.StripScope(evidence),
	})
}

// addKey adds a finding with a stable Key that separates several findings of
// one rule on one resource.
func (o *outcome) addKey(r sdk.ARMResource, key, evidence string) {
	o.add(r, evidence)
	o.findings[len(o.findings)-1].Key = key
}

func (o *outcome) result() sdk.Result {
	if len(o.findings) > 0 {
		// What was not evaluated is returned as Skips next to the findings
		// (review M3, ADR-008 amendment 3).
		var notes []string
		if o.unresolved {
			notes = append(notes, "unresolved-expression: a decisive value could not be resolved")
		}
		if o.cond {
			notes = append(notes, "conditional-deployment-unresolved: a resource may not be deployed")
		}
		if o.skip != "" {
			notes = append(notes, o.skip)
		}
		var skips []sdk.Skip
		for _, n := range notes {
			skips = append(skips, sdk.Skip{Reason: armexpr.StripScope(n)})
		}
		return sdk.Result{Findings: o.findings, Skips: skips}
	}
	if o.unresolved {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "unresolved-expression"}}
	}
	if o.cond {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "conditional-deployment-unresolved"}}
	}
	if o.skip != "" {
		return sdk.Result{Skipped: &sdk.Skip{Reason: armexpr.StripScope(o.skip)}}
	}
	return sdk.Result{}
}

func eval001(in *sdk.Input) sdk.Result {
	o := &outcome{}
	live, unc := liveResources(in.ARM)
	var budgets []sdk.ARMResource
	for _, r := range live {
		if strings.EqualFold(r.Type, typeBudget) {
			budgets = append(budgets, r)
		}
	}
	for _, r := range unc {
		if strings.EqualFold(r.Type, typeBudget) {
			o.cond = true
		}
	}
	if len(budgets) == 0 {
		if o.cond {
			return o.result()
		}
		// A budget is a subscription or resource-group object that is often
		// created outside the project repository, so its absence from the plan
		// is not proof that none exists (FND-COST-001 evidence).
		return sdk.Result{Skipped: &sdk.Skip{Reason: "budget-managed-outside-repository"}}
	}
	valid := false
	for _, b := range budgets {
		props := mapAt(b.Properties, "properties")
		if props == nil {
			props = b.Properties
		}
		if category := str(props["category"]); category != "" && !strings.EqualFold(category, "Cost") {
			o.addKey(b, "budget-category-is-not-cost", "budget category is not Cost")
			continue
		}
		if amount, ok := number(props["amount"]); ok && amount <= 0 {
			o.addKey(b, "budget-amount-must-be-positive", "budget amount must be positive")
		}
		if str(props["timeGrain"]) == "" {
			o.addKey(b, "budget-timegrain-is-missing", "budget timeGrain is missing")
		}
		if tp := mapAt(props, "timePeriod"); tp == nil || str(tp["startDate"]) == "" {
			if s, ok := props["timePeriod"].(string); !(ok && isExpr(s)) {
				o.addKey(b, "budget-timeperiod-startdate-is-missing", "budget timePeriod.startDate is missing")
			}
		}
		if s, ok := props["notifications"].(string); ok && isExpr(s) {
			o.unresolved = true
			continue
		}
		notifications := mapAt(props, "notifications")
		if notifications == nil {
			o.addKey(b, "budget-has-no-notifications", "budget has no notifications")
			continue
		}
		if len(notifications) > 5 {
			o.addKey(b, "budget-has-more-than-five-notifications", "budget has more than five notifications")
		}
		var enabled, forecasted, unresolved bool
		for _, raw := range notifications {
			n, _ := raw.(map[string]any)
			if n == nil {
				continue
			}
			if isExpr(str(n["enabled"])) || isExpr(str(n["thresholdType"])) || isExpr(str(n["operator"])) {
				unresolved = true
				continue
			}
			if !truthy(n["enabled"]) || str(n["operator"]) == "" {
				continue
			}
			has, contactUnresolved := contactStatus(n)
			if contactUnresolved {
				unresolved = true
			}
			if has {
				enabled = true
				if strings.EqualFold(str(n["thresholdType"]), "Forecasted") {
					forecasted = true
				}
			}
		}
		if !enabled {
			if unresolved {
				o.unresolved = true
				continue
			}
			o.addKey(b, "budget-has-no-enabled-notification-with-a-contact", "budget has no enabled notification with a contact")
			continue
		}
		if !forecasted {
			if unresolved {
				o.unresolved = true
				continue
			}
			o.addKey(b, "budget-has-no-forecasted-cost-notification", "budget has no forecasted-cost notification")
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
	live, unc := liveResources(in.ARM)
	for _, r := range unc {
		if !exempt(exemptions, r.Name) && costRelevant(r.Type) {
			o.cond = true
		}
	}
	for _, r := range live {
		if exempt(exemptions, r.Name) {
			continue
		}
		switch {
		case strings.EqualFold(r.Type, typeDeployment):
			switch strings.ToLower(strings.TrimSpace(r.SKUName)) {
			case "globalprovisionedmanaged", "datazoneprovisionedmanaged", "provisionedmanaged":
				o.addKey(r, "dev:provisioned-throughput", "development environment uses provisioned throughput model capacity")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
		case strings.EqualFold(r.Type, typeSearch):
			switch strings.ToLower(r.SKUName) {
			case "standard2", "standard3", "storage_optimized_l1", "storage_optimized_l2":
				o.addKey(r, "dev:search-tier", "development environment uses a production-sized Search tier")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
			if v, ok := intAt(r.Properties, "replicaCount"); ok {
				if v > 1 {
					o.addKey(r, "dev:search-replicas", "development environment uses more than one Search replica")
				}
			} else if propUnresolved(r.Properties, "replicaCount") {
				o.unresolved = true
			}
			if v, ok := intAt(r.Properties, "partitionCount"); ok {
				if v > 1 {
					o.addKey(r, "dev:search-partitions", "development environment uses more than one Search partition")
				}
			} else if propUnresolved(r.Properties, "partitionCount") {
				o.unresolved = true
			}
		case strings.EqualFold(r.Type, typeAPIM):
			switch strings.ToLower(strings.TrimSpace(r.SKUName)) {
			case "premium", "isolated":
				o.addKey(r, "dev:apim-tier", "development environment uses a production-sized API Management tier")
			case "":
				if skuUnresolved(r) {
					o.unresolved = true
				}
			}
			if v, ok := skuCapacity(r); ok {
				if v > 1 {
					o.addKey(r, "dev:apim-units", "development environment uses more than one API Management unit")
				}
			} else if r.SKU != nil && propUnresolved(r.SKU, "capacity") {
				o.unresolved = true
			}
		case cosmosThroughputType(r.Type):
			// The policy limit is applied exactly as configured.
			if throughput, ok := cosmosThroughput(r); ok {
				if hasLimit && throughput > float64(limit) {
					o.addKey(r, "dev:cosmos-throughput", "development environment Cosmos DB throughput exceeds the configured policy limit")
				}
			} else if propThroughputUnresolved(r) {
				o.unresolved = true
			}
			if autoscale, ok := cosmosAutoscale(r); ok {
				if hasLimit && autoscale > limit {
					o.addKey(r, "dev:cosmos-autoscale", "development environment Cosmos DB autoscale maxThroughput exceeds the configured policy limit")
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

// costRelevant reports whether eval002 inspects resources of the given type.
func costRelevant(typ string) bool {
	return strings.EqualFold(typ, typeDeployment) || strings.EqualFold(typ, typeSearch) ||
		strings.EqualFold(typ, typeAPIM) || cosmosThroughputType(typ)
}

const (
	cosmosSQLDatabase  = "microsoft.documentdb/databaseaccounts/sqldatabases"
	cosmosSQLContainer = "microsoft.documentdb/databaseaccounts/sqldatabases/containers"
)

// cosmosThroughputType matches throughputSettings resources and the Cosmos DB
// SQL databases and containers, whose properties.options.throughput and
// properties.options.autoscaleSettings.maxThroughput set throughput at creation
// (CreateUpdateOptions in the Microsoft.DocumentDB 2025-04-15 specification).
func cosmosThroughputType(typ string) bool {
	t := strings.ToLower(typ)
	return strings.HasSuffix(t, "/throughputsettings") || t == cosmosSQLDatabase || t == cosmosSQLContainer
}

func eval003(in *sdk.Input) sdk.Result {
	snap := runtimegw.BuildSnapshot(in)
	o := &outcome{}
	seen := false
	for _, svc := range snap.Services {
		for _, api := range svc.APIs {
			if !svc.AIAPIBacked(api) {
				continue
			}
			seen = true
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok {
				o.skip = "api-not-fronting-a-model-endpoint"
				continue
			}
			if pol.External {
				o.skip = "policy-uses-external-link"
				continue
			}
			limits := runtimegw.FindElements(pol.Inbound, "llm-token-limit")
			if len(limits) == 0 {
				o.findings = append(o.findings, sdk.Finding{
					Resource: sdk.ResourceRef{Type: "Microsoft.ApiManagement/service/apis", Name: api.Name},
					Location: api.SourceLocation,
					Key:      "llm-token-limit-missing",
					Evidence: "AI gateway API is missing llm-token-limit in inbound policy",
				})
				continue
			}
			for li, el := range limits {
				if !tokenLimitValid(el) {
					o.findings = append(o.findings, sdk.Finding{
						Key:      fmt.Sprintf("llm-token-limit-invalid:%d", li),
						Resource: sdk.ResourceRef{Type: "Microsoft.ApiManagement/service/apis", Name: api.Name},
						Location: api.SourceLocation,
						Evidence: "llm-token-limit is present but missing required attributes or quota settings",
					})
				}
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "api-not-fronting-a-model-endpoint"}}
	}
	return o.result()
}

func eval004(in *sdk.Input) sdk.Result {
	if in == nil || in.ARM == nil {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	o := &outcome{}
	supported := 0
	live, unc := liveResources(in.ARM)
	for _, r := range unc {
		if costRelevant(r.Type) {
			o.cond = true
		}
	}
	for _, r := range live {
		switch {
		case strings.EqualFold(r.Type, typeSearch):
			if strings.EqualFold(r.SKUName, "free") || strings.EqualFold(r.SKUName, "serverless") {
				continue
			}
			supported++
			if _, ok := intAt(r.Properties, "replicaCount"); !ok {
				o.addKey(r, "incomplete:search-replicas", "fixed-capacity estimate is incomplete because Search replicaCount is unresolved")
			}
			if _, ok := intAt(r.Properties, "partitionCount"); !ok {
				o.addKey(r, "incomplete:search-partitions", "fixed-capacity estimate is incomplete because Search partitionCount is unresolved")
			}
			if _, ok := literalRegion(r.Region); !ok {
				o.addKey(r, "incomplete:search-region", "fixed-capacity estimate is incomplete because Search region is unresolved")
			}
		case strings.EqualFold(r.Type, typeAPIM):
			if strings.EqualFold(r.SKUName, "Consumption") {
				continue
			}
			supported++
			if _, ok := skuCapacity(r); !ok {
				o.addKey(r, "incomplete:apim-capacity", "fixed-capacity estimate is incomplete because API Management sku.capacity is unresolved")
			}
			if _, ok := literalRegion(r.Region); !ok {
				o.addKey(r, "incomplete:apim-region", "fixed-capacity estimate is incomplete because API Management region is unresolved")
			}
		case strings.HasSuffix(strings.ToLower(r.Type), "/throughputsettings"):
			supported++
			if _, ok := cosmosThroughput(r); ok {
				continue
			}
			if max, ok := cosmosAutoscale(r); ok {
				o.addKey(r, "incomplete:cosmos-autoscale-meter", "fixed-capacity estimate is incomplete because Cosmos autoscale retail meter mapping is UNVERIFIED (maxThroughput="+strconv.Itoa(max)+")")
				continue
			}
			o.addKey(r, "incomplete:cosmos-throughput", "fixed-capacity estimate is incomplete because Cosmos throughput is unresolved")
		case strings.EqualFold(r.Type, typeDeployment):
			switch strings.ToLower(strings.TrimSpace(r.SKUName)) {
			case "globalprovisionedmanaged", "datazoneprovisionedmanaged", "provisionedmanaged":
				supported++
				o.addKey(r, "incomplete:ptu-meter", "fixed-capacity estimate is incomplete because PTU retail meter mapping remains UNVERIFIED for generic deployment SKUs")
			}
		}
	}
	if supported == 0 {
		if o.cond {
			return o.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-supported-fixed-capacity-resources"}}
	}
	if len(o.findings) == 0 {
		return sdk.Result{}
	}
	return o.result()
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

// contactStatus reports whether a notification has a literal contact and whether
// any contact list is an unresolved ARM expression (a string starting with "["
// that is not "[[", or a list that holds such a string). An expression-valued
// list is unresolved, never empty.
func contactStatus(m map[string]any) (has, unresolved bool) {
	for _, k := range []string{"contactEmails", "contactRoles", "contactGroups"} {
		switch x := m[k].(type) {
		case string:
			if isExpr(x) {
				unresolved = true
			} else if strings.TrimSpace(x) != "" {
				has = true
			}
		default:
			for _, s := range stringsFrom(x) {
				if isExpr(s) {
					unresolved = true
				} else {
					has = true
				}
			}
		}
	}
	return has, unresolved && !has
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

// throughputBag returns the object that holds throughput: properties.resource
// for throughputSettings, properties.options for SQL databases and containers.
func throughputBag(r sdk.ARMResource) map[string]any {
	t := strings.ToLower(r.Type)
	if t == cosmosSQLDatabase || t == cosmosSQLContainer {
		return mapAt(r.Properties, "options")
	}
	return mapAt(r.Properties, "resource")
}

func cosmosThroughput(r sdk.ARMResource) (float64, bool) {
	resource := throughputBag(r)
	if resource == nil {
		return 0, false
	}
	return number(resource["throughput"])
}

func cosmosAutoscale(r sdk.ARMResource) (int, bool) {
	resource := throughputBag(r)
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
	resource := throughputBag(r)
	return propUnresolved(resource, "throughput")
}

func propAutoscaleUnresolved(r sdk.ARMResource) bool {
	resource := throughputBag(r)
	if resource == nil {
		return false
	}
	auto := mapAt(resource, "autoscaleSettings")
	return propUnresolved(auto, "maxThroughput")
}

func tokenLimitValid(el runtimegw.PolicyElement) bool {
	if strings.TrimSpace(el.Attrs["counter-key"]) == "" || strings.TrimSpace(el.Attrs["estimate-prompt-tokens"]) == "" {
		return false
	}
	if strings.TrimSpace(el.Attrs["tokens-per-minute"]) != "" {
		return true
	}
	if strings.TrimSpace(el.Attrs["token-quota"]) == "" {
		return false
	}
	switch strings.TrimSpace(el.Attrs["token-quota-period"]) {
	case "Hourly", "Daily", "Weekly", "Monthly", "Yearly":
		return true
	default:
		return false
	}
}

func literalRegion(region string) (string, bool) {
	region = strings.TrimSpace(region)
	if region == "" || strings.HasPrefix(region, "[") {
		return "", false
	}
	return strings.ToLower(region), true
}
