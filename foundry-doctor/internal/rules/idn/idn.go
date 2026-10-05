// Package idn implements the Phase 1 FND-IDN-* rules as static checks over the
// compiled ARM model. An unresolved ARM expression yields a skip, never a pass;
// a missing ARM model yields SkipInputUnavailable. FND-IDN-002 needs effective
// permission data and is not implemented in Phase 1.
package idn

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SkipUnresolved is the skip reason used when a decisive value is an
// unresolved ARM expression and no finding could be established.
const SkipUnresolved = "unresolved-expression"

const (
	typeAccounts    = "Microsoft.CognitiveServices/accounts"
	typeProjects    = "Microsoft.CognitiveServices/accounts/projects"
	typeContainer   = "Microsoft.App/containerApps"
	typeSites       = "Microsoft.Web/sites"
	typeSlots       = "Microsoft.Web/sites/slots"
	typeRoleAssign  = "Microsoft.Authorization/roleAssignments"
	typeFederated   = "Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials"
	githubIssuer    = "https://token.actions.githubusercontent.com"
	aksIssuerSuffix = ".oic.prod-aks.azure.com"
)

const (
	roleOwner       = "8e3af657-a8ff-443c-a75c-2fe8c4bcb635"
	roleContributor = "b24988ac-6180-42a0-ab88-20f7382dd24c"
	roleUAA         = "18d7d88d-d35e-4fb5-a5c3-7773c20a72d9"
	roleRBACAdmin   = "f58310d9-a9f6-439a-9e8d-f62e7b41a168"
)

var (
	guidRe      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	ghSubjectRe = regexp.MustCompile(`^repo:[^/:*?]+/[^/:*?]+:.+$`)
	aksSubjRe   = regexp.MustCompile(`^system:serviceaccount:[^:*?]+:[^:*?]+$`)
	apiVerRe    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
)

// Register returns the Phase 1 IDN rules in ID order.
func Register() []sdk.Rule {
	return []sdk.Rule{
		fn{"FND-IDN-001", evalIDN001},
		fn{"FND-IDN-003", evalIDN003},
		fn{"FND-IDN-004", evalIDN004},
		fn{"FND-IDN-006", evalIDN006},
	}
}

type fn struct {
	id string
	f  func(*sdk.Input) sdk.Result
}

func (r fn) ID() string { return r.id }

func (r fn) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if err := ctx.Err(); err != nil {
		return sdk.Result{}, fmt.Errorf("%s: %w", r.id, err)
	}
	if in == nil || in.ARM == nil {
		return skip(sdk.SkipInputUnavailable), nil
	}
	return r.f(in), nil
}

func skip(reason string) sdk.Result { return sdk.Result{Skipped: &sdk.Skip{Reason: reason}} }

type acc struct {
	fs         []sdk.Finding
	unresolved bool
	seen       bool
}

func (a *acc) add(r sdk.ARMResource, evidence string) {
	a.fs = append(a.fs, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
		Location: r.Location,
		Evidence: evidence,
	})
}

func (a *acc) result() sdk.Result {
	switch {
	case len(a.fs) > 0:
		return sdk.Result{Findings: a.fs}
	case !a.seen:
		return skip(sdk.SkipInputUnavailable)
	case a.unresolved:
		return skip(SkipUnresolved)
	}
	return sdk.Result{}
}

func ofType(in *sdk.Input, types ...string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range in.ARM.Resources() {
		for _, t := range types {
			if strings.EqualFold(r.Type, t) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func unresolved(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "[[")
}

// strOf returns (value, present, isExpr). Non-string present values are
// treated as present with an empty value.
func strOf(m map[string]any, key string) (string, bool, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false, false
	}
	if unresolved(v) {
		return "", true, true
	}
	s, _ := v.(string)
	return s, true, false
}

// ---- FND-IDN-001 -----------------------------------------------------------

func evalIDN001(in *sdk.Input) sdk.Result {
	var a acc
	for _, r := range ofType(in, typeAccounts, typeProjects) {
		a.seen = true
		checkIdentity(&a, r)
	}
	for _, r := range ofType(in, typeContainer, typeSites, typeSlots) {
		if !referencesFoundry(r) {
			continue
		}
		a.seen = true
		checkIdentity(&a, r)
	}
	return a.result()
}

func checkIdentity(a *acc, r sdk.ARMResource) {
	if r.Identity == nil {
		a.add(r, "no identity block; managed identity is not enabled")
		return
	}
	t, present, isExpr := strOf(r.Identity, "type")
	switch {
	case isExpr:
		a.unresolved = true
		return
	case !present || strings.EqualFold(strings.TrimSpace(t), "None") || strings.TrimSpace(t) == "":
		a.add(r, fmt.Sprintf("identity.type is %q; managed identity is not enabled", t))
		return
	}
	if strings.Contains(strings.ToLower(t), "userassigned") {
		uai, ok := r.Identity["userAssignedIdentities"]
		switch {
		case ok && unresolved(uai):
			a.unresolved = true
		case !ok || uai == nil:
			a.add(r, fmt.Sprintf("identity.type %q requires a non-empty userAssignedIdentities map", t))
		default:
			if m, isMap := uai.(map[string]any); isMap && len(m) == 0 {
				a.add(r, fmt.Sprintf("identity.type %q has an empty userAssignedIdentities map", t))
			}
		}
	}
}

// referencesFoundry reports whether a workload carries a Foundry endpoint,
// project or connection setting in its environment or app settings.
func referencesFoundry(r sdk.ARMResource) bool {
	var pairs []any
	if v, ok := dig(r.Properties, "template", "containers"); ok {
		if cs, ok := v.([]any); ok {
			for _, c := range cs {
				if cm, ok := c.(map[string]any); ok {
					if env, ok := cm["env"].([]any); ok {
						pairs = append(pairs, env...)
					}
				}
			}
		}
	}
	if v, ok := dig(r.Properties, "siteConfig", "appSettings"); ok {
		if s, ok := v.([]any); ok {
			pairs = append(pairs, s...)
		}
	}
	for _, p := range pairs {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		val, _ := m["value"].(string)
		n := strings.ToUpper(name)
		if strings.Contains(n, "AZURE_AI_PROJECT") || strings.Contains(n, "FOUNDRY") ||
			strings.Contains(strings.ToLower(val), ".services.ai.azure.com") {
			return true
		}
	}
	return false
}

func dig(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// ---- FND-IDN-003 -----------------------------------------------------------

func evalIDN003(in *sdk.Input) sdk.Result {
	var a acc
	for _, r := range ofType(in, typeRoleAssign) {
		a.seen = true
		pt, ptPresent, ptExpr := strOf(r.Properties, "principalType")
		switch {
		case ptExpr:
			a.unresolved = true
		case !ptPresent || pt == "":
			a.add(r, "principalType is not set; set it explicitly to avoid replication-delay failures")
		}
		if ptPresent && !ptExpr && apiBefore(r.APIVersion, "2022-04-01") {
			a.add(r, fmt.Sprintf("apiVersion %q predates 2022-04-01 but principalType is set", r.APIVersion))
		}
		pid, pidPresent, pidExpr := strOf(r.Properties, "principalId")
		switch {
		case pidExpr:
			if refersToClientID(r.Properties["principalId"].(string)) {
				a.add(r, "principalId expression reads a clientId/applicationId; an object (principal) ID is required")
			} else {
				a.unresolved = true
			}
		case !pidPresent:
			a.add(r, "principalId is not set")
		case !guidRe.MatchString(pid):
			a.add(r, "principalId literal is not a GUID object ID")
		}
	}
	return a.result()
}

func refersToClientID(expr string) bool {
	l := strings.ToLower(expr)
	return strings.Contains(l, "clientid") || strings.Contains(l, "applicationid") || strings.Contains(l, "appid")
}

// apiBefore reports whether apiVersion is a well-formed date earlier than min.
// Unparseable versions are never reported.
func apiBefore(apiVersion, min string) bool {
	m := apiVerRe.FindString(apiVersion)
	return m != "" && m < min
}

// ---- FND-IDN-004 -----------------------------------------------------------

type scopeLevel int

const (
	scopeUnknown scopeLevel = iota
	scopeBroad              // subscription, management group, resource group
	scopeResource
)

func classifyScope(s string) scopeLevel {
	l := strings.ToLower(strings.Trim(s, "/"))
	parts := strings.Split(l, "/")
	switch {
	case len(parts) == 2 && parts[0] == "subscriptions":
		return scopeBroad
	case len(parts) == 4 && parts[0] == "subscriptions" && parts[2] == "resourcegroups":
		return scopeBroad
	case len(parts) == 4 && parts[0] == "providers" && parts[1] == "microsoft.management" && parts[2] == "managementgroups":
		return scopeBroad
	case len(parts) > 4 && parts[0] == "subscriptions" && parts[2] == "resourcegroups":
		return scopeResource
	}
	return scopeUnknown
}

func roleGUID(def string) string {
	i := strings.LastIndex(def, "/")
	return strings.ToLower(def[i+1:])
}

// ---- FND-IDN-006 -----------------------------------------------------------

func evalIDN006(in *sdk.Input) sdk.Result {
	var a acc
	for _, r := range ofType(in, typeFederated) {
		a.seen = true
		iss, issPresent, issExpr := strOf(r.Properties, "issuer")
		sub, subPresent, subExpr := strOf(r.Properties, "subject")
		switch {
		case issExpr:
			a.unresolved = true
		case !issPresent:
			a.add(r, "issuer is not set")
		default:
			u, err := url.Parse(iss)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				a.add(r, "issuer is not an https URL with a host")
			}
		}
		switch {
		case subExpr:
			a.unresolved = true
			continue
		case !subPresent || strings.TrimSpace(sub) == "":
			a.add(r, "subject is empty")
			continue
		case strings.ContainsAny(sub, "*?"):
			a.add(r, "subject contains a wildcard character; federated credential subjects are matched exactly")
			continue
		}
		if issExpr || !issPresent {
			continue
		}
		switch u, err := url.Parse(iss); {
		case err != nil || u.Host == "":
		case strings.EqualFold(strings.TrimRight(iss, "/"), githubIssuer):
			if !ghSubjectRe.MatchString(sub) {
				a.add(r, "GitHub Actions subject must start with repo:{owner}/{repo}:")
			}
		case strings.HasSuffix(strings.ToLower(u.Hostname()), aksIssuerSuffix):
			if !aksSubjRe.MatchString(sub) {
				a.add(r, "AKS subject must be system:serviceaccount:{namespace}:{serviceaccount}")
			}
		}
	}
	return a.result()
}
