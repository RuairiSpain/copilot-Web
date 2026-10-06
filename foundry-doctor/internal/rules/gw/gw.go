// Package gw implements the Phase 8 API Management AI gateway checks.
package gw

import (
	"context"
	"fmt"
	"slices"
	"strings"

	runtimegw "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/gateway"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func Register() []sdk.Rule {
	return []sdk.Rule{
		rule{id: "FND-GW-001", fn: eval001},
		rule{id: "FND-GW-002", fn: eval002},
		rule{id: "FND-GW-003", fn: eval003},
		rule{id: "FND-GW-004", fn: eval004},
		rule{id: "FND-GW-005", fn: eval005},
		rule{id: "FND-GW-006", fn: eval006},
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

type acc struct {
	findings []sdk.Finding
	skip     []string
	note     []string
}

func (a *acc) add(loc sdk.Location, typ, name, evidence string) {
	a.findings = append(a.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: typ, Name: name},
		Location: loc,
		Evidence: evidence,
	})
}
func (a *acc) result() sdk.Result {
	if len(a.findings) > 0 {
		slices.SortFunc(a.findings, func(x, y sdk.Finding) int {
			if c := strings.Compare(x.Resource.Name, y.Resource.Name); c != 0 {
				return c
			}
			return strings.Compare(x.Evidence, y.Evidence)
		})
		return sdk.Result{Findings: a.findings}
	}
	if len(a.skip) > 0 {
		slices.Sort(a.skip)
		return sdk.Result{Skipped: &sdk.Skip{Reason: strings.Join(unique(a.skip), "; ")}}
	}
	if len(a.note) > 0 {
		slices.Sort(a.note)
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: " + strings.Join(unique(a.note), "; ")}}
	}
	return sdk.Result{}
}

func snapshot(in *sdk.Input) runtimegw.Snapshot { return runtimegw.BuildSnapshot(in) }

func eval001(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range snap.Services {
		for _, api := range svc.APIs {
			if !svc.AIAPIBacked(api) {
				continue
			}
			seen = true
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok || pol.External {
				a.note = append(a.note, "effective policy could not be resolved for "+svc.Name+"/"+api.Name)
				continue
			}
			var okToken bool
			for _, el := range pol.Inbound {
				switch el.Name {
				case "validate-jwt":
					if validJWT(el) {
						okToken = true
					} else {
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "API gateway policy does not constrain validate-jwt with audience, issuer source and tenant")
					}
				case "validate-azure-ad-token":
					if validAADToken(el) {
						okToken = true
					} else {
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "API gateway policy does not constrain validate-azure-ad-token with a specific tenant and audience or application ID")
					}
				}
			}
			if !okToken {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "AI gateway API has no validate-jwt or validate-azure-ad-token policy")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-ai-gateway-apis"}}
	}
	return a.result()
}

func eval002(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range snap.Services {
		for _, api := range svc.APIs {
			if !svc.AIAPIBacked(api) {
				continue
			}
			seen = true
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok || pol.External {
				a.note = append(a.note, "effective policy could not be resolved for "+svc.Name+"/"+api.Name)
				continue
			}
			limits := runtimegw.FindElements(pol.Inbound, "llm-token-limit")
			metrics := runtimegw.FindElements(pol.Inbound, "llm-emit-token-metric")
			if len(limits) == 0 {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "AI gateway API is missing llm-token-limit in inbound policy")
			}
			if len(metrics) == 0 {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "AI gateway API is missing llm-emit-token-metric in inbound policy")
			}
			for _, el := range limits {
				if !validTokenLimit(el) {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "llm-token-limit is present but missing required attributes or quota settings")
				}
				if strings.EqualFold(svc.SKU, "Consumption") {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "llm-token-limit is not supported on the Consumption tier")
				}
			}
			for _, el := range metrics {
				if !validTokenMetric(el) {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "llm-emit-token-metric has more than five dimensions or a dimension without a name")
				}
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "api-not-fronting-a-model-endpoint"}}
	}
	return a.result()
}

func eval003(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range snap.Services {
		for _, b := range svc.Backends {
			for _, member := range b.PoolMemberIDs {
				if _, ok := svc.Backends[strings.ToLower(lastSegment(member))]; !ok {
					a.add(b.SourceLocation, "Microsoft.ApiManagement/service/backends", b.Name, "backend pool member does not resolve to an existing backend resource")
				}
			}
		}
		for _, api := range svc.APIs {
			seen = true
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok || pol.External {
				a.note = append(a.note, "effective policy could not be resolved for "+svc.Name+"/"+api.Name)
				continue
			}
			backends := runtimegw.FindElements(pol.Inbound, "set-backend-service")
			if len(backends) == 0 {
				if strings.TrimSpace(api.ServiceURL) == "" {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "API has neither serviceUrl nor set-backend-service policy")
				}
				continue
			}
			for _, el := range backends {
				id := strings.TrimSpace(el.Attrs["backend-id"])
				switch {
				case id == "":
					if strings.TrimSpace(el.Attrs["base-url"]) == "" {
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "set-backend-service must specify backend-id or base-url")
					}
				case runtimegw.HasExpression(id):
					a.note = append(a.note, "backend-id uses a policy expression for "+svc.Name+"/"+api.Name)
				case svc.Backends[strings.ToLower(id)].Name == "":
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "set-backend-service backend-id does not match a backend resource")
				}
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-apis"}}
	}
	return a.result()
}

func eval004(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	for _, svc := range snap.Services {
		groups := map[string]string{}
		for _, api := range svc.APIs {
			path := normPath(api.Path)
			group := strings.ToLower(api.BaseName)
			if api.VersionSetID != "" {
				group = strings.ToLower(api.VersionSetID)
			}
			if prev, ok := groups[path]; ok && prev != group {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "API path duplicates another API path in the same service")
				continue
			}
			groups[path] = group
		}
	}
	if len(a.findings) == 0 {
		return sdk.Result{}
	}
	return a.result()
}

func eval005(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range snap.Services {
		for _, api := range svc.APIs {
			if !svc.AIAPIBacked(api) {
				continue
			}
			seen = true
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok || pol.External {
				a.note = append(a.note, "effective policy could not be resolved for "+svc.Name+"/"+api.Name)
				continue
			}
			idents := runtimegw.FindElements(pol.Inbound, "authentication-managed-identity")
			var mi bool
			targets := foundryTargets(snap, svc, api)
			for _, el := range idents {
				res := strings.TrimSuffix(strings.TrimSpace(el.Attrs["resource"]), "/")
				if res != "https://cognitiveservices.azure.com" {
					continue
				}
				clientID := strings.TrimSpace(el.Attrs["client-id"])
				if clientID != "" {
					if !hasUserAssignedID(svc.IdentityType) || !clientIDMatchesServiceIdentity(snap, svc, clientID) {
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "authentication-managed-identity client-id does not match a user-assigned identity on the APIM service")
					}
					switch cognitiveRoleCoverage(snap, principalForClientID(snap, svc, clientID), targets) {
					case roleMissing:
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "user-assigned identity used by authentication-managed-identity lacks a Cognitive Services role on the Foundry resource")
					case roleUncertain:
						a.note = append(a.note, "user-assigned identity role coverage could not be proven from template-scoped RBAC metadata alone")
					}
				} else if !hasSystemAssignedID(svc.IdentityType) {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "authentication-managed-identity requires a system-assigned identity when client-id is omitted")
				} else {
					switch cognitiveRoleCoverage(snap, svc.PrincipalID, targets) {
					case roleMissing:
						a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "API Management system-assigned identity lacks a Cognitive Services role on the Foundry resource")
					case roleUncertain:
						a.note = append(a.note, "system-assigned identity role coverage could not be proven from template-scoped RBAC metadata alone")
					}
				}
				mi = true
			}
			for _, backend := range backendsForAPI(svc, pol) {
				if hasAPIKeyHeader(backend.HeaderNames) {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/backends", backend.Name, "backend credentials supply an api-key header instead of using managed identity")
				}
				if backend.AuthorizationCredentialsPresent {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/backends", backend.Name, "backend authorization credentials are header-based and do not prove managed identity authentication")
				}
			}
			if !mi {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "AI gateway API does not authenticate to Foundry with authentication-managed-identity")
			}
			for _, el := range runtimegw.FindElements(pol.Inbound, "set-header") {
				if strings.EqualFold(strings.TrimSpace(el.Attrs["name"]), "api-key") {
					a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "AI gateway policy sets an api-key header instead of using managed identity")
				}
			}
			if strings.TrimSpace(svc.IdentityType) == "" {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service", svc.Name, "API Management service has no managed identity")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "non-foundry-backend-host"}}
	}
	return a.result()
}

func eval006(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range snap.Services {
		for _, api := range svc.APIs {
			pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
			if !ok || pol.External {
				continue
			}
			if len(runtimegw.FindElements(pol.Inbound, "llm-emit-token-metric")) == 0 {
				continue
			}
			seen = true
			if hasMetricPipeline(snap, svc, api) {
				a.note = append(a.note, "custom metrics with dimensions cannot be verified metadata-only")
				continue
			}
			if !hasLLMLogSink(svc, api) {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "token telemetry policy has no API Management diagnostic or Azure Monitor sink")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-emit-token-metric-policy"}}
	}
	return a.result()
}

func validJWT(el runtimegw.PolicyElement) bool {
	openIDConfig := openIDConfigURL(el)
	if hasExpression(openIDConfig) || hasExpression(leafText(el, "issuer")) {
		return false
	}
	if len(children(el, "audiences")) == 0 || len(desc(el, "audience")) == 0 {
		return false
	}
	hasIssuerSource := openIDConfig != "" || len(desc(el, "issuer")) > 0
	if !hasIssuerSource {
		return false
	}
	if oc := strings.ToLower(strings.TrimSpace(openIDConfig)); oc != "" {
		return !strings.Contains(oc, "/common/") && !strings.Contains(oc, "/organizations/")
	}
	if issuers := desc(el, "issuer"); len(issuers) > 0 {
		for _, is := range issuers {
			if hasTenantHint(leafText(is, "")) {
				return true
			}
		}
	}
	for _, claims := range children(el, "required-claims") {
		for _, claim := range desc(claims, "claim") {
			if strings.EqualFold(claim.Attrs["name"], "tid") && len(desc(claim, "value")) > 0 {
				return true
			}
		}
	}
	return false
}

func validAADToken(el runtimegw.PolicyElement) bool {
	tenant := strings.ToLower(strings.TrimSpace(el.Attrs["tenant-id"]))
	if tenant == "" || strings.Contains(tenant, "organizations") || strings.Contains(tenant, "common") || hasExpression(tenant) {
		return false
	}
	return len(desc(el, "audience")) > 0 || len(desc(el, "application-id")) > 0
}

func validTokenLimit(el runtimegw.PolicyElement) bool {
	if strings.TrimSpace(el.Attrs["counter-key"]) == "" || strings.TrimSpace(el.Attrs["estimate-prompt-tokens"]) == "" {
		return false
	}
	if tpm := strings.TrimSpace(el.Attrs["tokens-per-minute"]); tpm != "" {
		return true
	}
	period := strings.TrimSpace(el.Attrs["token-quota-period"])
	switch period {
	case "Hourly", "Daily", "Weekly", "Monthly", "Yearly":
		return strings.TrimSpace(el.Attrs["token-quota"]) != ""
	default:
		return false
	}
}

func validTokenMetric(el runtimegw.PolicyElement) bool {
	dims := desc(el, "dimension")
	if len(dims) > 5 {
		return false
	}
	for _, d := range dims {
		if strings.TrimSpace(d.Attrs["name"]) == "" {
			return false
		}
	}
	return true
}

func hasMetricDiagnostic(snap runtimegw.Snapshot, svc *runtimegw.Service, api *runtimegw.API) bool {
	for _, d := range svc.Diagnostics {
		if !d.Metrics {
			continue
		}
		logger, ok := resolveLogger(svc, d.LoggerID)
		if !ok || !strings.EqualFold(logger.LoggerType, "applicationInsights") || !loggerResourceExists(snap, logger) {
			continue
		}
		if d.Scope == "service" || d.Scope == "api:"+strings.ToLower(api.Name) {
			return true
		}
	}
	return false
}

func hasMetricPipeline(snap runtimegw.Snapshot, svc *runtimegw.Service, api *runtimegw.API) bool {
	return hasMetricDiagnostic(snap, svc, api)
}

func hasLLMLogSink(svc *runtimegw.Service, api *runtimegw.API) bool {
	for _, d := range svc.Diagnostics {
		if !d.LargeLanguageModel {
			continue
		}
		if d.Scope == "service" || d.Scope == "api:"+strings.ToLower(api.Name) {
			return true
		}
	}
	for _, d := range svc.MonitorDiagnostics {
		if strings.TrimSpace(d.WorkspaceID) == "" {
			continue
		}
		if d.Scope == "" || strings.Contains(d.Scope, "/service/"+strings.ToLower(svc.Name)) {
			return true
		}
	}
	return false
}

func resolveLogger(svc *runtimegw.Service, loggerID string) (runtimegw.Logger, bool) {
	loggerID = strings.ToLower(strings.TrimSpace(loggerID))
	if loggerID == "" {
		return runtimegw.Logger{}, false
	}
	if logger, ok := svc.Loggers[loggerID]; ok {
		return logger, true
	}
	for name, logger := range svc.Loggers {
		if loggerID == strings.ToLower(name) || strings.HasSuffix(loggerID, "/loggers/"+strings.ToLower(name)) {
			return logger, true
		}
	}
	return runtimegw.Logger{}, false
}

func loggerResourceExists(snap runtimegw.Snapshot, logger runtimegw.Logger) bool {
	if strings.TrimSpace(logger.ResourceID) == "" {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(logger.ResourceID))
	if _, ok := snap.AppInsights[key]; ok {
		return true
	}
	if strings.HasSuffix(key, "/providers/microsoft.insights/components/"+lastSegment(key)) {
		_, ok := snap.AppInsights[lastSegment(key)]
		return ok
	}
	return false
}

func openIDConfigURL(el runtimegw.PolicyElement) string {
	if v := strings.TrimSpace(el.Attrs["openid-config"]); v != "" {
		return v
	}
	for _, child := range desc(el, "openid-config") {
		if v := strings.TrimSpace(child.Attrs["url"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(child.Text); v != "" {
			return v
		}
	}
	return ""
}

func clientIDMatchesServiceIdentity(snap runtimegw.Snapshot, svc *runtimegw.Service, clientID string) bool {
	for _, id := range svc.UserAssignedIDs {
		key := strings.ToLower(strings.TrimSpace(id))
		mi, ok := snap.UserAssigned[key]
		if !ok {
			mi, ok = snap.UserAssigned[lastSegment(key)]
		}
		if !ok {
			continue
		}
		if strings.EqualFold(mi.ClientID, clientID) {
			return true
		}
	}
	return false
}

func principalForClientID(snap runtimegw.Snapshot, svc *runtimegw.Service, clientID string) string {
	for _, id := range svc.UserAssignedIDs {
		key := strings.ToLower(strings.TrimSpace(id))
		mi, ok := snap.UserAssigned[key]
		if !ok {
			mi, ok = snap.UserAssigned[lastSegment(key)]
		}
		if ok && strings.EqualFold(mi.ClientID, clientID) {
			return mi.PrincipalID
		}
	}
	return ""
}

func foundryTargets(snap runtimegw.Snapshot, svc *runtimegw.Service, api *runtimegw.API) []runtimegw.FoundryAccount {
	seen := map[string]bool{}
	var out []runtimegw.FoundryAccount
	for _, host := range targetHosts(svc, api) {
		acct, ok := snap.FoundryAccounts[host]
		if !ok || seen[acct.ResourceID] {
			continue
		}
		seen[acct.ResourceID] = true
		out = append(out, acct)
	}
	return out
}

func targetHosts(svc *runtimegw.Service, api *runtimegw.API) []string {
	var out []string
	seenBackends := map[string]bool{}
	if host := hostLabel(runtimegw.HostFromURL(api.ServiceURL)); host != "" {
		out = append(out, host)
	}
	pol, ok := runtimegw.EffectivePolicyForAPI(svc, api)
	if !ok || pol.External {
		return unique(out)
	}
	for _, el := range runtimegw.FindElements(pol.Inbound, "set-backend-service") {
		if host := hostLabel(runtimegw.HostFromURL(el.Attrs["base-url"])); host != "" {
			out = append(out, host)
		}
		if id := strings.ToLower(strings.TrimSpace(el.Attrs["backend-id"])); id != "" {
			out = append(out, backendHosts(svc, id, seenBackends)...)
		}
	}
	return unique(out)
}

func backendsForAPI(svc *runtimegw.Service, pol runtimegw.EffectivePolicy) []runtimegw.Backend {
	var out []runtimegw.Backend
	seen := map[string]bool{}
	for _, el := range runtimegw.FindElements(pol.Inbound, "set-backend-service") {
		id := strings.ToLower(strings.TrimSpace(el.Attrs["backend-id"]))
		if id == "" {
			continue
		}
		out = append(out, collectBackends(svc, id, seen)...)
	}
	return out
}

func hasAPIKeyHeader(names []string) bool {
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), "api-key") {
			return true
		}
	}
	return false
}

type roleCoverage int

const (
	roleMissing roleCoverage = iota
	roleCovered
	roleUncertain
)

func cognitiveRoleCoverage(snap runtimegw.Snapshot, principalID string, targets []runtimegw.FoundryAccount) roleCoverage {
	principalID = strings.ToLower(strings.TrimSpace(principalID))
	if principalID == "" || len(targets) == 0 {
		return roleMissing
	}
	overallUncertain := false
	for _, target := range targets {
		targetCoverage := roleMissing
		for _, ra := range snap.RoleAssignments {
			if !strings.EqualFold(strings.TrimSpace(ra.PrincipalID), principalID) {
				continue
			}
			if !isCognitiveRole(ra.RoleDefinitionID) {
				continue
			}
			if strings.TrimSpace(ra.Condition) != "" {
				targetCoverage = roleUncertain
				continue
			}
			switch {
			case scopeCoversTarget(ra.Scope, target.ResourceID):
				targetCoverage = roleCovered
			case scopeCoverageUncertain(ra.Scope, target.ResourceID):
				if targetCoverage != roleCovered {
					targetCoverage = roleUncertain
				}
			}
		}
		if targetCoverage == roleMissing {
			return roleMissing
		}
		if targetCoverage == roleUncertain {
			overallUncertain = true
		}
	}
	if overallUncertain {
		return roleUncertain
	}
	return roleCovered
}

func isCognitiveRole(roleDefinitionID string) bool {
	roleDefinitionID = strings.ToLower(strings.TrimSpace(roleDefinitionID))
	return strings.HasSuffix(roleDefinitionID, "5e0bd9bd-7b93-4f28-af87-19fc36ad61bd") ||
		strings.HasSuffix(roleDefinitionID, "a97b65f3-24c7-4388-baec-2e87135dc908")
}

func scopeCoversTarget(scope, target string) bool {
	scope = strings.ToLower(strings.TrimRight(strings.TrimSpace(scope), "/"))
	target = strings.ToLower(strings.TrimRight(strings.TrimSpace(target), "/"))
	if scope == "" || target == "" {
		return false
	}
	return target == scope || strings.HasPrefix(target, scope+"/") || targetHasQualifiedSuffix(scope, target)
}

func scopeCoverageUncertain(scope, target string) bool {
	scope = strings.ToLower(strings.TrimRight(strings.TrimSpace(scope), "/"))
	target = strings.ToLower(strings.TrimRight(strings.TrimSpace(target), "/"))
	if scope == "" || target == "" || !strings.HasPrefix(target, "/providers/") {
		return false
	}
	if strings.HasPrefix(scope, "/providers/microsoft.management/managementgroups/") {
		return !targetHasQualifiedSuffix(scope, target)
	}
	if !strings.HasPrefix(scope, "/subscriptions/") {
		return false
	}
	if strings.Contains(scope, "/providers/") {
		return false
	}
	return true
}

func targetHasQualifiedSuffix(scope, target string) bool {
	return strings.HasPrefix(target, "/providers/") &&
		strings.HasSuffix(scope, target)
}

func backendHosts(svc *runtimegw.Service, id string, seen map[string]bool) []string {
	var out []string
	for _, backend := range collectBackends(svc, id, seen) {
		if host := hostLabel(runtimegw.HostFromURL(backend.URL)); host != "" {
			out = append(out, host)
		}
	}
	return out
}

func collectBackends(svc *runtimegw.Service, id string, seen map[string]bool) []runtimegw.Backend {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" || seen[id] {
		return nil
	}
	seen[id] = true
	backend, ok := svc.Backends[id]
	if !ok {
		backend, ok = svc.Backends[lastSegment(id)]
	}
	if !ok {
		return nil
	}
	out := []runtimegw.Backend{backend}
	for _, member := range backend.PoolMemberIDs {
		out = append(out, collectBackends(svc, member, seen)...)
	}
	return out
}

func hostLabel(host string) string {
	if i := strings.Index(host, "."); i >= 0 {
		return strings.ToLower(host[:i])
	}
	return strings.ToLower(strings.TrimSpace(host))
}

func children(el runtimegw.PolicyElement, name string) []runtimegw.PolicyElement {
	var out []runtimegw.PolicyElement
	for _, c := range el.Children {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func desc(el runtimegw.PolicyElement, name string) []runtimegw.PolicyElement {
	if name == "" {
		return []runtimegw.PolicyElement{el}
	}
	var out []runtimegw.PolicyElement
	for _, c := range el.Children {
		if c.Name == name {
			out = append(out, c)
		}
		out = append(out, desc(c, name)...)
	}
	return out
}

func leafText(el runtimegw.PolicyElement, _ string) string { return strings.TrimSpace(el.Text) }

func hasTenantHint(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Contains(s, "/tenants/") || strings.Contains(s, "/tfp/") || strings.Contains(s, "/v2.0")
}

func hasExpression(v string) bool { return runtimegw.HasExpression(v) }

func hasSystemAssignedID(t string) bool {
	return strings.Contains(strings.ToLower(t), "systemassigned")
}
func hasUserAssignedID(t string) bool { return strings.Contains(strings.ToLower(t), "userassigned") }

func normPath(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), "/")
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
