package sec

import (
	"context"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule001 is FND-SEC-001: the Foundry account rejects API-key authentication.
type rule001 struct{}

func (rule001) ID() string   { return "FND-SEC-001" }
func (rule001) Version() int { return 1 }

func (r rule001) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	return run(ctx, r, in)
}

const (
	rec001 = "Set properties.disableLocalAuth to true on the Foundry account and grant callers Entra ID roles instead of distributing keys. Keep keys enabled only in dev with a recorded suppression (reason and expiry)."
	fix001 = "resource account 'Microsoft.CognitiveServices/accounts@2026-09-01' = {\n  properties: {\n    disableLocalAuth: true\n  }\n}"
)

func (r rule001) evaluateARM(in *model.Input) []rules.Result {
	var out []rules.Result
	arm := in.ARM
	children := append(arm.ResourcesOfType(typProject), arm.ResourcesOfType(typDeployment)...)
	accts := arm.ResourcesOfType(typAccount)
	for _, acct := range accts {
		if notDeployed(acct) {
			out = append(out, notApplicable(r, acct, "the resource condition is false, so it is not deployed"))
			continue
		}
		owned, orphans := 0, 0
		for _, c := range children {
			switch {
			case isChildOf(c, acct):
				owned++
			case !hasKnownParent(c, accts):
				orphans++
			}
		}
		if owned == 0 && orphans == 0 {
			out = append(out, notApplicable(r, acct, "the account owns no projects or deployments in this template"))
			continue
		}
		d := assess(acct)
		if owned == 0 && !d.uncertain {
			d = doubt{uncertain: true, conf: sdk.ConfidenceUncertain, why: "a project or deployment in the template could not be tied to a parent account"}
		}
		const prop = "properties.disableLocalAuth"
		state, v := boolOf(acct, prop)
		switch state {
		case "true":
			out = append(out, rules.Pass())
		case "unresolved":
			out = append(out, unresolvedSkip(r, acct, prop, v))
		case "invalid":
			out = append(out, invalidSkip(r, acct, prop))
		default:
			ev := prop + " is false"
			if state == "absent" {
				ev = prop + " is absent, which is treated as API keys enabled"
			}
			ev += " on an account that hosts projects or deployments (apiVersion " + safeAPI(acct.APIVersion) + ")"
			out = append(out, finding(r, acct, d, "disableLocalAuth", prop, ev, rec001, fix001))
		}
	}
	return out
}

// isChildOf reports whether child is a project or deployment of the account, read from the symbolic
// name, dependsOn or the first segment of the child name.
func isChildOf(child, acct model.Resource) bool {
	if acct.Symbolic != "" {
		if strings.HasPrefix(child.Symbolic, acct.Symbolic+"::") {
			return true
		}
		for _, d := range child.DependsOn {
			if d == acct.Symbolic {
				return true
			}
		}
	}
	ck, ak := nameSegments(child.Name), nameSegments(acct.Name)
	return len(ck) >= 2 && len(ak) == 1 && ck[0] == ak[0]
}

func hasKnownParent(child model.Resource, accts []model.Resource) bool {
	for _, a := range accts {
		if isChildOf(child, a) {
			return true
		}
	}
	return false
}
