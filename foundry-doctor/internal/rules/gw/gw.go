// Package gw implements the Phase 8 API Management AI gateway checks.
//
// Policy rules evaluate the effective inbound policy of every operation and
// product path of an API (runtime/gateway). An operation fails only when every
// product path fails; when the paths disagree the coverage depends on the
// caller's product and the result is uncertain. Unresolved policy text
// (unparsable XML, external links, fragments outside the template, conditional
// <choose> branches) and APIM resources with an API version outside
// runtimegw.KnownAPIVersion are uncertain, never "policy absent".
package gw

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
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
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": compiled ARM model unavailable"}}, nil
	}
	return r.fn(in), nil
}

type acc struct {
	findings []sdk.Finding
	skip     []string
	note     []string
}

func (a *acc) add(loc sdk.Location, typ, name, key, evidence string) {
	a.findings = append(a.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: typ, Name: name},
		Key:      key,
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
			if c := strings.Compare(x.Key, y.Key); c != 0 {
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

func sortedServices(snap runtimegw.Snapshot) []*runtimegw.Service {
	keys := make([]string, 0, len(snap.Services))
	for k := range snap.Services {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*runtimegw.Service, 0, len(keys))
	for _, k := range keys {
		out = append(out, snap.Services[k])
	}
	return out
}

func sortedAPIs(svc *runtimegw.Service) []*runtimegw.API {
	keys := make([]string, 0, len(svc.APIs))
	for k := range svc.APIs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]*runtimegw.API, 0, len(keys))
	for _, k := range keys {
		out = append(out, svc.APIs[k])
	}
	return out
}

// versionGate records an uncertain note and returns false when an APIM
// resource behind api has an unverified API version.
func versionGate(a *acc, svc *runtimegw.Service, api *runtimegw.API) bool {
	issues := svc.UnverifiedVersions(api)
	if len(issues) == 0 {
		return true
	}
	a.note = append(a.note, svc.Name+"/"+api.Name+": "+strings.Join(issues, ", ")+" (verified: "+runtimegw.KnownAPIVersionList+")")
	return false
}

// aiAPI reports whether api fronts a model endpoint; an API whose backing is
// hidden by an unresolved policy adds an uncertain note.
func aiAPI(a *acc, svc *runtimegw.Service, api *runtimegw.API) bool {
	if svc.AIAPIBacked(api) {
		return true
	}
	if svc.AIBackingUnknown(api) {
		a.note = append(a.note, "backend of "+svc.Name+"/"+api.Name+" is hidden by an unresolved policy")
	}
	return false
}

// failure is one definite policy defect of one effective-policy variant.
type failure struct {
	key string
	msg string
}

// policyCheck evaluates one resolved variant; a non-empty uncertain string
// means the variant cannot be decided.
type policyCheck func(runtimegw.EffectivePolicy) (fails []failure, uncertain string)

// evaluateAPI folds the variant outcomes of every operation of api.
func evaluateAPI(a *acc, svc *runtimegw.Service, api *runtimegw.API, check policyCheck) {
	if !svc.HasPolicyDocument(api) {
		a.note = append(a.note, "no policy document applies to "+svc.Name+"/"+api.Name+" in the template; its policies may be deployed elsewhere")
		return
	}
	variants := runtimegw.EffectivePoliciesForAPI(svc, api)
	byOp := map[string][]runtimegw.EffectivePolicy{}
	var ops []string
	for _, v := range variants {
		if _, ok := byOp[v.Operation]; !ok {
			ops = append(ops, v.Operation)
		}
		byOp[v.Operation] = append(byOp[v.Operation], v)
	}
	for _, op := range ops {
		target := svc.Name + "/" + api.Name
		if op != "" {
			target += "/" + op
		}
		var failing, passing int
		var fails []failure
		var uncertain []string
		for _, v := range byOp[op] {
			if v.External {
				uncertain = append(uncertain, strings.Join(v.Unresolved, "; "))
				continue
			}
			f, unsure := check(v)
			switch {
			case unsure != "":
				uncertain = append(uncertain, unsure)
			case len(f) > 0:
				failing++
				fails = append(fails, f...)
			default:
				passing++
			}
		}
		switch {
		case len(uncertain) > 0:
			a.note = append(a.note, "effective policy of "+target+" is uncertain: "+strings.Join(unique(uncertain), "; "))
		case failing > 0 && passing == 0:
			seen := map[string]bool{}
			for _, f := range fails {
				if seen[f.key] {
					continue
				}
				seen[f.key] = true
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, opKey(op, f.key), gwEvidence(f.msg, svc, api, op))
			}
		case failing > 0:
			a.note = append(a.note, "policy coverage of "+target+" depends on the product the caller subscribes to")
		}
	}
}

func opKey(op, key string) string {
	if op == "" {
		return key
	}
	return "operation:" + op + ":" + key
}

// gwEvidence names the ARM resources and the policy scopes that produced the
// effective policy.
func gwEvidence(msg string, svc *runtimegw.Service, api *runtimegw.API, op string) string {
	where := "API scope"
	if op != "" {
		where = "operation " + op
	}
	return msg + " [ARM Microsoft.ApiManagement/service/apis/" + svc.Name + "/" + api.Name + " (api-version " + orNone(api.APIVersion) + "); effective inbound policy of " + where + " resolved from service, product, API and operation scopes]"
}

func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "none"
	}
	return v
}

func eval001(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		for _, api := range sortedAPIs(svc) {
			if !aiAPI(a, svc, api) {
				continue
			}
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			evaluateAPI(a, svc, api, func(v runtimegw.EffectivePolicy) ([]failure, string) {
				jwts := runtimegw.TopLevel(v.Inbound, "validate-jwt")
				aads := runtimegw.TopLevel(v.Inbound, "validate-azure-ad-token")
				if len(jwts)+len(aads) == 0 {
					if runtimegw.Conditional(v.Inbound, "validate-jwt") || runtimegw.Conditional(v.Inbound, "validate-azure-ad-token") {
						return nil, "token validation is only inside a <choose> branch"
					}
					return []failure{{"no-token-validation", "AI gateway API has no validate-jwt or validate-azure-ad-token policy"}}, ""
				}
				var fails []failure
				for _, el := range jwts {
					ok, unsure := jwtStatus(el)
					if unsure {
						return nil, "validate-jwt uses a policy expression or named value for its issuer source"
					}
					if !ok {
						fails = append(fails, failure{"validate-jwt-unconstrained", "API gateway policy does not constrain validate-jwt with audience, issuer source and tenant"})
					}
				}
				for _, el := range aads {
					ok, unsure := aadTokenStatus(el)
					if unsure {
						return nil, "validate-azure-ad-token uses a policy expression or named value for tenant-id"
					}
					if !ok {
						fails = append(fails, failure{"validate-azure-ad-token-unconstrained", "API gateway policy does not constrain validate-azure-ad-token with a specific tenant and audience or application ID"})
					}
				}
				return fails, ""
			})
		}
	}
	if !seen {
		if len(a.note) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-ai-gateway-apis"}}
	}
	return a.result()
}

func eval002(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		for _, api := range sortedAPIs(svc) {
			if !aiAPI(a, svc, api) {
				continue
			}
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			consumption := strings.EqualFold(svc.SKU, "Consumption")
			evaluateAPI(a, svc, api, func(v runtimegw.EffectivePolicy) ([]failure, string) {
				limits := runtimegw.TopLevel(v.Inbound, "llm-token-limit")
				metrics := runtimegw.TopLevel(v.Inbound, "llm-emit-token-metric")
				if len(limits) == 0 && runtimegw.Conditional(v.Inbound, "llm-token-limit") || len(metrics) == 0 && runtimegw.Conditional(v.Inbound, "llm-emit-token-metric") {
					return nil, "token policy is only inside a <choose> branch"
				}
				if len(limits) == 0 && len(runtimegw.FindElements(v.Inbound, "azure-openai-token-limit")) > 0 ||
					len(metrics) == 0 && len(runtimegw.FindElements(v.Inbound, "azure-openai-emit-token-metric")) > 0 {
					return nil, "legacy azure-openai-* token policy name present; whether the service still accepts it is unverified"
				}
				var fails []failure
				if len(limits) == 0 {
					fails = append(fails, failure{"llm-token-limit-missing", "AI gateway API is missing llm-token-limit in inbound policy"})
				}
				if len(metrics) == 0 {
					fails = append(fails, failure{"llm-emit-token-metric-missing", "AI gateway API is missing llm-emit-token-metric in inbound policy"})
				}
				for _, el := range limits {
					if !validTokenLimit(el) {
						fails = append(fails, failure{"llm-token-limit-invalid", "llm-token-limit is present but missing required attributes or quota settings"})
					}
					if consumption {
						fails = append(fails, failure{"llm-token-limit-tier", "llm-token-limit is not supported on the Consumption tier"})
					}
				}
				for _, el := range metrics {
					if !validTokenMetric(el) {
						fails = append(fails, failure{"llm-emit-token-metric-invalid", "llm-emit-token-metric has more than five dimensions or a dimension without a name"})
					}
				}
				return fails, ""
			})
		}
	}
	if !seen {
		if len(a.note) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "api-not-fronting-a-model-endpoint"}}
	}
	return a.result()
}

func eval003(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		backendNames := make([]string, 0, len(svc.Backends))
		for k := range svc.Backends {
			backendNames = append(backendNames, k)
		}
		slices.Sort(backendNames)
		for _, name := range backendNames {
			b := svc.Backends[name]
			for _, member := range b.PoolMemberIDs {
				if hasExpression(member) || strings.HasPrefix(strings.TrimSpace(member), "[") {
					a.note = append(a.note, "backend pool member of "+svc.Name+"/"+b.Name+" is an unresolved expression")
					continue
				}
				if _, ok := svc.Backends[strings.ToLower(lastSegment(member))]; !ok {
					a.add(b.SourceLocation, "Microsoft.ApiManagement/service/backends", b.Name, "pool-member:"+strings.ToLower(lastSegment(member)),
						"backend pool member does not resolve to an existing backend resource [ARM Microsoft.ApiManagement/service/backends/"+svc.Name+"/"+b.Name+"]")
				}
			}
		}
		for _, api := range sortedAPIs(svc) {
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			evaluateAPI(a, svc, api, func(v runtimegw.EffectivePolicy) ([]failure, string) {
				backends := runtimegw.FindElements(v.Inbound, "set-backend-service")
				if len(backends) == 0 {
					answered := len(runtimegw.FindElements(v.Inbound, "return-response")) > 0 || len(runtimegw.FindElements(v.Inbound, "mock-response")) > 0
					if strings.TrimSpace(api.ServiceURL) == "" && !answered {
						return []failure{{"no-backend", "API has neither serviceUrl nor set-backend-service policy"}}, ""
					}
					return nil, ""
				}
				var fails []failure
				for _, el := range backends {
					id := strings.TrimSpace(el.Attrs["backend-id"])
					switch {
					case id == "":
						if strings.TrimSpace(el.Attrs["base-url"]) == "" {
							fails = append(fails, failure{"backend-unspecified", "set-backend-service must specify backend-id or base-url"})
						}
					case runtimegw.HasExpression(id):
						return nil, "backend-id uses a policy expression"
					case svc.Backends[strings.ToLower(id)].Name == "":
						fails = append(fails, failure{"backend-missing:" + strings.ToLower(id), "set-backend-service backend-id " + id + " does not match a backend resource"})
					}
				}
				return fails, ""
			})
		}
	}
	if !seen {
		if len(a.findings) > 0 || len(a.note) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-apis"}}
	}
	return a.result()
}

func eval004(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		groups := map[string]string{}
		for _, api := range sortedAPIs(svc) {
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			raw := strings.TrimSpace(api.Path)
			if strings.HasPrefix(raw, "[") {
				a.note = append(a.note, "path-from-unresolved-parameter: path of "+svc.Name+"/"+api.Name+" is an ARM expression")
				continue
			}
			path := normPath(raw)
			group := strings.ToLower(api.BaseName)
			if api.VersionSetID != "" {
				group = strings.ToLower(api.VersionSetID)
			}
			if prev, ok := groups[path]; ok && prev != group {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "duplicate-path",
					"API path duplicates another API path in the same service [ARM Microsoft.ApiManagement/service/apis/"+svc.Name+"/"+api.Name+" (api-version "+orNone(api.APIVersion)+")]")
				continue
			}
			groups[path] = group
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-apis"}}
	}
	return a.result()
}

func eval005(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		for _, api := range sortedAPIs(svc) {
			if !aiAPI(a, svc, api) {
				continue
			}
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			identity := strings.TrimSpace(svc.IdentityType)
			if svc.Declared && identity == "" {
				a.add(svc.SourceLocation, "Microsoft.ApiManagement/service", svc.Name, "service-identity:"+api.Name,
					"API Management service has no managed identity [ARM Microsoft.ApiManagement/service/"+svc.Name+" (api-version "+orNone(svc.APIVersion)+")]")
			}
			evaluateAPI(a, svc, api, func(v runtimegw.EffectivePolicy) ([]failure, string) {
				return managedIdentityCheck(snap, svc, api, v)
			})
		}
	}
	if !seen {
		if len(a.note) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "non-foundry-backend-host"}}
	}
	return a.result()
}

// managedIdentityCheck evaluates one variant for FND-GW-005. Identities are
// matched to role assignments by literal principal ID or by a reference
// expression naming the identity; anything not provable from the template is
// uncertain, never a failure.
func managedIdentityCheck(snap runtimegw.Snapshot, svc *runtimegw.Service, api *runtimegw.API, v runtimegw.EffectivePolicy) ([]failure, string) {
	var fails []failure
	var unsure []string
	idents := runtimegw.TopLevel(v.Inbound, "authentication-managed-identity")
	if len(idents) == 0 && runtimegw.Conditional(v.Inbound, "authentication-managed-identity") {
		return nil, "authentication-managed-identity is only inside a <choose> branch"
	}
	identityType := strings.TrimSpace(svc.IdentityType)
	identityExpr := strings.HasPrefix(identityType, "[")
	targets := foundryTargets(snap, svc, api, v)
	for _, el := range idents {
		res := strings.TrimSuffix(strings.TrimSpace(el.Attrs["resource"]), "/")
		switch {
		case res == "":
			fails = append(fails, failure{"mi-resource-missing", "authentication-managed-identity has no resource attribute"})
			continue
		case hasExpression(res):
			unsure = append(unsure, "authentication-managed-identity resource is a policy expression or named value")
			continue
		case res != "https://cognitiveservices.azure.com":
			unsure = append(unsure, "authentication-managed-identity targets audience "+res+"; only https://cognitiveservices.azure.com is verified")
			continue
		}
		clientID := strings.TrimSpace(el.Attrs["client-id"])
		var p principal
		switch {
		case clientID != "" && hasExpression(clientID):
			unsure = append(unsure, "authentication-managed-identity client-id is a policy expression or named value")
			continue
		case clientID != "":
			if identityExpr || !svc.Declared {
				unsure = append(unsure, "API Management identity is not visible as a literal in the template")
				continue
			}
			if !hasUserAssignedID(identityType) {
				fails = append(fails, failure{"mi-client-id-without-uai", "authentication-managed-identity client-id is set but the APIM service has no user-assigned identity"})
				continue
			}
			mi, state := userAssignedFor(snap, svc, clientID)
			switch state {
			case uaiMismatch:
				fails = append(fails, failure{"mi-client-id-mismatch", "authentication-managed-identity client-id does not match a user-assigned identity on the APIM service"})
				continue
			case uaiUnknown:
				unsure = append(unsure, "client-id cannot be matched to a user-assigned identity because client IDs are runtime values")
				continue
			}
			p = principal{literal: mi.PrincipalID, refs: []string{"microsoft.managedidentity/userassignedidentities", "'" + strings.ToLower(mi.Name) + "'", "principalid"}}
		default:
			if identityExpr || !svc.Declared {
				unsure = append(unsure, "API Management identity is not visible as a literal in the template")
				continue
			}
			if !hasSystemAssignedID(identityType) {
				if identityType != "" {
					fails = append(fails, failure{"mi-system-identity-missing", "authentication-managed-identity requires a system-assigned identity when client-id is omitted"})
				}
				continue
			}
			p = principal{literal: svc.PrincipalID, refs: []string{"microsoft.apimanagement/service", "'" + strings.ToLower(svc.Name) + "'", "identity.principalid"}}
		}
		switch cov, why := cognitiveRoleCoverage(snap, p, targets); cov {
		case roleMissing:
			fails = append(fails, failure{"mi-role-missing", "managed identity used by authentication-managed-identity lacks a Cognitive Services role on the Foundry resource"})
		case roleUncertain:
			unsure = append(unsure, why)
		}
	}
	if len(idents) == 0 {
		fails = append(fails, failure{"mi-missing", "AI gateway API does not authenticate to Foundry with authentication-managed-identity"})
	}
	for _, backend := range backendsForAPI(svc, v) {
		if hasAPIKeyHeader(backend.HeaderNames) {
			fails = append(fails, failure{"backend-api-key:" + strings.ToLower(backend.Name), "backend " + backend.Name + " credentials supply an api-key header instead of using managed identity"})
		}
		if backend.AuthorizationCredentialsPresent {
			fails = append(fails, failure{"backend-authorization:" + strings.ToLower(backend.Name), "backend " + backend.Name + " authorization credentials are header-based and do not prove managed identity authentication"})
		}
	}
	for _, el := range runtimegw.FindElements(v.Inbound, "set-header") {
		if strings.EqualFold(strings.TrimSpace(el.Attrs["name"]), "api-key") {
			fails = append(fails, failure{"set-header-api-key", "AI gateway policy sets an api-key header instead of using managed identity"})
		}
	}
	if len(fails) > 0 {
		return fails, ""
	}
	if len(unsure) > 0 {
		return nil, strings.Join(unique(unsure), "; ")
	}
	return nil, ""
}

func eval006(in *sdk.Input) sdk.Result {
	snap := snapshot(in)
	a := &acc{}
	seen := false
	for _, svc := range sortedServices(snap) {
		for _, api := range sortedAPIs(svc) {
			variants := runtimegw.EffectivePoliciesForAPI(svc, api)
			present, unresolved := false, false
			for _, v := range variants {
				if v.External {
					unresolved = true
					continue
				}
				if len(runtimegw.FindElements(v.Inbound, "llm-emit-token-metric")) > 0 {
					present = true
				}
			}
			if !present {
				if unresolved {
					a.note = append(a.note, "effective policy of "+svc.Name+"/"+api.Name+" is unresolved, so token telemetry policy presence is unknown")
				}
				continue
			}
			seen = true
			if !versionGate(a, svc, api) {
				continue
			}
			if hasMetricPipeline(snap, svc, api) {
				a.note = append(a.note, "custom metrics with dimensions cannot be verified metadata-only")
				continue
			}
			if metricDiagnosticUnresolved(svc, api) {
				a.note = append(a.note, "a metrics diagnostic of "+svc.Name+"/"+api.Name+" names its logger through an unresolved template expression, so the telemetry sink is uncertain")
				continue
			}
			if !hasLLMLogSink(svc, api) {
				a.add(api.SourceLocation, "Microsoft.ApiManagement/service/apis", api.Name, "telemetry-sink",
					"token telemetry policy has no API Management diagnostic (metrics or largeLanguageModel.logs enabled) or Azure Monitor sink [ARM Microsoft.ApiManagement/service/apis/"+svc.Name+"/"+api.Name+" (api-version "+orNone(api.APIVersion)+"); ARM Microsoft.ApiManagement/service/diagnostics]")
			}
		}
	}
	if !seen {
		if len(a.note) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-emit-token-metric-policy"}}
	}
	return a.result()
}

var guidRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// jwtStatus reports whether validate-jwt constrains audience, an issuer
// source and the tenant; unsure is true when the issuer source is an
// expression or named value.
func jwtStatus(el runtimegw.PolicyElement) (ok, unsure bool) {
	openIDConfig := openIDConfigURL(el)
	if hasExpression(openIDConfig) {
		return false, true
	}
	issuers := desc(el, "issuer")
	for _, is := range issuers {
		if hasExpression(is.Text) {
			return false, true
		}
	}
	if len(children(el, "audiences")) == 0 || len(desc(el, "audience")) == 0 {
		return false, false
	}
	if openIDConfig == "" && len(issuers) == 0 {
		return false, false
	}
	if openIDConfig != "" {
		return hasTenantHint(openIDConfig), false
	}
	for _, is := range issuers {
		if hasTenantHint(is.Text) {
			return true, false
		}
	}
	for _, claims := range children(el, "required-claims") {
		for _, claim := range desc(claims, "claim") {
			if strings.EqualFold(claim.Attrs["name"], "tid") && len(desc(claim, "value")) > 0 {
				return true, false
			}
		}
	}
	return false, false
}

func aadTokenStatus(el runtimegw.PolicyElement) (ok, unsure bool) {
	tenant := strings.ToLower(strings.TrimSpace(el.Attrs["tenant-id"]))
	if hasExpression(tenant) {
		return false, true
	}
	if tenant == "" || tenant == "organizations" || tenant == "common" || tenant == "consumers" {
		return false, false
	}
	return len(desc(el, "audience")) > 0 || len(desc(el, "application-id")) > 0, false
}

// hasTenantHint reports whether an issuer or OpenID configuration URL names
// one tenant: a tenant GUID, or a domain segment that is not common,
// organizations or consumers.
func hasTenantHint(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	seg, _, _ := strings.Cut(strings.Trim(u.Path, "/"), "/")
	switch seg {
	case "", "common", "organizations", "consumers":
		return false
	}
	return guidRe.MatchString(seg) || strings.Contains(seg, ".")
}

// principal identifies an APIM identity in role assignments: a literal
// principal ID when the template states one, or a reference expression that
// contains every refs substring.
type principal struct {
	literal string
	refs    []string
}

func (p principal) matches(raw string) bool {
	r := strings.ToLower(strings.TrimSpace(raw))
	if r == "" {
		return false
	}
	if lit := strings.ToLower(strings.TrimSpace(p.literal)); lit != "" && r == lit {
		return true
	}
	if !strings.HasPrefix(r, "[") || len(p.refs) == 0 {
		return false
	}
	for _, ref := range p.refs {
		if !strings.Contains(r, ref) {
			return false
		}
	}
	return true
}

type uaiState int

const (
	uaiMatched uaiState = iota
	uaiMismatch
	uaiUnknown
)

// userAssignedFor matches a literal client-id against the user-assigned
// identities attached to svc. Client IDs are runtime values, so a template
// identity without a literal clientId (or one not in the template) is unknown.
func userAssignedFor(snap runtimegw.Snapshot, svc *runtimegw.Service, clientID string) (runtimegw.ManagedIdentity, uaiState) {
	state := uaiMismatch
	for _, id := range svc.UserAssignedIDs {
		key := strings.ToLower(strings.TrimSpace(id))
		mi, ok := snap.UserAssigned[key]
		if !ok {
			mi, ok = snap.UserAssigned[lastSegment(key)]
		}
		if !ok || strings.TrimSpace(mi.ClientID) == "" || hasExpression(mi.ClientID) || strings.HasPrefix(mi.ClientID, "[") {
			state = uaiUnknown
			continue
		}
		if strings.EqualFold(mi.ClientID, clientID) {
			return mi, uaiMatched
		}
	}
	if len(svc.UserAssignedIDs) == 0 {
		return runtimegw.ManagedIdentity{}, uaiUnknown
	}
	return runtimegw.ManagedIdentity{}, state
}

func foundryTargets(snap runtimegw.Snapshot, svc *runtimegw.Service, api *runtimegw.API, v runtimegw.EffectivePolicy) []runtimegw.FoundryAccount {
	seen := map[string]bool{}
	var out []runtimegw.FoundryAccount
	for _, host := range targetHosts(svc, api, v) {
		acct, ok := snap.FoundryAccounts[host]
		if !ok || seen[acct.ResourceID] {
			continue
		}
		seen[acct.ResourceID] = true
		out = append(out, acct)
	}
	return out
}

func targetHosts(svc *runtimegw.Service, api *runtimegw.API, v runtimegw.EffectivePolicy) []string {
	var out []string
	seenBackends := map[string]bool{}
	if host := hostLabel(runtimegw.HostFromURL(api.ServiceURL)); host != "" {
		out = append(out, host)
	}
	for _, el := range runtimegw.FindElements(v.Inbound, "set-backend-service") {
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

type roleCoverage int

const (
	roleMissing roleCoverage = iota
	roleCovered
	roleUncertain
)

// Built-in role GUIDs. Cognitive Services OpenAI User and Cognitive Services
// User were verified for the GW-005 catalogue entry. Foundry User (formerly
// Azure AI User) is recognised from third-party role catalogues only
// (UNVERIFIED against Microsoft Learn on 2026-10-18), so it yields uncertain.
const (
	roleCognitiveServicesOpenAIUser = "5e0bd9bd-7b93-4f28-af87-19fc36ad61bd"
	roleCognitiveServicesUser       = "a97b65f3-24c7-4388-baec-2e87135dc908"
	roleFoundryUserUnverified       = "53ca6127-db72-4b80-b1b0-d745d6d5456d"
)

type roleKind int

const (
	roleOther roleKind = iota
	roleVerified
	roleUnverified
)

func classifyRole(roleDefinitionID string) roleKind {
	id := strings.ToLower(roleDefinitionID)
	switch {
	case strings.Contains(id, roleCognitiveServicesOpenAIUser), strings.Contains(id, roleCognitiveServicesUser):
		return roleVerified
	case strings.Contains(id, roleFoundryUserUnverified):
		return roleUnverified
	default:
		return roleOther
	}
}

// cognitiveRoleCoverage decides whether p holds a Cognitive Services role on
// every Foundry target. Missing is reported only when the template assigns
// roles to p (so it manages p's RBAC) and a target is still uncovered.
func cognitiveRoleCoverage(snap runtimegw.Snapshot, p principal, targets []runtimegw.FoundryAccount) (roleCoverage, string) {
	if len(targets) == 0 {
		return roleUncertain, "the Foundry resource behind the gateway is not in the template (role-assignment-scope-not-readable)"
	}
	anyForPrincipal := false
	missing, uncertain := false, false
	for _, target := range targets {
		coverage := roleMissing
		for _, ra := range snap.RoleAssignments {
			if !p.matches(ra.PrincipalID) {
				continue
			}
			anyForPrincipal = true
			kind := classifyRole(ra.RoleDefinitionID)
			if kind == roleOther {
				continue
			}
			scope := strings.TrimSpace(ra.Scope)
			switch {
			case strings.TrimSpace(ra.Condition) != "":
				if coverage != roleCovered {
					coverage = roleUncertain
				}
			case strings.HasPrefix(scope, "[") && scopeReferencesAccount(scope, target.Name):
				if kind == roleVerified {
					coverage = roleCovered
				} else if coverage != roleCovered {
					coverage = roleUncertain
				}
			case scope == "", strings.HasPrefix(scope, "["):
				if coverage != roleCovered {
					coverage = roleUncertain
				}
			case scopeCoversTarget(ra.Scope, target.ResourceID):
				if kind == roleVerified {
					coverage = roleCovered
				} else if coverage != roleCovered {
					coverage = roleUncertain
				}
			case scopeCoverageUncertain(ra.Scope, target.ResourceID):
				if coverage != roleCovered {
					coverage = roleUncertain
				}
			}
		}
		switch coverage {
		case roleMissing:
			missing = true
		case roleUncertain:
			uncertain = true
		}
	}
	switch {
	case !anyForPrincipal:
		return roleUncertain, "no role assignment for the APIM identity is visible in the template; it may be assigned in another deployment"
	case missing:
		return roleMissing, ""
	case uncertain:
		return roleUncertain, "role coverage for the APIM identity could not be proven from template-scoped RBAC metadata alone"
	default:
		return roleCovered, ""
	}
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

// metricDiagnosticUnresolved reports whether a metrics-enabled diagnostic in
// scope for api refers to its logger, or the logger to its Application
// Insights component, through an ARM expression such as resourceId(...). The
// compiled template of a correct Bicep file looks like that, so the sink can
// neither be confirmed nor refuted and the rule must not fail.
func metricDiagnosticUnresolved(svc *runtimegw.Service, api *runtimegw.API) bool {
	for _, d := range svc.Diagnostics {
		if !d.Metrics || !(d.Scope == "service" || d.Scope == "api:"+strings.ToLower(api.Name)) {
			continue
		}
		// loggerId is required, so an empty value means the compiled model could
		// not resolve the expression that produced it.
		if id := strings.TrimSpace(d.LoggerID); id == "" || strings.HasPrefix(id, "[") {
			return true
		}
		if logger, ok := resolveLogger(svc, d.LoggerID); ok && strings.HasPrefix(strings.TrimSpace(logger.ResourceID), "[") {
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
		// A deployed logger ID ends in /loggers/<name>. The compiled template's
		// resourceId() reduces to <service>/<name>, so that form matches too.
		if loggerID == strings.ToLower(name) || strings.HasSuffix(loggerID, "/loggers/"+strings.ToLower(name)) || strings.HasSuffix(loggerID, "/"+strings.ToLower(name)) {
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

func hasAPIKeyHeader(names []string) bool {
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), "api-key") {
			return true
		}
	}
	return false
}

// scopeReferencesAccount reports whether an ARM scope expression references
// the Cognitive Services account by its literal name, for example
// [resourceId('Microsoft.CognitiveServices/accounts', 'acct')].
func scopeReferencesAccount(scope, account string) bool {
	s := strings.ToLower(scope)
	name := strings.ToLower(strings.TrimSpace(account))
	return name != "" && strings.Contains(s, "microsoft.cognitiveservices/accounts") && strings.Contains(s, "'"+name+"'") &&
		!strings.Contains(s, "/projects") && !strings.Contains(s, "/deployments")
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
