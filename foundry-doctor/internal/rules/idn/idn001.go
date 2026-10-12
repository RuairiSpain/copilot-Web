package idn

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// idn001 implements FND-IDN-001: managed identities used for Foundry and workloads.
type idn001 struct{ meta }

func newIDN001() rules.Rule { return idn001{meta{"FND-IDN-001", 1}} }

// callsFoundry reports whether a workload body carries a Foundry endpoint, project or connection setting.
// It walks every string of the body: a setting name containing FOUNDRY or AI_PROJECT, or a value that is an
// expression referencing a Microsoft.CognitiveServices/accounts resource. No property path is assumed.
func callsFoundry(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for _, e := range x {
			if callsFoundry(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if callsFoundry(e) {
				return true
			}
		}
	case string:
		u := strings.ToUpper(x)
		if strings.Contains(u, "FOUNDRY") || strings.Contains(u, "AI_PROJECT") {
			return true
		}
		if model.IsARMExpression(x) && strings.Contains(u, "MICROSOFT.COGNITIVESERVICES/ACCOUNTS") {
			return true
		}
	}
	return false
}

func (r idn001) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, stop := armGuard(r, in); stop {
		return res, nil
	}
	t := in.ARM
	var out []rules.Result
	accounts := t.ResourcesOfType(typeAccount)
	projects := t.ResourcesOfType(typeProject)
	var targets []model.Resource
	targets = append(targets, accounts...)
	targets = append(targets, projects...)
	for _, typ := range []string{typeContainerAp, typeSite, typeSlot} {
		for _, w := range t.ResourcesOfType(typ) {
			if callsFoundry(w.Body) {
				targets = append(targets, w)
			}
		}
	}
	for _, res := range targets {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("FND-IDN-001: %w", err)
		}
		out = append(out, r.check(res)...)
	}
	if len(accounts) > 0 && len(projects) == 0 {
		out = append(out, skipRes(r, accounts[0], rules.ReasonNotApplicable,
			"no Foundry project is declared in the template; the project identity cannot be checked", "project resource in the compiled template"))
	}
	return out, nil
}

func (r idn001) check(res model.Resource) []rules.Result {
	base := sdk.ConfidenceCertain
	var what string
	switch {
	case isType(res, typeAccount):
		what = "Foundry account"
	case isType(res, typeProject):
		what = "Foundry project"
	default:
		base = sdk.ConfidenceLikely
		what = "workload that references Foundry"
	}
	ptr := "/identity"
	bad := func(key, why string) []rules.Result {
		return []rules.Result{fail(r, res, key, ptr, fmt.Sprintf("%s %s: %s", what, res.Name, why), rec001, fix001, base)}
	}
	switch k := identityOf(res); k {
	case identAbsent:
		return bad("identity-missing", "identity.type is not set, so the resource has no managed identity")
	case identNone:
		return bad("identity-none", "identity.type is None")
	case identParam:
		return []rules.Result{skipRes(r, res, rules.ReasonUnresolvedValue("parameter"), what+" identity.type depends on a parameter", "parameter value")}
	case identModule:
		return []rules.Result{uncertain(r, res, "identity-from-module-output", ptr,
			fmt.Sprintf("%s %s: identity.type is read from a module output and cannot be evaluated statically", what, res.Name), rec001, fix001)}
	case identUnresolved:
		return []rules.Result{skipRes(r, res, rules.ReasonUnresolvedValue("arm-expression"), what+" identity.type is an expression or unrecognised value", "resolved identity.type")}
	default:
		if k.hasUser() {
			keys, unresolved := userIdentityKeys(res)
			if unresolved {
				return []rules.Result{skipRes(r, res, rules.ReasonUnresolvedValue("arm-expression"), what+" identity.userAssignedIdentities is an expression", "resolved identity map")}
			}
			if len(keys) == 0 {
				return bad("user-assigned-identities-empty", "identity.type contains UserAssigned but identity.userAssignedIdentities is empty")
			}
		}
	}
	return []rules.Result{rules.Pass()}
}
