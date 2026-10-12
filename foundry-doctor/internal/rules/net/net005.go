package net

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net005 implements FND-NET-005: private-mode VNet and region consistency.
type net005 struct{ meta }

func newNet005() rules.Rule { return net005{meta{"FND-NET-005", 1}} }

const net005Fix = `param location string = 'swedencentral'
// create the VNet, the agent subnet and the Foundry account with location: location, and each private
// endpoint in the region of the VNet that hosts its subnet`

// place is a resource location: a normalised region, or an expression key that is equal only to itself.
type place struct {
	region string
	expr   string
	known  bool
}

func placeOf(r model.Resource) place {
	switch s, st := strProp(r, "location"); st {
	case model.Literal:
		return place{region: normaliseRegion(s), known: true}
	case model.Unresolved:
		if raw, ok := r.Body["location"].(string); ok {
			return place{expr: strings.Join(r.Meta.ModuleChain, "/") + "|" + raw, known: true}
		}
	}
	return place{}
}

// sameRegion compares two places. same is meaningful only when ok is true.
func sameRegion(a, b place) (same, ok bool) {
	switch {
	case !a.known || !b.known:
		return false, false
	case a.region != "" && b.region != "":
		return a.region == b.region, true
	case a.expr != "" && b.expr != "":
		return a.expr == b.expr, a.expr == b.expr // different expressions: cannot tell
	}
	return false, false
}

// Evaluate compares the account with the VNet of its agent subnet, and every private endpoint with the
// VNet of its subnet. Only the documented same-region constraint is encoded; the subscription of an
// endpoint is not part of the compiled resource and is not compared. Regions of Storage, Cosmos DB and
// Search relative to the account are informational and produce no result: the contract cannot carry
// a per-finding info severity.
func (r net005) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	accts, out := accounts(r, t)
	subs, skips := agentSubnets(r, t, accts)
	out = append(out, skips...)
	for _, s := range subs {
		acct := t.res[s.account]
		if s.state != resolved {
			out = append(out, skipped(r, acct, rules.ReasonUnresolvedValue("subnet-id"),
				"the agent subnet ID cannot be resolved to a VNet of the template", "resolved subnet"))
			continue
		}
		vnet := t.parentOf(s.subnet, typeVNet)
		if vnet < 0 {
			out = append(out, skipped(r, acct, rules.ReasonUnresolvedValue("vnet"), "the VNet that hosts the agent subnet cannot be identified", "resolved VNet"))
			continue
		}
		out = append(out, r.compare(acct, t.res[vnet], "account-vnet-region",
			"The Foundry account %s and the VNet %s that hosts its agent subnet are in different regions.", "/location"))
	}
	for _, p := range t.ofType(typePE) {
		out = append(out, r.endpoint(t, p)...)
	}
	return out, nil
}

func (r net005) endpoint(t *topo, p int) []rules.Result {
	pe := t.res[p]
	subnets := t.pes[p].subnets
	if len(subnets) != 1 {
		return []rules.Result{skipped(r, pe, rules.ReasonUnresolvedValue("subnet-id"),
			"the endpoint subnet is a parameter, an expression or a subnet that is not a resource of the template", "resolved subnet")}
	}
	vnet := t.parentOf(subnets[0], typeVNet)
	if vnet < 0 {
		return []rules.Result{skipped(r, pe, rules.ReasonUnresolvedValue("vnet"), "the VNet that hosts the endpoint subnet cannot be identified", "resolved VNet")}
	}
	return []rules.Result{r.compare(pe, t.res[vnet], "private-endpoint-region",
		"The private endpoint %s is in a different region than the VNet %s that hosts its subnet.", "/location")}
}

func (r net005) compare(a, vnet model.Resource, key, format, prop string) rules.Result {
	same, ok := sameRegion(placeOf(a), placeOf(vnet))
	switch {
	case !ok:
		return skipped(r, a, rules.ReasonUnresolvedValue("location"), "a location is a parameter or expression that cannot be compared", "resolved template value")
	case same:
		return rules.Pass()
	}
	return failure(r, a, prop, key, text{fmt.Sprintf(format, a.Name, vnet.Name),
		"Create the VNet, agent subnet and Foundry account in one region, and each private endpoint in the region of its VNet.", net005Fix},
		sdk.ConfidenceCertain, vnet)
}
