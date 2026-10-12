package idn

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// idn004 implements FND-IDN-004: no broad Owner/Contributor for application identities.
type idn004 struct{ meta }

func newIDN004() rules.Rule { return idn004{meta{"FND-IDN-004", 1}} }

// privilegedRole is one privileged administrator role of the catalogue (Azure built-in roles reference).
type privilegedRole struct {
	slug, name  string
	conditional bool // an ABAC condition downgrades the finding (documentation recommends conditions)
	narrowOK    bool // allowed at the scope of a single resource
}

var privilegedRoles = map[string]privilegedRole{
	"8e3af657-a8ff-443c-a75c-2fe8c4bcb635": {slug: "owner", name: "Owner"},
	"b24988ac-6180-42a0-ab88-20f7382dd24c": {slug: "contributor", name: "Contributor", narrowOK: true},
	"18d7d88d-d35e-4fb5-a5c3-7773c20a72d9": {slug: "user-access-administrator", name: "User Access Administrator", conditional: true},
	"f58310d9-a9f6-439a-9e8d-f62e7b41a168": {slug: "rbac-administrator", name: "Role Based Access Control Administrator", conditional: true},
}

func (r idn004) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, stop := armGuard(r, in); stop {
		return res, nil
	}
	t := in.ARM
	var out []rules.Result
	for _, ra := range t.ResourcesOfType(typeRoleAssign) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("FND-IDN-004: %w", err)
		}
		out = append(out, r.check(t, ra)...)
	}
	out = append(out, liveSkip(r, "live role assignments at subscription, resource group and Foundry scopes are not read in offline mode"))
	return out, nil
}

// appKind classifies the principal of an assignment.
type appKind struct {
	app, deploy, known bool
	desc               string
}

func classifyPrincipal(t *model.ARMTemplate, p principal) appKind {
	if p.kind != principalRef || p.field != fieldObjectID || len(p.cands) == 0 {
		return appKind{}
	}
	k := appKind{known: true}
	for _, c := range p.cands {
		switch {
		case p.system && (isType(c, typeAccount) || isType(c, typeProject) || isType(c, typeContainerAp) || isType(c, typeSite) || isType(c, typeSlot)):
			k.app = true
			k.desc = c.Type + " " + c.Name
		case isType(c, typeUAMI):
			if attachedToApp(t, c) {
				k.app = true
				k.desc = "user-assigned identity " + c.Name
			}
			if hasFederatedCredential(t, c) {
				k.deploy = true
				if k.desc == "" {
					k.desc = "user-assigned identity " + c.Name
				}
			}
		}
	}
	return k
}

func attachedToApp(t *model.ARMTemplate, u model.Resource) bool {
	for _, typ := range []string{typeAccount, typeProject, typeContainerAp, typeSite, typeSlot} {
		for _, a := range t.ResourcesOfType(typ) {
			if containsRes(attachedIdentities(t, a), u) {
				return true
			}
		}
	}
	return false
}

// hasFederatedCredential reports whether a federated credential is declared under the identity, the signal the
// rule uses for a deployment (pipeline) identity.
func hasFederatedCredential(t *model.ARMTemplate, u model.Resource) bool {
	fics := t.ResourcesOfType(typeFIC)
	uSegs := nameSegments(u.Name)
	for _, f := range fics {
		fSegs := nameSegments(f.Name)
		if len(fSegs) >= 2 && len(uSegs) >= 1 && nameKey(fSegs[0]) == nameKey(uSegs[len(uSegs)-1]) {
			return true
		}
	}
	return len(fics) > 0 && len(t.ResourcesOfType(typeUAMI)) == 1
}

func (r idn004) check(t *model.ARMTemplate, ra model.Resource) []rules.Result {
	rawRole, _ := rawString(ra, "properties", "roleDefinitionId")
	guid, ok, _ := roleGUIDText(rawRole)
	role, priv := privilegedRoles[guid]
	if ok && !priv {
		return nil
	}
	p := resolvePrincipal(t, ra)
	k := classifyPrincipal(t, p)
	if !k.known {
		if !ok {
			return nil
		}
		return []rules.Result{skipRes(r, ra, rules.ReasonUnresolvedValue("principal"),
			fmt.Sprintf("%s is assigned to a principal that cannot be tied to an identity of the template; whether it is an application identity is unknown", role.name), "resolved principal")}
	}
	if !k.app {
		return []rules.Result{rules.Pass()}
	}
	if !ok {
		return []rules.Result{skipRes(r, ra, rules.ReasonUnresolvedValue("role-definition"),
			"the role definition ID of an assignment to an application identity is not a literal", "resolved role definition ID")}
	}
	if k.deploy {
		return []rules.Result{uncertain(r, ra, "application-and-deployment-identity", "/properties/principalId",
			fmt.Sprintf("%s is assigned to %s, which runs the workload and also carries a federated credential (a pipeline signal); it cannot be told whether it is a deployment identity", role.name, k.desc), rec004, fix004)}
	}
	scope, present := scopeOf(ra)
	if present && scope.kind == idUnknown {
		return []rules.Result{uncertain(r, ra, role.slug+"-scope-unknown", "/scope",
			fmt.Sprintf("%s is assigned to %s at a scope the rule cannot classify", role.name, k.desc), rec004, fix004)}
	}
	if role.narrowOK && scope.kind == idResource {
		return []rules.Result{rules.Pass()}
	}
	if cond, has := rawString(ra, "properties", "condition"); role.conditional && has && strings.TrimSpace(cond) != "" {
		return []rules.Result{rules.Pass()}
	}
	where, sk := "the deployment scope (resource group or wider)", "deployment"
	if present {
		where = scope.describe() + " scope"
		sk = scope.keyName()
	}
	key := role.slug + "-" + sk
	return []rules.Result{fail(r, ra, key, "/properties/roleDefinitionId",
		fmt.Sprintf("%s (%s) is assigned to application identity %s at %s", role.name, guid, k.desc, where), rec004, fix004, sdk.ConfidenceLikely)}
}
