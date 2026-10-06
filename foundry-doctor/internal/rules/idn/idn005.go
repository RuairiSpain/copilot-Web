package idn

import (
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	roleFoundryOwner        = "c883944f-8b7b-4483-af10-35834be79c4a"
	roleFoundryAccountOwner = "e47c6f54-e4a2-4754-9501-8e0985b135e1"
	uncertainPrefix         = "uncertain: "
)

func evalIDN005(in *sdk.Input) sdk.Result {
	var (
		findings   []sdk.Finding
		subOwners  int
		considered bool
		unresolved bool
	)
	assignments := ofType(in, typeRoleAssign)
	if len(assignments) == 0 {
		return skip(sdk.SkipInputUnavailable)
	}
	for _, r := range assignments {
		def, present, isExpr := strOf(r.Properties, "roleDefinitionId")
		if !present {
			continue
		}
		if isExpr {
			unresolved = true
			continue
		}
		role := roleGUID(def)
		label, ok := humanPrivilegedRole(role)
		if !ok {
			continue
		}
		pt, ptPresent, ptExpr := strOf(r.Properties, "principalType")
		if ptExpr || !ptPresent {
			unresolved = true
			continue
		}
		if !strings.EqualFold(pt, "User") && !strings.EqualFold(pt, "Group") {
			continue
		}
		considered = true
		scope := classifyScope(r.Scope)
		if scope == scopeUnknown {
			unresolved = true
			continue
		}
		if isSubscriptionScope(r.Scope) && role == roleOwner {
			subOwners++
		}
		findings = append(findings, sdk.Finding{
			Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
			Location: r.Location,
			Evidence: fmt.Sprintf("%s assignment for principalType %s at scope %q is standing human admin access that requires review", label, pt, r.Scope),
		})
	}
	if subOwners > 3 {
		findings = append(findings, sdk.Finding{
			Resource: sdk.ResourceRef{Type: typeRoleAssign, Name: "subscription-owner-count"},
			Evidence: fmt.Sprintf("subscription scope has %d Owner assignments in the template; review against the documented maximum of 3", subOwners),
		})
	} else if considered && subOwners > 0 && subOwners < 2 {
		findings = append(findings, sdk.Finding{
			Resource: sdk.ResourceRef{Type: typeRoleAssign, Name: "subscription-owner-count"},
			Evidence: fmt.Sprintf("subscription scope has %d Owner assignment in the template; review against the documented expectation of more than one owner", subOwners),
		})
	}
	if len(findings) > 0 {
		return sdk.Result{Findings: findings}
	}
	switch {
	case unresolved:
		return skip(uncertainPrefix + "effective privileged human access cannot be proven from template-only role assignments")
	case considered:
		return skip(sdk.SkipInputUnavailable + ": PIM schedule data is not available in template-only evaluation")
	default:
		return skip(sdk.SkipInputUnavailable + ": live privileged human assignment inventory is not available")
	}
}

func humanPrivilegedRole(id string) (string, bool) {
	switch id {
	case roleOwner:
		return "Owner", true
	case roleContributor:
		return "Contributor", true
	case roleUAA:
		return "User Access Administrator", true
	case roleRBACAdmin:
		return "Role Based Access Control Administrator", true
	case roleFoundryOwner:
		return "Foundry Owner", true
	case roleFoundryAccountOwner:
		return "Foundry Account Owner", true
	default:
		return "", false
	}
}

func isSubscriptionScope(scope string) bool {
	l := strings.ToLower(strings.Trim(scope, "/"))
	parts := strings.Split(l, "/")
	return len(parts) == 2 && parts[0] == "subscriptions"
}
