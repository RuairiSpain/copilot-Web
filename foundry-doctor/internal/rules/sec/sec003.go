package sec

import (
	"context"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

// rule003 is FND-SEC-003: Cosmos DB accounts used by Foundry disable key-based authentication.
//
// The catalogue says the NoSQL-only scope of the PSRule equivalent is not independently verified, so
// this rule does not exclude accounts by API: no account is dropped as "not NoSQL", and the Cosmos API
// kind is not read.
type rule003 struct{}

func (rule003) ID() string   { return "FND-SEC-003" }
func (rule003) Version() int { return 1 }

func (r rule003) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	return run(ctx, r, in)
}

const (
	rec003 = "Set properties.disableLocalAuth to true and grant the Foundry project managed identity a Cosmos DB data-plane role assignment. Check that clients use Entra ID before enforcing in an existing environment."
	fix003 = "resource cosmos 'Microsoft.DocumentDB/databaseAccounts@2026-03-15' = {\n  properties: {\n    disableLocalAuth: true\n  }\n}"
)

func (r rule003) evaluateARM(in *model.Input) []rules.Result {
	var out []rules.Result
	ix := buildIndex(in.ARM)
	for _, res := range in.ARM.ResourcesOfType(typCosmos) {
		sc, d, skipped := scoped(r, ix, res, cosmosKind)
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
			ev := prop + " is " + state + "; " + scopeText(sc)
			if len(sc.authTypes) > 0 {
				var safe []string
				for _, a := range sc.authTypes {
					if isSafeToken(a) {
						safe = append(safe, a)
					}
				}
				if len(safe) > 0 {
					ev += "; connection authType (context only): " + strings.Join(safe, ", ")
				}
			}
			ev += " (apiVersion " + safeAPI(res.APIVersion) + ")"
			out = append(out, finding(r, res, d, "disableLocalAuth", prop, ev, rec003, fix003))
		}
	}
	return out
}
