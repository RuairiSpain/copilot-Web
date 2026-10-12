package net

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net008 implements FND-NET-008: a Search service that must be private is not on the Free tier.
type net008 struct{ meta }

func newNet008() rules.Rule { return net008{meta{"FND-NET-008", 1}} }

const net008Fix = `resource search 'Microsoft.Search/searchServices@2025-05-01' = {
  name: searchName
  location: location
  sku: { name: 'standard' } // 'free' does not support private endpoints
  properties: { publicNetworkAccess: 'Disabled' }
}`

// Evaluate fails a search service with sku.name free (case-insensitive) when a private endpoint with
// group ID searchService targets it, publicNetworkAccess is Disabled, or a private Foundry account uses
// it as its vector store. Other tiers, including S1 and serverless, are never evaluated: the
// documented tier limits are informational and the S1 documentation conflicts.
func (r net008) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	private := r.privateVectorStores(t)
	var out []rules.Result
	for _, i := range t.ofType(typeSearch) {
		out = append(out, r.service(t, i, private[i])...)
	}
	return out, nil
}

// privateVectorStores maps search service index to the private account that uses it as vector store.
func (net008) privateVectorStores(t *topo) map[int]int {
	m := map[int]int{}
	accts, _ := accounts(net008{}, t)
	for _, a := range accts {
		if private, known := isPrivate(t.res[a]); !known || !private {
			continue
		}
		for _, d := range t.dependents(a) {
			if d.kind == "search" && d.state == resolved {
				if _, seen := m[d.res]; !seen {
					m[d.res] = a + 1
				}
			}
		}
	}
	return m
}

func (r net008) service(t *topo, i, viaAccount int) []rules.Result {
	svc := t.res[i]
	sku, st := strProp(svc, "sku.name")
	if st == model.Unresolved {
		return []rules.Result{unresolvedSkip(r, svc, "sku.name")}
	}
	if st != model.Literal || !strings.EqualFold(sku, "free") {
		return []rules.Result{rules.Pass()}
	}
	var out []rules.Result
	add := func(key, why string) {
		ev := fmt.Sprintf("Search service %s uses the Free tier (sku.name %s), which does not support private endpoints; %s.", svc.Name, sku, why)
		out = append(out, failure(r, svc, "/sku/name", key, text{ev,
			"Use the Basic tier or higher for any search service that must be reached through a private endpoint.", net008Fix}, sdk.ConfidenceCertain))
	}
	for _, p := range t.endpointsFor(i) {
		if has, _ := t.pes[p].hasGroup("searchService"); has {
			add("free-with-private-endpoint", "the private endpoint "+t.res[p].Name+" targets it")
			break
		}
	}
	if pna, ps := strProp(svc, "properties.publicNetworkAccess"); ps == model.Literal && strings.EqualFold(pna, "Disabled") {
		add("free-public-disabled", "publicNetworkAccess is Disabled")
	}
	if viaAccount > 0 {
		add("free-vector-store-of-private-account", "it is the vector store of the private account "+t.res[viaAccount-1].Name)
	}
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}
