package net

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net007 implements FND-NET-007: restricted mode has an allowed IP range.
type net007 struct{ meta }

func newNet007() rules.Rule { return net007{meta{"FND-NET-007", 1}} }

const net007Fix = `properties: {
  publicNetworkAccess: 'Enabled'
  networkAcls: {
    defaultAction: 'Deny'
    bypass: 'AzureServices'
    ipRules: [{ value: '203.0.113.0/24' }]
  }
}`

// Evaluate checks accounts with publicNetworkAccess Enabled and networkAcls.defaultAction Deny:
// at least one ipRules or virtualNetworkRules entry, and every ipRules value an IPv4 address or CIDR.
// The confidence is likely: no primary source states that Deny with empty rules blocks all callers.
func (r net007) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	accts, out := accounts(r, t)
	for _, i := range accts {
		out = append(out, r.account(t, i)...)
	}
	return out, nil
}

func (r net007) account(t *topo, i int) []rules.Result {
	acct := t.res[i]
	pna, pst := strProp(acct, "properties.publicNetworkAccess")
	acl, ast := strProp(acct, "properties.networkAcls.defaultAction")
	if pst == model.Unresolved || ast == model.Unresolved {
		return []rules.Result{unresolvedSkip(r, acct, "publicNetworkAccess or networkAcls.defaultAction")}
	}
	if pst != model.Literal || ast != model.Literal || !strings.EqualFold(pna, "Enabled") || !strings.EqualFold(acl, "Deny") {
		return []rules.Result{rules.Pass()} // not restricted mode
	}
	nIP, ipSt := arrayProp(acct, "properties.networkAcls.ipRules")
	nVN, vnSt := arrayProp(acct, "properties.networkAcls.virtualNetworkRules")
	if ipSt == model.Unresolved || vnSt == model.Unresolved {
		return []rules.Result{unresolvedSkip(r, acct, "networkAcls.ipRules or virtualNetworkRules")}
	}
	var out []rules.Result
	bad := 0
	for k := 0; k < nIP; k++ {
		v := acct.Get("properties.networkAcls.ipRules[" + strconv.Itoa(k) + "].value")
		if v.State == model.Unresolved {
			return []rules.Result{unresolvedSkip(r, acct, "networkAcls.ipRules value")}
		}
		if s, ok := v.String(); !ok || !ipv4OrCIDR(s) {
			bad++
		}
	}
	if bad > 0 {
		// The value itself is not echoed: a misplaced secret in a rule must not reach a report.
		ev := fmt.Sprintf("Account %s has %d networkAcls.ipRules entr(ies) whose value is not an IPv4 address or IPv4 CIDR range.", acct.Name, bad)
		out = append(out, failure(r, acct, "/properties/networkAcls/ipRules", "ip-rule-invalid", text{ev,
			"Write each ipRules value as an IPv4 address or CIDR range, for example 203.0.113.0/24.", net007Fix}, sdk.ConfidenceLikely))
	}
	if nIP+nVN == 0 {
		if r.hasPrivateEndpoint(t, i) {
			return append(out, skipped(r, acct, rules.ReasonNotApplicable, "private endpoint present: defaultAction Deny without IP rules is a private-only posture", ""))
		}
		if t.anyUnresolvedEndpoint() {
			return append(out, skipped(r, acct, rules.ReasonUnresolvedValue("private-endpoint-target"),
				"a private endpoint of the template has a target that cannot be resolved and might serve this account", "resolved private endpoint target"))
		}
		ev := fmt.Sprintf("Account %s is in restricted mode (publicNetworkAccess Enabled, defaultAction Deny) but has no ipRules and no virtualNetworkRules, so no public caller is allowed.", acct.Name)
		return append(out, failure(r, acct, "/properties/networkAcls", "no-allowed-networks", text{ev,
			"List the egress IPs of build agents and developer networks as ipRules (or subnets as virtualNetworkRules), or set publicNetworkAccess to Disabled and use a private endpoint.", net007Fix}, sdk.ConfidenceLikely))
	}
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}

func (net007) hasPrivateEndpoint(t *topo, i int) bool {
	for _, p := range t.endpointsFor(i) {
		if has, _ := t.pes[p].hasGroup("account"); has && !t.pes[p].manualOnly() {
			return true
		}
	}
	return false
}

func ipv4OrCIDR(s string) bool {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Is4()
	}
	p, err := netip.ParsePrefix(s)
	return err == nil && p.Addr().Is4()
}
