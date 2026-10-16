package idn

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

var (
	guidEqualsRe = regexp.MustCompile(`(?i)GuidEquals\s*\{([^}]*)\}`)
	guidInRe     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

type principalClass int

const (
	principalUnknown principalClass = iota
	principalApp
)

// appIdentityResources returns the template resources whose identity is an
// application identity (Foundry account/project and declared workloads).
func appIdentityResources(in *sdk.Input) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range ofType(in, typeAccounts, typeProjects, typeContainer, typeSites, typeSlots) {
		if len(r.Identity) == 0 {
			continue
		}
		if t, _, _ := strOf(r.Identity, "type"); strings.EqualFold(t, "None") {
			continue
		}
		out = append(out, r)
	}
	return out
}

// classifyPrincipal correlates a role assignment principalId to an application
// identity declared in the template. Anything it cannot prove is unknown.
//
// A literal principalId matches an identity.principalId literal. A compiled
// expression such as
// reference(resourceId('Microsoft.CognitiveServices/accounts/projects', format('ai-{0}', ...), 'proj'), '2025-06-01', 'full').identity.principalId
// matches when the resource named in its first argument is one of the apps
// (type and name segments compared with armexpr), or one of the apps'
// user-assigned identities.
func classifyPrincipal(pid string, apps []sdk.ARMResource) principalClass {
	pid = strings.TrimSpace(pid)
	if pid == "" {
		return principalUnknown
	}
	if !armexpr.IsExpression(pid) {
		for _, r := range apps {
			if v, ok, e := strOf(r.Identity, "principalId"); ok && !e && strings.EqualFold(v, pid) {
				return principalApp
			}
		}
		return principalUnknown
	}
	c, ok := armexpr.ParseCall(pid)
	if !ok || c.Fn != "reference" || !strings.Contains(strings.ToLower(c.Suffix), "principalid") {
		return principalUnknown
	}
	target := armexpr.ParseRef(pid)
	if target.Opaque {
		return principalUnknown
	}
	for _, r := range apps {
		if armexpr.Compare(target, armexpr.ParseName(r.Type, r.Name)) == armexpr.Same {
			return principalApp
		}
		if ua, ok := r.Identity["userAssignedIdentities"].(map[string]any); ok {
			for k := range ua {
				if armexpr.Compare(target, armexpr.ParseRef(k)) == armexpr.Same {
					return principalApp
				}
			}
		}
	}
	return principalUnknown
}

type condVerdict int

const (
	condNone        condVerdict = iota // no condition present
	condRestrictive                    // verified to delegate only non-privileged roles
	condIneffective                    // evaluated and does not restrict effectively
	condUncertain                      // cannot be evaluated
)

// evalCondition evaluates an ABAC condition on a role-assignment-writing role.
// It verifies the standard constrained-delegation shape: a GuidEquals set over
// the requested role definition for write and over the resource role
// definition for delete, containing no privileged administrator role.
func evalCondition(r sdk.ARMResource) (condVerdict, string) {
	cond, present, isExpr := strOf(r.Properties, "condition")
	if !present || (!isExpr && strings.TrimSpace(cond) == "") {
		return condNone, ""
	}
	if isExpr {
		return condUncertain, "condition is an unresolved expression"
	}
	ver, _, verExpr := strOf(r.Properties, "conditionVersion")
	if verExpr {
		return condUncertain, "conditionVersion is an unresolved expression"
	}
	if ver != "2.0" {
		return condUncertain, fmt.Sprintf("conditionVersion %q is not 2.0", ver)
	}
	sets := guidEqualsRe.FindAllStringSubmatch(cond, -1)
	if len(sets) == 0 {
		return condUncertain, "condition has no role definition GUID set that can be evaluated"
	}
	lc := strings.ToLower(cond)
	for _, s := range sets {
		for _, g := range guidInRe.FindAllString(s[1], -1) {
			switch strings.ToLower(g) {
			case roleOwner, roleContributor, roleUAA, roleRBACAdmin:
				return condIneffective, "condition allows delegating privileged role " + strings.ToLower(g)
			}
		}
	}
	if !strings.Contains(lc, "@request[microsoft.authorization/roleassignments:roledefinitionid]") ||
		!strings.Contains(lc, "roleassignments/write") {
		return condIneffective, "condition does not constrain the role definition of role assignment writes"
	}
	if !strings.Contains(lc, "roleassignments/delete") ||
		!strings.Contains(lc, "@resource[microsoft.authorization/roleassignments:roledefinitionid]") {
		return condIneffective, "condition does not constrain the role definition of role assignment deletes"
	}
	return condRestrictive, ""
}

// variableLookup is implemented by an ARM model that exposes the compiled
// template's literal variables. The current armmodel does not, so a role
// definition ID held in variables('x') is reported as unresolved with that
// real cause; when a model offers the lookup the literal is substituted.
type variableLookup interface {
	Variable(name string) (any, bool)
}

var variablesCallRe = regexp.MustCompile(`(?i)variables\(\s*'([^']*)'\s*\)`)

// resolveRoleDefinition returns the role definition GUID. When it cannot, why
// names the real cause.
func resolveRoleDefinition(in *sdk.Input, def string) (guid, why string, ok bool) {
	if g, ok := armexpr.RoleDefinitionGUID(def); ok {
		return g, "", true
	}
	if !armexpr.IsExpression(def) {
		return "", "roleDefinitionId is not a role definition GUID", false
	}
	names := variablesCallRe.FindAllStringSubmatch(def, -1)
	if len(names) == 0 {
		return "", "roleDefinitionId is an expression that does not end in a literal role GUID", false
	}
	lookup, canLookup := in.ARM.(variableLookup)
	resolved := def
	for _, m := range names {
		if !canLookup {
			return "", "roleDefinitionId reads variables('" + m[1] + "') and the compiled template variables are not available to the rule", false
		}
		v, found := lookup.Variable(m[1])
		lit, isStr := v.(string)
		if !found || !isStr || armexpr.IsExpression(lit) {
			return "", "roleDefinitionId reads variables('" + m[1] + "') which is not a literal string", false
		}
		resolved = strings.ReplaceAll(resolved, m[0], "'"+lit+"'")
	}
	if g, ok := armexpr.RoleDefinitionGUID(resolved); ok {
		return g, "", true
	}
	return "", "roleDefinitionId does not resolve to a literal role GUID after substituting variables", false
}

func scopeDescription(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "deployment scope"
	}
	return fmt.Sprintf("scope %q", armexpr.StripScope(scope))
}

func evalIDN004(in *sdk.Input) sdk.Result {
	var a acc
	apps := appIdentityResources(in)
	excluded := configuredDeploymentPrincipals(in)
	evaluated := false
	excludedOnly := false
	missingData := false
	var reasons []string
	why := func(r sdk.ARMResource, msg string) {
		a.unres(r, msg)
		reasons = append(reasons, msg)
	}
	for _, r := range ofType(in, typeRoleAssign) {
		if a.maybe(r) {
			continue
		}
		def, present := rawStr(r.Properties, "roleDefinitionId")
		if !present {
			missingData = true
			continue
		}
		// Compiled Bicep writes subscriptionResourceId('...roleDefinitions',
		// '<guid>') (or extensionResourceId / format); the last literal GUID
		// is the role. A role that is not a literal GUID cannot be judged.
		role, msg, ok := resolveRoleDefinition(in, def)
		if !ok {
			if armexpr.IsExpression(def) {
				why(r, msg)
			}
			continue
		}
		var name string
		switch role {
		case roleOwner:
			name = "Owner"
		case roleContributor:
			name = "Contributor"
		case roleUAA:
			name = "User Access Administrator"
		case roleRBACAdmin:
			name = "Role Based Access Control Administrator"
		default:
			continue
		}
		// Classify the principal first: a principalId that provably is a
		// Foundry identity is evaluated whether or not principalType is set.
		pt, ptPresent, ptExpr := strOf(r.Properties, "principalType")
		pid, pidPresent, _ := strOf(r.Properties, "principalId")
		if v, ok := r.Properties["principalId"].(string); ok && unresolved(v) {
			pid = v
		}
		provableApp := pidPresent && classifyPrincipal(pid, apps) == principalApp
		switch {
		case !ptExpr && (pt == "AgentServicePrincipal" || pt == "AgentUser"):
		case !ptExpr && ptPresent && pt != "ServicePrincipal" && pt != "":
			continue // human and group principals are out of scope
		default: // ServicePrincipal, not set, or an unresolved expression
			if pidPresent && excluded[strings.ToLower(strings.TrimSpace(pid))] {
				excludedOnly = true
				continue
			}
			// A principal that cannot be tied to an application identity may be
			// a deployment identity or a person; that is uncertain, never a
			// failure.
			if !provableApp {
				switch {
				case !pidPresent:
					why(r, "principalId is not set")
				case ptExpr:
					why(r, "principalType is an unresolved expression and principalId is not a provable Foundry identity")
				case !ptPresent || pt == "":
					why(r, "principalType is not set and principalId is not a provable Foundry identity")
				default:
					why(r, "principalId is not a provable Foundry identity (it may be a deployment identity)")
				}
				continue
			}
		}
		// An empty scope is the deployment scope (resource group, subscription
		// or management group), which is broad. Expressions are classified by
		// armexpr (resourceGroup().id, subscription().id, format() paths,
		// resourceId() calls).
		lvl := armexpr.ClassifyScope(r.Scope)
		if lvl == armexpr.ScopeUnknown {
			why(r, "scope is an expression that cannot be classified")
			continue
		}
		evaluated = true
		var condNote string
		if role == roleUAA || role == roleRBACAdmin {
			switch v, cwhy := evalCondition(r); v {
			case condRestrictive:
				continue
			case condUncertain:
				why(r, "condition cannot be evaluated: "+cwhy)
				continue
			case condIneffective:
				condNote = "; condition is ineffective: " + cwhy
			}
		}
		switch {
		case lvl == armexpr.ScopeBroad:
			a.addKey(r, "scope:"+role, fmt.Sprintf("%s granted to an application identity at broad %s%s", name, scopeDescription(r.Scope), condNote))
		case lvl == armexpr.ScopeResource && role != roleContributor:
			a.addKey(r, "scope:"+role, fmt.Sprintf("%s granted to an application identity at resource %s%s", name, scopeDescription(r.Scope), condNote))
		case lvl == armexpr.ScopeResource:
			// Contributor at the scope of a single resource is informational
			// (catalogue product decision: Foundry sources list Contributor as
			// the minimum role for some tools). The rule lowers the severity
			// to info for this finding; the engine never raises it.
			a.addKey(r, "scope:"+role, fmt.Sprintf("Contributor granted to an application identity at resource %s; informational only, a narrower Foundry role is preferred%s", scopeDescription(r.Scope), condNote))
			a.fs[len(a.fs)-1].Severity = sdk.SeverityInfo
		}
	}
	switch {
	case len(a.fs) > 0:
		return withSkips(a.fs, a.notes)
	case a.unresolved:
		sort.Strings(reasons)
		return skip(SkipUnresolved + ": " + strings.Join(dedupe(reasons), "; "))
	case a.conditional:
		return skip(foundrylink.SkipConditional)
	case evaluated:
		return sdk.Result{}
	case excludedOnly:
		return skip(sdk.SkipInputUnavailable + ": only excluded role assignments were found, so no assignment could be evaluated")
	case missingData:
		return skip(sdk.SkipInputUnavailable + ": role assignment data missing from the compiled ARM model")
	default:
		return sdk.Result{}
	}
}

func dedupe(s []string) []string {
	var out []string
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}

func configuredDeploymentPrincipals(in *sdk.Input) map[string]bool {
	out := map[string]bool{}
	if in == nil || in.Policy == nil {
		return out
	}
	v, ok := in.Policy.Get("identity.deploymentPrincipalIds")
	if !ok {
		return out
	}
	switch ids := v.(type) {
	case []string:
		for _, id := range ids {
			if s := strings.ToLower(strings.TrimSpace(id)); s != "" {
				out[s] = true
			}
		}
	case []any:
		for _, id := range ids {
			if s, ok := id.(string); ok {
				if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
					out[s] = true
				}
			}
		}
	}
	return out
}
