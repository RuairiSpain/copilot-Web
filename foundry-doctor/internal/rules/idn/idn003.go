package idn

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// idn003 implements FND-IDN-003: role assignments use object IDs and principalType.
type idn003 struct{ meta }

func newIDN003() rules.Rule { return idn003{meta{"FND-IDN-003", 1}} }

// minStableRoleAPI is the first stable role assignment API version that supports principalType (catalogue:
// the RBAC article names 2018-09-01-preview as the minimum and 2022-04-01 as the first stable).
const minStableRoleAPI = "2022-04-01"

var apiDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func (r idn003) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, stop := armGuard(r, in); stop {
		return res, nil
	}
	var out []rules.Result
	for _, ra := range in.ARM.ResourcesOfType(typeRoleAssign) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("FND-IDN-003: %w", err)
		}
		out = append(out, r.check(in.ARM, ra)...)
	}
	return out, nil
}

func (r idn003) check(t *model.ARMTemplate, ra model.Resource) []rules.Result {
	var out []rules.Result
	bad := func(key, ptr, why string, base sdk.Confidence) {
		out = append(out, fail(r, ra, key, ptr, fmt.Sprintf("role assignment %s: %s", ra.Name, why), rec003, fix003, base))
	}
	p := resolvePrincipal(t, ra)
	_, sameTemplate := p.unique()
	sameTemplate = sameTemplate || (p.kind == principalRef && len(p.cands) > 0)
	sameTemplate = sameTemplate && p.field == fieldObjectID

	// principalId
	skipped := false
	switch p.kind {
	case principalAbsent, principalNotString:
		bad("principal-id-missing", "/properties/principalId", "properties.principalId is not set", sdk.ConfidenceCertain)
	case principalBadLit:
		bad("principal-id-not-guid", "/properties/principalId", "principalId is a literal that is not a GUID object ID", sdk.ConfidenceCertain)
	case principalParam:
		skipped = true
		out = append(out, skipRes(r, ra, rules.ReasonUnresolvedValue("parameter"),
			fmt.Sprintf("principalId is the parameter %q; its object-ID nature cannot be checked statically", p.param), "parameter value"))
	case principalModule:
		skipped = true
		out = append(out, skipRes(r, ra, rules.ReasonUnresolvedValue("module-output"),
			"principalId is read from a module output and cannot be evaluated statically", "resolved module output"))
	case principalUnknown:
		skipped = true
		out = append(out, skipRes(r, ra, rules.ReasonUnresolvedValue("arm-expression"),
			"principalId is an expression the rule does not interpret", "resolved principalId"))
	case principalRef:
		switch p.field {
		case fieldClientID:
			bad("principal-id-is-client-id", "/properties/principalId",
				"principalId reads clientId or applicationId (property "+strings.TrimPrefix(p.chain, ".")+"); a role assignment needs the object ID (principalId)", sdk.ConfidenceCertain)
		case fieldUnknown:
			skipped = true
			out = append(out, skipRes(r, ra, rules.ReasonUnresolvedValue("arm-expression"),
				"principalId reads a property the rule does not classify as object ID or client ID", "resolved principalId"))
		}
	}

	// principalType
	pt, hasPT := rawString(ra, "properties", "principalType")
	_, ptPresent := rawAt(ra.Body, "properties", "principalType")
	switch {
	case !ptPresent:
		why := "principalType is not set; the RBAC documentation says an assignment to a newly created principal can fail in some cases because of replication delay, so this is a reliability risk, not a certain failure"
		if sameTemplate {
			why = "principalType is not set on an assignment to an identity created in the same template (" + strings.TrimPrefix(p.chain, ".") + "); " + strings.TrimPrefix(why, "principalType is not set; ")
		}
		key := "principal-type-missing"
		if sameTemplate {
			key = "principal-type-missing-same-template"
		}
		out = append(out, uncertain(r, ra, key, "/properties/principalType", fmt.Sprintf("role assignment %s: %s", ra.Name, why), rec003, fix003))
	case !hasPT || model.IsARMExpression(pt):
		skipped = true
		out = append(out, skipRes(r, ra, rules.ReasonUnresolvedValue("arm-expression"), "principalType is an expression or parameter", "resolved principalType"))
	case strings.EqualFold(pt, "AgentUser") || strings.EqualFold(pt, "AgentServicePrincipal"):
		out = append(out, uncertain(r, ra, "agent-principal-type", "/properties/principalType",
			fmt.Sprintf("role assignment %s: principalType %s is a documented enum value, but the Foundry documentation does not state which value an agent identity needs", ra.Name, pt), rec003, fix003))
	case sameTemplate && !strings.EqualFold(pt, "ServicePrincipal"):
		bad("principal-type-not-service-principal", "/properties/principalType",
			fmt.Sprintf("principalId is an identity created in the same template (%s) but principalType is %q; it must be ServicePrincipal", strings.TrimPrefix(p.chain, "."), pt), sdk.ConfidenceCertain)
	}

	// apiVersion
	if ptPresent {
		if d := apiDateRe.FindString(ra.APIVersion); d != "" && d < minStableRoleAPI {
			bad("api-version-too-old", "", fmt.Sprintf("apiVersion %s is older than %s while principalType is set", ra.APIVersion, minStableRoleAPI), sdk.ConfidenceCertain)
		}
	}

	if len(out) == 0 && !skipped {
		return []rules.Result{rules.Pass()}
	}
	return out
}
