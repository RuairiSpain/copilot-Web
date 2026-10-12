package net

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net001 implements FND-NET-001: Foundry public access and private endpoint posture.
type net001 struct{ meta }

func newNet001() rules.Rule { return net001{meta{"FND-NET-001", 1}} }

const net001Fix = `properties: {
  publicNetworkAccess: 'Disabled'
  networkAcls: { defaultAction: 'Deny' }
}
// plus a Microsoft.Network/privateEndpoints with groupIds: ['account'] targeting the account`

// Evaluate checks each AI Services account.
//
// Finding "public-open": publicNetworkAccess Enabled or absent with networkAcls.defaultAction Allow or
// absent. Finding "no-private-endpoint": publicNetworkAccess Disabled and no private endpoint with group
// ID account targets the account. Restricted mode (Enabled with Deny) passes here and is NET-007's.
// policy.network.publicAccess entra-only or allowed should lower the severity of "public-open"; the
// result contract has no severity override, so the policy is named in the evidence only.
func (r net001) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	t, res, stop, err := run(ctx, r, in)
	if stop || err != nil {
		return res, err
	}
	accts, out := accounts(r, t)
	for _, i := range accts {
		out = append(out, r.account(t, i, in.Policy.EffectivePublicAccess())...)
	}
	return out, nil
}

func (r net001) account(t *topo, i int, policy model.PublicAccess) []rules.Result {
	acct := t.res[i]
	pna, pst := strProp(acct, "properties.publicNetworkAccess")
	if pst == model.Unresolved {
		return []rules.Result{unresolvedSkip(r, acct, "properties.publicNetworkAccess")}
	}
	switch {
	case pst == model.Absent || strings.EqualFold(pna, "Enabled"):
		acl, ast := strProp(acct, "properties.networkAcls.defaultAction")
		if ast == model.Unresolved {
			return []rules.Result{unresolvedSkip(r, acct, "properties.networkAcls.defaultAction")}
		}
		if ast == model.Absent || strings.EqualFold(acl, "Allow") {
			ev := fmt.Sprintf("Account %s accepts traffic from all public networks: publicNetworkAccess is %s and networkAcls.defaultAction is %s.%s Policy policy.network.publicAccess is %s.",
				acct.Name, shown(pst, pna, "absent (defaults to Enabled)"), shown(ast, acl, "absent (defaults to Allow)"), r.projects(t, i), policy)
			return []rules.Result{failure(r, acct, "/properties/publicNetworkAccess", "public-open", text{ev,
				"Set publicNetworkAccess to Disabled and add a private endpoint with group ID account, or use restricted mode (defaultAction Deny plus an allow list).", net001Fix},
				sdk.ConfidenceCertain)}
		}
		return []rules.Result{rules.Pass()}
	case strings.EqualFold(pna, "Disabled"):
		return r.endpoint(t, i)
	}
	return []rules.Result{unresolvedSkip(r, acct, "properties.publicNetworkAccess")}
}

func shown(st model.ValueState, v, absent string) string {
	if st == model.Absent {
		return absent
	}
	return v
}

// projects counts the Foundry projects declared under the account, for the evidence.
func (net001) projects(t *topo, i int) string {
	n := 0
	for _, j := range t.ofType(typeProject) {
		if t.parentOf(j, typeAccount) == i {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" %d project(s) in the template inherit this posture.", n)
}

func (r net001) endpoint(t *topo, i int) []rules.Result {
	acct := t.res[i]
	var matched, manual []int
	unknownGroups := false
	for _, p := range t.endpointsFor(i) {
		info := t.pes[p]
		has, known := info.hasGroup("account")
		if !known {
			unknownGroups = true
		}
		switch {
		case has && info.manualOnly():
			manual = append(manual, p)
		case has:
			matched = append(matched, p)
		}
	}
	live := skipped(r, acct, rules.ReasonControlPlaneUnavailable,
		"the approval state of the private endpoint connection is read from ARM; offline analysis cannot see it", "live Azure access")
	switch {
	case len(matched) > 0:
		deployed := false
		for _, p := range matched {
			if !t.res[p].Meta.MayNotDeploy {
				deployed = true
			}
		}
		if !deployed {
			ev := fmt.Sprintf("Account %s has public access disabled; its only private endpoints with group ID account (%s) are conditional and may not deploy.", acct.Name, t.res[matched[0]].Name)
			return []rules.Result{doubt(r, acct, "/properties/publicNetworkAccess", "private-endpoint-conditional", text{ev,
				"Deploy a private endpoint with group ID account unconditionally in private environments.", net001Fix}), live}
		}
		return []rules.Result{rules.Pass(), live}
	case len(manual) > 0:
		ev := fmt.Sprintf("Account %s has public access disabled; the private endpoint %s uses a manual connection that stays Pending until it is approved.", acct.Name, t.res[manual[0]].Name)
		return []rules.Result{doubt(r, acct, "/properties/publicNetworkAccess", "private-endpoint-pending-approval", text{ev,
			"Approve the private endpoint connection on the account, or use privateLinkServiceConnections (automatic approval) in the same subscription.", net001Fix}), live}
	case unknownGroups || t.anyUnresolvedEndpoint() || t.nodes[i] == "":
		return []rules.Result{skipped(r, acct, rules.ReasonUnresolvedValue("private-endpoint-target"),
			"a private endpoint in the template targets an ID or group list that cannot be resolved, or the endpoint is declared elsewhere", "resolved private endpoint target")}
	}
	ev := fmt.Sprintf("Account %s has publicNetworkAccess Disabled and no private endpoint with group ID account targets it in the analysed template, so no client can reach it.", acct.Name)
	return []rules.Result{failure(r, acct, "/properties/publicNetworkAccess", "no-private-endpoint", text{ev,
		"Add a private endpoint with group ID account in the VNet that hosts your clients and agents, with its private DNS zones (FND-NET-003).", net001Fix},
		sdk.ConfidenceCertain)}
}
