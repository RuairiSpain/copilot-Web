package idn

import (
	"fmt"
	"regexp"
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

func evalIDN004(in *sdk.Input) sdk.Result {
	var a acc
	apps := appIdentityResources(in)
	excluded := configuredDeploymentPrincipals(in)
	evaluated := false
	excludedOnly := false
	missingData := false
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
		role, ok := armexpr.RoleDefinitionGUID(def)
		if !ok {
			if armexpr.IsExpression(def) {
				a.unresolved = true
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
		pt, ptPresent, ptExpr := strOf(r.Properties, "principalType")
		if ptExpr || !ptPresent {
			a.unresolved = true // cannot tell whether the principal is an application identity
			continue
		}
		switch pt {
		case "AgentServicePrincipal", "AgentUser":
		case "ServicePrincipal":
			pid, pidPresent, _ := strOf(r.Properties, "principalId")
			if v, ok := r.Properties["principalId"].(string); ok && unresolved(v) {
				pid = v
			}
			if pidPresent && excluded[strings.ToLower(strings.TrimSpace(pid))] {
				excludedOnly = true
				continue
			}
			// A principal that cannot be tied to an application identity may be a
			// deployment identity; that is uncertain, never a failure.
			if !pidPresent || classifyPrincipal(pid, apps) != principalApp {
				a.unresolved = true
				continue
			}
		default:
			continue // human and group principals are out of scope
		}
		// An empty scope is the deployment scope (resource group, subscription
		// or management group), which is broad. Expressions are classified by
		// armexpr (resourceGroup().id, subscription().id, format() paths,
		// resourceId() calls).
		lvl := armexpr.ClassifyScope(r.Scope)
		if lvl == armexpr.ScopeUnknown {
			a.unresolved = true
			continue
		}
		evaluated = true
		var condNote string
		if role == roleUAA || role == roleRBACAdmin {
			switch v, why := evalCondition(r); v {
			case condRestrictive:
				continue
			case condUncertain:
				a.unresolved = true
				continue
			case condIneffective:
				condNote = "; condition is ineffective: " + why
			}
		}
		switch {
		case lvl == armexpr.ScopeBroad:
			a.add(r, fmt.Sprintf("%s granted to an application identity at broad scope %q%s", name, r.Scope, condNote))
		case lvl == armexpr.ScopeResource && role != roleContributor:
			a.add(r, fmt.Sprintf("%s granted to an application identity at resource scope %q%s", name, r.Scope, condNote))
		}
	}
	switch {
	case len(a.fs) > 0:
		return sdk.Result{Findings: a.fs}
	case a.unresolved:
		return skip(SkipUnresolved)
	case a.conditional:
		return skip(foundrylink.SkipConditional)
	case evaluated:
		return sdk.Result{}
	case excludedOnly:
		return skip(sdk.SkipInputUnavailable)
	case missingData:
		return skip(sdk.SkipInputUnavailable)
	default:
		return sdk.Result{}
	}
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
