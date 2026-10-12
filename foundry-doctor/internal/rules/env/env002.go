package env

import (
	"context"
	"fmt"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// copiedValues is FND-ENV-002.
type copiedValues struct{ src Source }

func (copiedValues) ID() string   { return "FND-ENV-002" }
func (copiedValues) Version() int { return 1 }

const env002Rec = "Review each reported key and give the environment its own value, or suppress the finding with a reason and " +
	"an expiry where the shared value is intentional. Prefer deriving names from AZURE_ENV_NAME."

const env002Fix = "# .azure/prod/.env\nAZURE_RESOURCE_GROUP=rg-myapp-prod\nAZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project\n"

// Evaluate reports environment-bound keys with an equal, non-empty value in a production and a
// non-production environment whose AZURE_ENV_NAME differs. ADR-007 makes the rule depend on
// policy.environments.production and nonProduction; there is no name-based guess. The finding
// carries the key and the environment names only: no value and no hash of a value.
func (r copiedValues) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, ok := needTiers(r, in); !ok {
		return res, nil
	}
	envs := visible(r.src, in)
	if len(envs) < 2 {
		return []rules.Result{skipSingle(r, in, len(envs))}, nil
	}
	pairs := tierPairs(envs, in.Policy)
	if len(pairs) == 0 {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"sibling-environments-not-visible: no visible production and non-production environment pair",
			"a visible .azure/<env>/.env for a production and a non-production environment", sdk.ResourceRef{Kind: "project", Name: in.Project.Name})}, nil
	}
	var out []rules.Result
	for _, p := range pairs {
		if tierName(p.prod) == tierName(p.non) {
			continue
		}
		d, err := compare.Environments(ctx, p.prod, p.non)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.ID(), err)
		}
		for _, k := range d.Keys {
			if k.Kind != compare.Same || !k.Bound || value(p.prod, k.Key) == "" {
				continue
			}
			f := rules.NewFinding(r, sdk.ResourceRef{Kind: "env-key", Name: k.Key},
				fmt.Sprintf("Environment-bound key %s has an equal value in environment %s (production) and %s (non-production). The value is not shown.",
					k.Key, p.prod.Name, p.non.Name), env002Rec)
			f.Key = "copied-value:" + k.Key + ":" + p.prod.Name + "+" + p.non.Name
			f.Location = envLocation(p.prod.Name)
			f.Fix = env002Fix
			f.Confidence = sdk.ConfidenceLikely
			out = append(out, rules.Fail(f))
		}
	}
	return results(out), nil
}

type tierPair struct{ prod, non model.Environment }

// tierPairs returns each (production, non-production) pair of visible environments in name order.
func tierPairs(envs []model.Environment, p model.Policy) []tierPair {
	var out []tierPair
	for _, a := range envs {
		if !inList(p.Environments.Production, a.Name) {
			continue
		}
		for _, b := range envs {
			if inList(p.Environments.NonProduction, b.Name) && a.Name != b.Name {
				out = append(out, tierPair{a, b})
			}
		}
	}
	return out
}

// needTiers returns a skip when policy.environments.production or nonProduction is missing.
func needTiers(r rules.Rule, in *model.Input) ([]rules.Result, bool) {
	for _, k := range []struct {
		key string
		set bool
	}{
		{model.KeyEnvironmentsProduction, in.Policy.Environments.Production != nil},
		{model.KeyEnvironmentsNonProduction, in.Policy.Environments.NonProduction != nil},
	} {
		if !k.set {
			return []rules.Result{rules.SkipFor(r, rules.ReasonProfileKeyMissing(k.key),
				"environment tiers are not configured; the tier is never guessed from the environment name",
				"profile key "+k.key, sdk.ResourceRef{Kind: "project", Name: in.Project.Name})}, false
		}
	}
	return nil, true
}
