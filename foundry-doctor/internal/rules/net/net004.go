package net

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net004 implements FND-NET-004: agent subnet delegation and sizing.
type net004 struct{ meta }

func newNet004() rules.Rule { return net004{meta{"FND-NET-004", 1}} }

const (
	agentDelegation = "Microsoft.App/environments"
	maxSubnetBytes  = 63
	maxPrefixLen    = 27
)

const net004Fix = `resource agentSubnet 'Microsoft.Network/virtualNetworks/subnets@2024-05-01' = {
  parent: vnet
  name: 'agent-subnet'
  properties: {
    addressPrefix: '10.0.0.0/24'
    delegations: [{ name: 'Microsoft.App.environments', properties: { serviceName: 'Microsoft.App/environments' } }]
  }
}`

// agentSubnet is the resolved agent subnet of one account.
type agentSubnet struct {
	account int
	subnet  int // resource index, -1 when unresolved
	state   resolution
}

// agentSubnets resolves the subnet of every non-managed agent network injection of every account.
func agentSubnets(r rules.Rule, t *topo, accts []int) ([]agentSubnet, []rules.Result) {
	var out []agentSubnet
	var skips []rules.Result
	for _, a := range accts {
		inj, ok := agentInjections(t.res[a])
		if !ok && len(inj) == 0 {
			skips = append(skips, unresolvedSkip(r, t.res[a], "properties.networkInjections"))
			continue
		}
		for _, in := range inj {
			switch {
			case !in.managedKnown:
				skips = append(skips, unresolvedSkip(r, t.res[a], "networkInjections.useMicrosoftManagedNetwork"))
			case in.managed:
				skips = append(skips, skipped(r, t.res[a], rules.ReasonNotApplicable, "managed-virtual-network-mode: useMicrosoftManagedNetwork is true", ""))
			case !in.subnetPresent:
				skips = append(skips, unresolvedSkip(r, t.res[a], "networkInjections.subnetArmId"))
			default:
				j, st := t.resolveID(a, in.subnetRaw, typeSubnet)
				out = append(out, agentSubnet{account: a, subnet: j, state: st})
			}
		}
	}
	return out, skips
}

// Evaluate checks the delegation, size, name length and exclusivity of each agent subnet.
func (r net004) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	accts, out := accounts(r, t)
	subs, skips := agentSubnets(r, t, accts)
	out = append(out, skips...)
	users := map[int][]int{}
	for _, s := range subs {
		if s.state == resolved {
			users[s.subnet] = append(users[s.subnet], s.account)
		}
	}
	done := map[int]bool{} // a shared subnet is reported once
	for _, s := range subs {
		if s.state != resolved {
			out = append(out, skipped(r, t.res[s.account], rules.ReasonUnresolvedValue("subnet-id"),
				"the agent subnet ID is a parameter, an expression or a subnet that is not a resource of the template", "resolved subnet"))
			continue
		}
		if done[s.subnet] {
			continue
		}
		done[s.subnet] = true
		out = append(out, r.subnet(t, s, users[s.subnet])...)
	}
	return out, nil
}

func (r net004) subnet(t *topo, s agentSubnet, sharedBy []int) []rules.Result {
	acct, sn := t.res[s.account], t.res[s.subnet]
	var out []rules.Result
	bad := func(key, ev string) {
		out = append(out, failure(r, sn, "/properties", key, text{ev,
			"Give each Foundry account a dedicated subnet delegated to Microsoft.App/environments, /27 or larger, with a name of 63 bytes or fewer.", net004Fix},
			sdk.ConfidenceCertain, acct))
	}
	// Delegation.
	switch n, st := arrayProp(sn, "properties.delegations"); {
	case st == model.Unresolved:
		out = append(out, unresolvedSkip(r, sn, "properties.delegations"))
	default:
		found, other := false, false
		for k := 0; k < n; k++ {
			svc, ss := strProp(sn, fmt.Sprintf("properties.delegations[%d].properties.serviceName", k))
			if ss == model.Unresolved {
				found = true // cannot tell: do not fail
				out = append(out, unresolvedSkip(r, sn, "delegation serviceName"))
				continue
			}
			if strings.EqualFold(svc, agentDelegation) {
				found = true
			} else {
				other = true
			}
		}
		switch {
		case found:
		case other:
			bad("delegation-service", fmt.Sprintf("Agent subnet %s of account %s is delegated to a service other than %s.", sn.Name, acct.Name, agentDelegation))
		default:
			bad("delegation-missing", fmt.Sprintf("Agent subnet %s of account %s has no delegation to %s.", sn.Name, acct.Name, agentDelegation))
		}
	}
	// Size.
	if tooSmall, known := prefixTooSmall(sn); !known {
		out = append(out, unresolvedSkip(r, sn, "subnet address prefix"))
	} else if tooSmall {
		bad("size", fmt.Sprintf("Agent subnet %s of account %s is smaller than /%d; the size cannot be changed after assignment.", sn.Name, acct.Name, maxPrefixLen))
	}
	// Name length.
	if name, ok := lastNameSegment(sn.Name); !ok {
		out = append(out, unresolvedSkip(r, sn, "subnet name"))
	} else if len(name) > maxSubnetBytes {
		bad("name-length", fmt.Sprintf("Agent subnet name is %d UTF-8 bytes; the limit for Container Apps-backed deployments is %d.", len(name), maxSubnetBytes))
	}
	// Exclusivity.
	if len(sharedBy) > 1 {
		bad("shared-subnet", fmt.Sprintf("Agent subnet %s is used by %d Foundry accounts; a subnet cannot be shared by more than one Foundry resource.", sn.Name, len(sharedBy)))
	}
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}

// prefixTooSmall reports whether the subnet address prefix, or every prefix of addressPrefixes, is
// longer than /27. known is false when no literal prefix can be read.
func prefixTooSmall(sn model.Resource) (tooSmall, known bool) {
	v := sn.Get("properties.addressPrefix")
	if v.State == model.Literal {
		s, ok := v.String()
		bits, ok2 := prefixBits(s)
		return ok && ok2 && bits > maxPrefixLen, ok && ok2
	}
	if v.State == model.Unresolved {
		return false, false
	}
	n, st := arrayProp(sn, "properties.addressPrefixes")
	if st != model.Literal || n == 0 {
		return false, false
	}
	all := true
	for k := 0; k < n; k++ {
		s, ps := strProp(sn, fmt.Sprintf("properties.addressPrefixes[%d]", k))
		bits, ok := prefixBits(s)
		if ps != model.Literal || !ok {
			return false, false
		}
		if bits <= maxPrefixLen {
			all = false
		}
	}
	return all, true
}

func prefixBits(s string) (int, bool) {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return 0, false
	}
	return p.Bits(), true
}
