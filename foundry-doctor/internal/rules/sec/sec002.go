package sec

import (
	"context"
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule002 is FND-SEC-002: Azure AI Search used by Foundry rejects API keys.
type rule002 struct{}

func (rule002) ID() string   { return "FND-SEC-002" }
func (rule002) Version() int { return 1 }

func (r rule002) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	return run(ctx, r, in)
}

const (
	rec002 = "Set disableLocalAuth to true and remove authOptions, then assign Entra roles (for example index data roles) to the Foundry project identity. If a migration needs keys temporarily, use authOptions.aadOrApiKey with aadAuthFailureMode and track removal with a suppression expiry."
	fix002 = "resource search 'Microsoft.Search/searchServices@2025-05-01' = {\n  properties: {\n    disableLocalAuth: true\n  }\n}"
)

// scoped resolves whether a Foundry-side reference reaches res. When ok is false, skipResult explains why.
func scoped(r rules.Rule, ix *foundryIndex, res model.Resource, k targetKind) (sc scopeResult, d doubt, skipResult *rules.Result) {
	if notDeployed(res) {
		s := notApplicable(r, res, "the resource condition is false, so it is not deployed")
		return sc, d, &s
	}
	sc = ix.scope(res, k)
	if sc.state == scopeOut {
		s := notApplicable(r, res, "out-of-scope: no Foundry connection (metadata.ResourceId) or capability setting names this resource")
		return sc, d, &s
	}
	d = assess(append([]model.Resource{res}, sc.related...)...)
	if sc.state == scopeUncertain && !d.uncertain {
		d = doubt{uncertain: true, conf: sdk.ConfidenceUncertain, why: sc.why}
	}
	return sc, d, nil
}

// scopeText describes how the resource is tied to Foundry, for evidence.
func scopeText(sc scopeResult) string {
	if sc.state == scopeIn {
		return "referenced by " + strings.Join(sc.refs, ", ")
	}
	return "Foundry scope could not be established statically (" + sc.why + ")"
}

func (r rule002) evaluateARM(in *model.Input) []rules.Result {
	var out []rules.Result
	ix := buildIndex(in.ARM)
	for _, res := range in.ARM.ResourcesOfType(typSearch) {
		sc, d, skipped := scoped(r, ix, res, searchKind)
		if skipped != nil {
			out = append(out, *skipped)
			continue
		}
		const prop = "properties.disableLocalAuth"
		state, v := boolOf(res, prop)
		switch state {
		case "true":
			out = append(out, rules.Pass())
		case "unresolved":
			out = append(out, unresolvedSkip(r, res, prop, v))
		case "invalid":
			out = append(out, invalidSkip(r, res, prop))
		default:
			sub := searchSubState(res)
			ev := prop + " is " + map[string]string{"false": "false", "absent": "absent"}[state] +
				"; authOptions sub-state: " + sub + "; " + scopeText(sc) + " (apiVersion " + safeAPI(res.APIVersion) + ")"
			out = append(out, finding(r, res, d, "disableLocalAuth:"+sub, prop, ev, rec002, fix002))
		}
	}
	return out
}

// searchSubState names which data-plane authentication option the service states.
func searchSubState(res model.Resource) string {
	api := res.Get("properties.authOptions.apiKeyOnly")
	aad := res.Get("properties.authOptions.aadOrApiKey")
	switch {
	case api.State == model.Unresolved || aad.State == model.Unresolved:
		return "unresolved"
	case api.State == model.Literal:
		return "apiKeyOnly"
	case aad.State == model.Literal:
		mode := res.Get("properties.authOptions.aadOrApiKey.aadAuthFailureMode")
		if s, ok := mode.String(); ok && failureMode.MatchString(s) {
			return "aadOrApiKey(aadAuthFailureMode=" + s + ")"
		}
		return "aadOrApiKey"
	}
	return "none"
}

// failureMode is the shape of an aadAuthFailureMode value; anything else is not echoed.
var failureMode = regexp.MustCompile(`^http[0-9]{3}[A-Za-z]{0,30}$`)

// isSafeToken accepts short identifier-like enum values so free text never reaches the evidence.
func isSafeToken(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
