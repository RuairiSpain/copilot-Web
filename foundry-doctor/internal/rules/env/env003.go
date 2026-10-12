package env

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// prodReferencesNonProd is FND-ENV-003.
type prodReferencesNonProd struct{ src Source }

func (prodReferencesNonProd) ID() string   { return "FND-ENV-003" }
func (prodReferencesNonProd) Version() int { return 1 }

const env003Rec = "Point the production environment at production resources, or provision the dependency in production. " +
	"Re-run provisioning for the production environment so its outputs are written to its own .env."

const env003Fix = "# .azure/prod/.env\nAZURE_AI_PROJECT_ENDPOINT=https://prod-account.services.ai.azure.com/api/projects/prod-project\n"

// Evaluate reports values of a production environment, and azure.yaml scalars after substitution
// for it, that refer to a resource group, ARM resource ID or endpoint host of a visible
// non-production environment. Matching is exact on normalised identities (ADR-007: tiers come from
// policy.environments only). Parameter files are not part of model.Input, so only .env values and
// azure.yaml are inspected. Nothing but key names, azure.yaml paths and environment names is printed.
func (r prodReferencesNonProd) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	if res, ok := needTiers(r, in); !ok {
		return res, nil
	}
	envs := visible(r.src, in)
	pairs := tierPairs(envs, in.Policy)
	if len(pairs) == 0 {
		return []rules.Result{rules.SkipFor(r, rules.ReasonNotApplicable,
			"sibling-environments-not-visible: no visible production and non-production environment pair",
			"a visible .azure/<env>/.env for a production and a non-production environment", sdk.ResourceRef{Kind: "project", Name: in.Project.Name})}, nil
	}
	var out []rules.Result
	for _, p := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", r.ID(), err)
		}
		target := envIdentities(p.non)
		for _, k := range p.prod.Keys() {
			if k == keySubscription {
				continue
			}
			ids := identities(value(p.prod, k))
			if k == keyResourceGroup {
				ids = append(ids, scopeID(p.prod)...)
			}
			if !anyIn(ids, target) {
				continue
			}
			f := rules.NewFinding(r, sdk.ResourceRef{Kind: "env-key", Name: k},
				fmt.Sprintf("Key %s of production environment %s refers to a resource group, resource ID or endpoint host that belongs to non-production environment %s. The value is not shown.",
					k, p.prod.Name, p.non.Name), env003Rec)
			f.Key = "prod-ref:" + k + ":" + p.prod.Name + "->" + p.non.Name
			f.Location = envLocation(p.prod.Name)
			f.Fix = env003Fix
			out = append(out, rules.Fail(f))
		}
		if in.AzureYAML != nil {
			walkScalars(in.AzureYAML.Root, "", func(path string, n *model.Node) {
				if !anyIn(identities(expand(n.Value, p.prod)), target) {
					return
				}
				f := rules.NewFinding(r, sdk.ResourceRef{Kind: "file", Name: "azure.yaml", Pointer: yamlPointer(path)},
					fmt.Sprintf("azure.yaml value at %s, after substitution for production environment %s, refers to a resource group, resource ID or endpoint host that belongs to non-production environment %s. The value is not shown.",
						path, p.prod.Name, p.non.Name), env003Rec)
				f.Key = "prod-ref-yaml:" + path + ":" + p.prod.Name + "->" + p.non.Name
				f.Location = yamlLocation(in.AzureYAML, n.Pos)
				f.Fix = env003Fix
				out = append(out, rules.Fail(f))
			})
		}
	}
	return results(out), nil
}

// scopeID is the resource-group identity of an environment, from its subscription and group keys.
func scopeID(e model.Environment) []string {
	sub, rg := value(e, keySubscription), value(e, keyResourceGroup)
	if sub == "" || rg == "" {
		return nil
	}
	return identities("/subscriptions/" + sub + "/resourceGroups/" + rg)[1:]
}

func anyIn(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}

// walkScalars visits scalar nodes in document order with a dotted path of keys and indexes.
func walkScalars(n *model.Node, path string, fn func(string, *model.Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case model.NodeScalar:
		if path != "" && n.Value != "" {
			fn(path, n)
		}
	case model.NodeMapping:
		for _, c := range n.Children {
			walkScalars(c, join(path, c.Key), fn)
		}
	case model.NodeSequence:
		for i, c := range n.Children {
			walkScalars(c, join(path, strconv.Itoa(i)), fn)
		}
	}
}

func join(path, seg string) string {
	if path == "" {
		return seg
	}
	return path + "." + seg
}

func yamlPointer(path string) string { return "/" + strings.ReplaceAll(path, ".", "/") }
