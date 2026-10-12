package net

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// net002 implements FND-NET-002: dependent services are private when the Foundry account is private.
type net002 struct{ meta }

func newNet002() rules.Rule { return net002{meta{"FND-NET-002", 1}} }

// dependent is a data resource an account's agents use.
type dependent struct {
	kind  string // storage, cosmos or search
	via   string // capabilitySettings or capability-host-connection
	res   int    // resource index when state is resolved
	state resolution
}

// depKinds maps the capability role to the dependent kind, the ARM type and the required group ID.
var depKinds = map[string]struct{ typ, group string }{
	"storage": {typeStorage, "blob"},
	"cosmos":  {typeCosmos, "Sql"},
	"search":  {typeSearch, "searchService"},
}

// capabilitySettings keys (preview 2026-07-15-preview) and capability host connection arrays (stable).
var (
	capabilityKeys = []struct{ key, kind string }{{"blobStore", "storage"}, {"documentStore", "cosmos"}, {"vectorStore", "search"}}
	connArrays     = []struct{ key, kind string }{{"storageConnections", "storage"}, {"threadStorageConnections", "cosmos"}, {"vectorStoreConnections", "search"}}
)

// belongsTo reports whether resource j is the account or sits under it (a project, or a child of one).
// With a single account in the template every project-level resource is taken to belong to it.
func (t *topo) belongsTo(j, acct int) bool {
	if j == acct || t.parentOf(j, typeAccount) == acct {
		return true
	}
	for _, p := range t.ofType(typeProject) {
		if t.parentOf(j, typeProject) == p && t.parentOf(p, typeAccount) == acct {
			return true
		}
	}
	return len(t.ofType(typeAccount)) == 1
}

// dependents resolves the Storage, Cosmos DB and Search resources of an account from capabilitySettings
// on the account and its projects, and from capability host connection names resolved through connections.
func (t *topo) dependents(acct int) []dependent {
	var out []dependent
	owners := []int{acct}
	for _, p := range t.ofType(typeProject) {
		if t.belongsTo(p, acct) {
			owners = append(owners, p)
		}
	}
	for _, o := range owners {
		for _, ck := range capabilityKeys {
			raw := rawAt(t.res[o].Body, "properties", "capabilitySettings", ck.key)
			if raw == nil {
				continue
			}
			j, st := t.resolveID(o, raw, depKinds[ck.kind].typ)
			out = append(out, dependent{kind: ck.kind, via: "capabilitySettings." + ck.key, res: j, state: st})
		}
	}
	var conns []int
	for _, typ := range []string{typeConn, typeProjConn} {
		for _, c := range t.ofType(typ) {
			if t.belongsTo(c, acct) {
				conns = append(conns, c)
			}
		}
	}
	for _, typ := range []string{typeCapHost, typeProjCapH} {
		for _, h := range t.ofType(typ) {
			if !t.belongsTo(h, acct) {
				continue
			}
			for _, ca := range connArrays {
				arr, _ := rawAt(t.res[h].Body, "properties", ca.key).([]any)
				for _, n := range arr {
					name, _ := n.(string)
					out = append(out, t.viaConnection(conns, name, ca.kind))
				}
			}
		}
	}
	return out
}

func (t *topo) viaConnection(conns []int, name, kind string) dependent {
	d := dependent{kind: kind, via: "capability host connection", res: -1, state: unresolved}
	if name == "" || model.IsARMExpression(name) {
		return d
	}
	for _, c := range conns {
		if last, ok := lastNameSegment(t.res[c].Name); ok && last == name {
			raw := rawAt(t.res[c].Body, "properties", "metadata", "ResourceId")
			d.res, d.state = t.resolveID(c, raw, depKinds[kind].typ)
			return d
		}
	}
	return d
}

// Evaluate checks the dependents of every private account.
func (r net002) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
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

func (r net002) account(t *topo, i int) []rules.Result {
	acct := t.res[i]
	inj, _ := agentInjections(acct)
	for _, in := range inj {
		if in.managedKnown && in.managed {
			return []rules.Result{skipped(r, acct, rules.ReasonNotApplicable, "managed-virtual-network-mode: Microsoft manages the network", "")}
		}
	}
	private, known := isPrivate(acct)
	if !known {
		return []rules.Result{unresolvedSkip(r, acct, "account network posture")}
	}
	if !private {
		return []rules.Result{skipped(r, acct, rules.ReasonNotApplicable, "the Foundry account allows public access; dependents are checked for private accounts only", "")}
	}
	deps := t.dependents(i)
	if len(deps) == 0 {
		return []rules.Result{skipped(r, acct, rules.ReasonNotApplicable, "no dependent Storage, Cosmos DB or Search resource is declared (platform-managed data resources)", "")}
	}
	var out []rules.Result
	for _, d := range deps {
		out = append(out, r.dependent(t, acct, d)...)
	}
	return out
}

const net002Fix = `// on each dependent: publicNetworkAccess: 'Disabled'
// plus a Microsoft.Network/privateEndpoints with the group ID blob (Storage), Sql (Cosmos DB) or searchService (Search)`

func (r net002) dependent(t *topo, acct model.Resource, d dependent) []rules.Result {
	if d.state != resolved {
		why := "dependent-resource-id"
		detail := d.kind + " dependent (" + d.via + ") has an ID that is a parameter or expression that cannot be resolved"
		if d.state == external {
			why, detail = "dependent-not-in-template", d.kind+" dependent ("+d.via+") is not declared in the template"
		}
		return []rules.Result{skipped(r, acct, rules.ReasonUnresolvedValue(why), detail, "resolved template value")}
	}
	dep := t.res[d.res]
	var out []rules.Result
	open, known := r.publicOpen(dep, d.kind)
	if !known {
		return []rules.Result{unresolvedSkip(r, dep, "public access settings")}
	}
	if open {
		ev := fmt.Sprintf("Private account %s uses %s %s (via %s), which still accepts public network traffic.", acct.Name, d.kind, dep.Name, d.via)
		out = append(out, failure(r, dep, "/properties", d.kind+":public-access", text{ev,
			"Disable public network access on the dependent (or default-deny its firewall) and give it a private endpoint with its private DNS zone (FND-NET-003).", net002Fix},
			sdk.ConfidenceCertain, acct))
	}
	group := depKinds[d.kind].group
	hasGroup, other := false, false
	for _, p := range t.endpointsFor(d.res) {
		if has, _ := t.pes[p].hasGroup(group); has {
			hasGroup = true
		} else {
			other = true
		}
	}
	switch {
	case hasGroup:
	case other:
		ev := fmt.Sprintf("Private account %s uses %s %s, but no private endpoint targeting it has the group ID %s.", acct.Name, d.kind, dep.Name, group)
		out = append(out, failure(r, dep, "", d.kind+":private-endpoint-group-id", text{ev,
			"Use the group ID " + group + " on the private endpoint of the " + d.kind + " resource.", net002Fix}, sdk.ConfidenceCertain, acct))
	case t.anyUnresolvedEndpoint():
		out = append(out, skipped(r, dep, rules.ReasonUnresolvedValue("private-endpoint-target"),
			"a private endpoint of the template has a target that cannot be resolved and might serve this dependent", "resolved private endpoint target"))
	default:
		ev := fmt.Sprintf("Private account %s uses %s %s, which has no private endpoint in the analysed template.", acct.Name, d.kind, dep.Name)
		out = append(out, failure(r, dep, "", d.kind+":no-private-endpoint", text{ev,
			"Add a private endpoint with the group ID " + group + " for the " + d.kind + " resource.", net002Fix}, sdk.ConfidenceCertain, acct))
	}
	if len(out) == 0 {
		out = append(out, rules.Pass())
	}
	return out
}

// publicOpen reports whether the dependent accepts public traffic. known is false when a deciding value
// cannot be read.
func (net002) publicOpen(r model.Resource, kind string) (open, known bool) {
	pna, pst := strProp(r, "properties.publicNetworkAccess")
	if pst == model.Unresolved {
		return false, false
	}
	if pst == model.Literal && strings.EqualFold(pna, "Disabled") {
		return false, true
	}
	switch kind {
	case "storage":
		acl, st := strProp(r, "properties.networkAcls.defaultAction")
		if st == model.Unresolved {
			return false, false
		}
		return !(st == model.Literal && strings.EqualFold(acl, "Deny")), true
	case "cosmos":
		f := r.Get("properties.isVirtualNetworkFilterEnabled")
		if f.State == model.Unresolved {
			return false, false
		}
		on, _ := f.Bool()
		nIP, s1 := arrayProp(r, "properties.ipRules")
		nVN, s2 := arrayProp(r, "properties.virtualNetworkRules")
		if s1 == model.Unresolved || s2 == model.Unresolved {
			return false, false
		}
		return !(on && nIP+nVN > 0), true
	}
	return true, true
}
