package rel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	runtimegw "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/gateway"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeSearch     = "Microsoft.Search/searchServices"
	typeStorage    = "Microsoft.Storage/storageAccounts"
	typeCosmos     = "Microsoft.DocumentDB/databaseAccounts"
	typeFoundry    = "Microsoft.CognitiveServices/accounts"
	typeLock       = "Microsoft.Authorization/locks"
	typeAPIM       = "Microsoft.ApiManagement/service"
	typeDeployment = "Microsoft.CognitiveServices/accounts/deployments"
)

func Register() []sdk.Rule {
	return []sdk.Rule{
		rule{id: "FND-REL-001", fn: eval001},
		rule{id: "FND-REL-002", fn: eval002},
		rule{id: "FND-REL-003", fn: eval003},
		rule{id: "FND-REL-004", fn: eval004},
		rule{id: "FND-REL-005", fn: eval005},
		rule{id: "FND-REL-006", fn: eval006},
		rule{id: "FND-REL-008", fn: eval008},
		rule{id: "FND-REL-009", fn: eval009},
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
	findings []sdk.Finding
	skips    []string
	notes    []string
	v        *view
}

// add records a finding for r. key is a stable per-check discriminator: a rule
// can report several checks on one resource and the fingerprint must tell them
// apart without the runner deriving a key from evidence.
func (o *outcome) add(r sdk.ARMResource, key, evidence string) {
	o.addRes(r.Type, r.Name, r.Location, key, evidence)
}

func (o *outcome) addRes(typ, name string, loc sdk.Location, key, evidence string) {
	o.findings = append(o.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: typ, Name: armexpr.StripScope(name)},
		Location: loc,
		Key:      key,
		Evidence: armexpr.StripScope(evidence),
	})
}

func (o *outcome) result() sdk.Result {
	if len(o.findings) > 0 {
		slices.SortFunc(o.findings, func(a, b sdk.Finding) int {
			if c := strings.Compare(a.Resource.Type, b.Resource.Type); c != 0 {
				return c
			}
			if c := strings.Compare(a.Resource.Name, b.Resource.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Evidence, b.Evidence)
		})
		// Uncertain resources are returned as Skips next to the findings
		// (review M3, ADR-008 amendment 3). Not-applicable skips are not.
		var skips []sdk.Skip
		if len(o.notes) > 0 {
			slices.Sort(o.notes)
			for _, n := range unique(o.notes) {
				skips = append(skips, sdk.Skip{Reason: armexpr.StripScope("uncertain: " + n)})
			}
		}
		return sdk.Result{Findings: o.findings, Skips: skips}
	}
	if len(o.skips) > 0 {
		slices.Sort(o.skips)
		return sdk.Result{Skipped: &sdk.Skip{Reason: armexpr.StripScope(strings.Join(unique(o.skips), "; "))}}
	}
	if len(o.notes) > 0 {
		slices.Sort(o.notes)
		return sdk.Result{Skipped: &sdk.Skip{Reason: armexpr.StripScope("uncertain: " + strings.Join(unique(o.notes), "; "))}}
	}
	return sdk.Result{}
}

func unique(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	default:
		return 0, false
	}
}

func intProp(m map[string]any, key string, def int) (int, bool) {
	if m == nil {
		return def, false
	}
	n, ok := number(m[key])
	if !ok {
		return def, false
	}
	return int(n), true
}

func mapProp(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	out, _ := m[key].(map[string]any)
	return out
}

func unresolved(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || isExpr(v)
}

func eval001(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	for _, r := range o.of(typeSearch) {
		if strings.EqualFold(r.SKUName, "free") {
			o.skips = append(o.skips, r.Name+": free-tier-search-sla-not-applicable")
			continue
		}
		replicas, ok := intProp(r.Properties, "replicaCount", 1)
		if !ok && unresolved(fmt.Sprint(r.Properties["replicaCount"])) {
			o.notes = append(o.notes, r.Name+": replicaCount is unresolved")
			continue
		}
		if replicas < 2 {
			o.add(r, "billable-search-service-fewer-two-replicas", "billable Search service has fewer than two replicas and does not meet the documented query SLA posture")
		}
	}
	return o.result()
}

var redundantStorage = map[string]bool{
	"standard_zrs": true, "premium_zrs": true,
	"standard_grs": true, "standard_ragrs": true,
	"standard_gzrs": true, "standard_ragzrs": true,
	"standardv2_zrs": true, "standardv2_grs": true, "standardv2_gzrs": true,
	"premiumv2_zrs": true, "premiumv2_grs": true, "premiumv2_gzrs": true,
}

func eval002(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	for _, r := range o.of(typeStorage) {
		sku := strings.ToLower(strings.TrimSpace(r.SKUName))
		if sku == "" {
			o.notes = append(o.notes, r.Name+": storage sku.name is unresolved")
			continue
		}
		if redundantStorage[sku] {
			continue
		}
		if strings.HasSuffix(sku, "_lrs") {
			o.add(r, "storage-account-uses-single-zone-local", "storage account uses single-zone local redundancy")
			continue
		}
		o.notes = append(o.notes, r.Name+": storage sku.name is not in the verified redundancy mapping")
	}
	return o.result()
}

func eval003(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	for _, r := range o.of(typeCosmos) {
		locations, ok := r.Properties["locations"].([]any)
		if !ok || len(locations) == 0 {
			o.notes = append(o.notes, r.Name+": Cosmos locations are unavailable")
		} else {
			for _, raw := range locations {
				m, _ := raw.(map[string]any)
				zr, ok := m["isZoneRedundant"].(bool)
				if !ok {
					o.notes = append(o.notes, r.Name+": Cosmos location zone redundancy is unresolved")
					continue
				}
				if !zr {
					o.add(r, "cosmos-account-location-zone-redundant", "Cosmos account location is not zone redundant")
				}
			}
		}
		bp := mapProp(r.Properties, "backupPolicy")
		switch strings.TrimSpace(fmt.Sprint(bp["type"])) {
		case "Continuous":
		case "":
			o.notes = append(o.notes, r.Name+": Cosmos backupPolicy.type is unresolved")
		default:
			o.add(r, "cosmos-account-configured-continuous-backup", "Cosmos account is not configured for continuous backup")
		}
	}
	return o.result()
}

// lockCover classifies how one lock relates to the deployment resource group and
// to a target resource.
type lockCover int

const (
	coverNone lockCover = iota
	coverYes
	coverUnknown
)

// splitCall splits "fn(a, b(c), 'd,e')" into the function name and its top-level
// arguments. ok is false when s is not a single function call.
func splitCall(s string) (fn string, args []string, ok bool) {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return "", nil, false
	}
	fn = strings.TrimSpace(s[:open])
	inner := s[open+1 : len(s)-1]
	depth, start, inQuote := 0, 0, false
	for i := 0; i < len(inner); i++ {
		switch c := inner[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
			if depth < 0 {
				return "", nil, false
			}
		case c == ',' && depth == 0:
			args = append(args, strings.TrimSpace(inner[start:i]))
			start = i + 1
		}
	}
	if depth != 0 || inQuote {
		return "", nil, false
	}
	if last := strings.TrimSpace(inner[start:]); last != "" || len(args) > 0 {
		args = append(args, last)
	}
	return fn, args, true
}

func quoted(arg string) (string, bool) {
	a := strings.TrimSpace(arg)
	if len(a) >= 2 && a[0] == '\'' && a[len(a)-1] == '\'' && !strings.Contains(a[1:len(a)-1], "'") {
		return a[1 : len(a)-1], true
	}
	return "", false
}

// lockCoversGroup reports whether a lock's scope is the deployment resource
// group. The input carries no deployment resource-group name, so only a
// scope-less lock (it applies to the deployment scope) and resourceGroup().id
// are provably that group; any other resource-group scope is unknown.
func lockCoversGroup(scope string) lockCover {
	s := strings.TrimSpace(scope)
	switch {
	case s == "":
		return coverYes
	case isExpr(s):
		n := normExpr(s)
		switch {
		case n == "resourcegroup().id":
			return coverYes
		case strings.HasPrefix(n, "resourceid(") || strings.HasPrefix(n, "extensionresourceid(") || strings.HasPrefix(n, "subscriptionresourceid("):
			// A resourceId-style call names a resource, never the group.
			return coverNone
		}
		// A parameter, variable, format() or any other expression has a value
		// the template cannot show: it may well be the resource group.
		return coverUnknown
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "/resourcegroups/") && !strings.Contains(low, "/providers/") {
		return coverUnknown
	}
	return coverNone
}

// lockCoversResource reports whether the lock scope names resource r. Expression
// names are compared by normalised text of the last resourceId argument; a
// comparison that cannot be proven is coverUnknown.
func lockCoversResource(scope string, r sdk.ARMResource) lockCover {
	s := strings.TrimSpace(scope)
	if s == "" {
		return coverNone
	}
	if !isExpr(s) {
		if isExpr(r.Name) {
			return coverUnknown
		}
		want := strings.ToLower("/providers/" + r.Type + "/" + lastSeg(r.Name))
		if strings.HasSuffix(strings.ToLower(s), want) {
			return coverYes
		}
		return coverNone
	}
	body := strings.TrimSpace(s)
	body = strings.TrimSpace(body[1 : len(body)-1])
	fn, args, ok := splitCall(body)
	if !ok || !strings.EqualFold(fn, "resourceid") {
		return coverUnknown
	}
	for i, a := range args {
		q, ok := quoted(a)
		if !ok || !strings.Contains(q, "/") || !strings.Contains(strings.SplitN(q, "/", 2)[0], ".") {
			continue
		}
		if !strings.EqualFold(q, r.Type) {
			return coverNone
		}
		names := args[i+1:]
		if len(names) != 1 {
			return coverUnknown
		}
		nq, nameLit := quoted(names[0])
		switch {
		case nameLit && !isExpr(r.Name):
			if strings.EqualFold(nq, lastSeg(r.Name)) {
				return coverYes
			}
			return coverNone
		case !nameLit && isExpr(r.Name) && normExpr(names[0]) == normExpr(r.Name):
			return coverYes
		}
		return coverUnknown
	}
	return coverUnknown
}

func eval004(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	targets := []sdk.ARMResource{}
	targets = append(targets, o.of(typeFoundry)...)
	targets = append(targets, o.of(typeCosmos)...)
	targets = append(targets, o.of(typeSearch)...)
	targets = append(targets, o.of(typeStorage)...)
	if len(targets) == 0 {
		return o.result()
	}
	locks := o.of(typeLock)
	if len(locks) == 0 {
		if o.hasUncertain(typeLock) {
			return o.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: no lock resources are declared in the template and locks can be applied later or at parent scope"}}
	}
	groupCovered, unknown := false, false
	covered := map[string]bool{}
	for _, lock := range locks {
		level := ""
		if s, ok := mapProp(lock.Properties, "properties")["level"].(string); ok {
			level = strings.TrimSpace(s)
		} else if s, ok := lock.Properties["level"].(string); ok {
			level = strings.TrimSpace(s)
		}
		if isExpr(level) {
			unknown = true
			continue
		}
		if !strings.EqualFold(level, "CanNotDelete") && !strings.EqualFold(level, "ReadOnly") {
			continue
		}
		switch lockCoversGroup(lock.Scope) {
		case coverYes:
			groupCovered = true
		case coverUnknown:
			unknown = true
		}
		for _, r := range targets {
			switch lockCoversResource(lock.Scope, r) {
			case coverYes:
				covered[strings.ToLower(r.Type+"|"+r.Name)] = true
			case coverUnknown:
				unknown = true
			}
		}
	}
	if groupCovered {
		return sdk.Result{}
	}
	var missing []string
	for _, r := range targets {
		if !covered[strings.ToLower(r.Type+"|"+r.Name)] {
			missing = append(missing, r.Name)
		}
	}
	if len(missing) > 0 {
		reason := "uncertain: delete locks may exist outside the template for "
		if unknown {
			reason = "uncertain: a lock scope could not be correlated with the deployment resource group or the resource for "
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: reason + strings.Join(missing, ", ")}}
	}
	if len(o.notes) > 0 {
		return o.result()
	}
	return sdk.Result{}
}

func eval005(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	for _, r := range o.of(typeSearch) {
		sku := strings.ToLower(strings.TrimSpace(r.SKUName))
		if sku == "free" || sku == "serverless" {
			o.skips = append(o.skips, r.Name+": search tier has no documented sizing constraints for this rule")
			continue
		}
		replicas, rok := intProp(r.Properties, "replicaCount", 1)
		partitions, pok := intProp(r.Properties, "partitionCount", 1)
		hosting := strings.TrimSpace(fmt.Sprint(r.Properties["hostingMode"]))
		if !rok && unresolved(fmt.Sprint(r.Properties["replicaCount"])) {
			o.notes = append(o.notes, r.Name+": replicaCount is unresolved")
			continue
		}
		if !pok && unresolved(fmt.Sprint(r.Properties["partitionCount"])) {
			o.notes = append(o.notes, r.Name+": partitionCount is unresolved")
			continue
		}
		switch partitions {
		case 1, 2, 3, 4, 6, 12:
		default:
			o.add(r, "search-partitioncount-outside-documented-allowed-set", "Search partitionCount is outside the documented allowed set")
		}
		if replicas > 12 {
			o.add(r, "search-replicacount-exceeds-documented-maximum-12", "Search replicaCount exceeds the documented maximum of 12")
		}
		if sku == "basic" && replicas > 3 {
			o.add(r, "basic-search-tier-replicacount-exceeds-documented", "Basic Search tier replicaCount exceeds the documented maximum of 3")
		}
		if sku != "basic" && replicas*partitions > 36 {
			o.add(r, "search-replicas-multiplied-partitions-exceeds-documented", "Search replicas multiplied by partitions exceeds the documented 36 search-unit maximum")
		}
		if strings.EqualFold(hosting, "HighDensity") {
			if sku != "standard3" {
				o.add(r, "highdensity-hosting-mode-documented-only-standard3", "HighDensity hosting mode is documented only for the standard3 tier")
			}
			if partitions > 3 {
				o.add(r, "highdensity-hosting-mode-supports-most-three", "HighDensity hosting mode supports at most three partitions")
			}
		}
		if sku == "basic" && partitions > 1 {
			o.notes = append(o.notes, r.Name+": Basic Search partitionCount above one depends on creation date and region")
		}
	}
	return o.result()
}

func eval006(in *sdk.Input) sdk.Result {
	snap := runtimegw.BuildSnapshot(in)
	o := newOutcome(in)
	seen := false
	raw := map[string]sdk.ARMResource{}
	for _, r := range in.ARM.Resources() {
		if strings.EqualFold(r.Type, "Microsoft.ApiManagement/service/backends") {
			raw[strings.ToLower(r.Name)] = r
		}
	}
	for _, svc := range snap.Services {
		hasRetry := false
		for _, api := range svc.APIs {
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok {
				continue
			}
			if pol.External {
				o.notes = append(o.notes, svc.Name+": policy fragments are external and retry evidence is unresolved")
				continue
			}
			if len(runtimegw.FindElements(pol.Inbound, "retry")) > 0 {
				hasRetry = true
			}
		}
		for _, b := range svc.Backends {
			if !runtimegw.APIMHostLikely(runtimegw.HostFromURL(b.URL)) {
				continue
			}
			seen = true
			if strings.EqualFold(svc.SKU, "Consumption") {
				o.skips = append(o.skips, svc.Name+": circuit breaker is unsupported on the Consumption tier")
				continue
			}
			r, ok := raw[strings.ToLower(svc.Name+"/"+b.Name)]
			if !ok {
				o.notes = append(o.notes, svc.Name+"/"+b.Name+": backend resource shape is unavailable")
				continue
			}
			if !strings.EqualFold(r.APIVersion, "2025-09-01-preview") {
				o.skips = append(o.skips, svc.Name+"/"+b.Name+": unsupported API version for circuit breaker evidence")
				continue
			}
			cb := mapProp(r.Properties, "circuitBreaker")
			rules, _ := cb["rules"].([]any)
			if len(rules) == 0 && !hasRetry {
				o.addRes("Microsoft.ApiManagement/service/backends", b.Name, b.SourceLocation, "backend-no-circuit-breaker-retry", "Foundry-targeting backend has neither a circuit breaker rule nor retry policy evidence")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "backend-does-not-target-foundry-or-azure-openai"}}
	}
	return o.result()
}

func eval008(in *sdk.Input) sdk.Result {
	o := newOutcome(in)
	deploys := o.of(typeDeployment)
	index := map[string]sdk.ARMResource{}
	for _, r := range deploys {
		index[strings.ToLower(r.Name)] = r
	}
	for _, r := range deploys {
		switch strings.TrimSpace(r.SKUName) {
		case "ProvisionedManaged", "GlobalProvisionedManaged", "DataZoneProvisionedManaged":
		default:
			continue
		}
		spill := strings.TrimSpace(fmt.Sprint(r.Properties["spilloverDeploymentName"]))
		if spill == "" {
			o.add(r, "provisioned-deployment-spilloverdeploymentname-request-level-spillover", "provisioned deployment has no spilloverDeploymentName; request-level spillover may exist but is not visible offline")
			continue
		}
		if unresolved(spill) {
			o.notes = append(o.notes, r.Name+": spilloverDeploymentName is unresolved")
			continue
		}
		sibling, ok := index[strings.ToLower(parentName(r.Name)+"/"+spill)]
		if !ok {
			sibling, ok = index[strings.ToLower(spill)]
		}
		if !ok {
			if o.hasUncertain(typeDeployment) {
				o.notes = append(o.notes, r.Name+": spilloverDeploymentName may refer to a conditional or looped deployment")
				continue
			}
			o.add(r, "spilloverdeploymentname-resolve-deployment-same-account", "spilloverDeploymentName does not resolve to a deployment in the same account")
			continue
		}
		if strings.Contains(strings.ToLower(sibling.SKUName), "provisioned") {
			o.add(r, "spilloverdeploymentname-must-refer-standard-deployment-another", "spilloverDeploymentName must refer to a standard deployment, not another provisioned deployment")
		}
		if !sameModel(r, sibling) {
			if modelHasExpr(r) || modelHasExpr(sibling) {
				o.notes = append(o.notes, r.Name+": model identity contains an unresolved expression")
				continue
			}
			o.add(r, "spilloverdeploymentname-target-same-model-version", "spilloverDeploymentName does not target the same model and version")
		}
	}
	return o.result()
}

func eval009(in *sdk.Input) sdk.Result {
	slaRequired := policyBool(in, "reliability.apimSlaRequired")
	zoneRequired := policyBool(in, "reliability.apimZoneRequired")
	regionalRequired := policyBool(in, "reliability.apimRegionalRequired")
	if !slaRequired && !zoneRequired && !regionalRequired {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-resilience-objective-declared"}}
	}
	o := newOutcome(in)
	for _, r := range o.of(typeAPIM) {
		sku := strings.TrimSpace(r.SKUName)
		if sku == "" {
			o.notes = append(o.notes, r.Name+": APIM sku.name is unresolved")
			continue
		}
		if slaRequired && strings.EqualFold(sku, "Developer") {
			o.add(r, "developer-tier-sla-satisfy-declared-api", "Developer tier has no SLA and does not satisfy the declared API gateway objective")
		}
		if zoneRequired {
			if strings.EqualFold(sku, "StandardV2") {
				o.notes = append(o.notes, r.Name+": StandardV2 zone support is documented inconsistently")
			} else if !(strings.EqualFold(sku, "Premium") || strings.EqualFold(sku, "PremiumV2")) {
				o.add(r, "apim-tier-satisfy-declared-zone-resilience", "APIM tier does not satisfy the declared zone-resilience objective")
			} else if len(r.Zones) == 0 {
				o.add(r, "apim-zone-resilience-objective-declared-zones", "APIM zone-resilience objective is declared but no zones are configured")
			}
		}
		if regionalRequired {
			additional, _ := r.Properties["additionalLocations"].([]any)
			if !strings.EqualFold(sku, "Premium") || len(additional) == 0 {
				o.add(r, "apim-regional-resilience-objective-declared-documented", "APIM regional-resilience objective is declared but the documented Premium multi-region shape is absent")
			}
		}
	}
	return o.result()
}

func parentName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[:i]
	}
	return ""
}

func lastSeg(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func modelHasExpr(r sdk.ARMResource) bool {
	m, _ := r.Properties["model"].(map[string]any)
	for _, k := range []string{"format", "name", "version"} {
		if s, ok := m[k].(string); ok && isExpr(s) {
			return true
		}
	}
	return false
}

func sameModel(a, b sdk.ARMResource) bool {
	am, _ := a.Properties["model"].(map[string]any)
	bm, _ := b.Properties["model"].(map[string]any)
	return strings.EqualFold(fmt.Sprint(am["format"]), fmt.Sprint(bm["format"])) &&
		strings.EqualFold(fmt.Sprint(am["name"]), fmt.Sprint(bm["name"])) &&
		strings.EqualFold(fmt.Sprint(am["version"]), fmt.Sprint(bm["version"]))
}

func policyBool(in *sdk.Input, key string) bool {
	if in == nil || in.Policy == nil {
		return false
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
