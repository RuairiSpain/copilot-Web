package sec

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

// rule006 is FND-SEC-006: IP firewall rules on Foundry-related resources contain no open ranges, and
// (Storage and Cosmos DB only) no private-network ranges.
type rule006 struct{}

func (rule006) ID() string   { return "FND-SEC-006" }
func (rule006) Version() int { return 1 }

func (r rule006) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	return run(ctx, r, in)
}

const (
	rec006 = "Remove open or private ranges from IP rules. Use private endpoints, virtual network rules or service endpoints for private traffic, and list only the specific public egress addresses that need access."
	fix006 = "networkAcls: {\n  defaultAction: 'Deny'\n  ipRules: [\n    { value: '203.0.113.10' } // specific public address, not 0.0.0.0/0 or 10.0.0.0/8\n  ]\n}"
)

// ipRuleSite says where a resource type keeps its IP rules.
type ipRuleSite struct {
	typ     string
	array   string // path of the ipRules array
	field   string // property of an item holding the address or range
	private bool   // private ranges are a documented platform constraint for this type
	cosmos  bool   // a bare 0.0.0.0 means all Azure data centers
	kind    *targetKind
	foundry bool // scope is "account hosts projects or deployments"
}

var (
	keyVaultKind = targetKind{typ: typKeyVault}

	ipRuleSites = []ipRuleSite{
		{typ: typStorage, array: "properties.networkAcls.ipRules", field: "value", private: true, kind: &storageKind},
		{typ: typCosmos, array: "properties.ipRules", field: "ipAddressOrRange", private: true, cosmos: true, kind: &cosmosKind},
		{typ: typKeyVault, array: "properties.networkAcls.ipRules", field: "value", kind: &keyVaultKind},
		{typ: typSearch, array: "properties.networkRuleSet.ipRules", field: "value", kind: &searchKind},
		{typ: typAccount, array: "properties.networkAcls.ipRules", field: "value", foundry: true},
	}

	// privateRanges are the ranges Storage and Cosmos DB document as not enforceable in IP rules.
	privateRanges = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("100.64.0.0/10"),
	}
)

func (r rule006) evaluateARM(in *model.Input) []rules.Result {
	var out []rules.Result
	ix := buildIndex(in.ARM)
	accts := in.ARM.ResourcesOfType(typAccount)
	for _, site := range ipRuleSites {
		for _, res := range in.ARM.ResourcesOfType(site.typ) {
			out = append(out, r.checkResource(in, ix, accts, site, res)...)
		}
	}
	return out
}

func (r rule006) checkResource(in *model.Input, ix *foundryIndex, accts []model.Resource, site ipRuleSite, res model.Resource) []rules.Result {
	arr := res.Get(site.array)
	if arr.State == model.Absent {
		return nil // no IP rules, nothing to check
	}
	var d doubt
	var sc scopeResult
	if site.foundry {
		if notDeployed(res) {
			return []rules.Result{notApplicable(r, res, "the resource condition is false, so it is not deployed")}
		}
		owned, orphans := 0, 0
		for _, c := range append(in.ARM.ResourcesOfType(typProject), in.ARM.ResourcesOfType(typDeployment)...) {
			switch {
			case isChildOf(c, res):
				owned++
			case !hasKnownParent(c, accts):
				orphans++
			}
		}
		if owned == 0 && orphans == 0 {
			return []rules.Result{notApplicable(r, res, "out-of-scope: the account owns no projects or deployments in this template")}
		}
		d = assess(res)
		if owned == 0 && !d.uncertain {
			d = doubt{uncertain: true, conf: sdk.ConfidenceUncertain, why: "a project or deployment in the template could not be tied to a parent account"}
		}
	} else {
		var skipped *rules.Result
		sc, d, skipped = scoped(r, ix, res, *site.kind)
		if skipped != nil {
			return []rules.Result{*skipped}
		}
	}
	if arr.State == model.Unresolved {
		return []rules.Result{unresolvedSkip(r, res, site.array, arr)}
	}
	items, _ := arr.Lit.([]any)
	if len(items) == 0 {
		return nil
	}
	tail := ""
	if !site.foundry {
		tail = "; " + scopeText(sc)
	}
	var out []rules.Result
	flagged, skippedAny, privateUnchecked := false, false, false
	for i := range items {
		path := site.array + "[" + strconv.Itoa(i) + "]." + site.field
		v := res.Get(path)
		if v.State == model.Unresolved {
			out = append(out, unresolvedSkip(r, res, fmt.Sprintf("%s[%d]", site.array, i), v))
			skippedAny = true
			continue
		}
		s, ok := v.String()
		p, perr := parseRange(s)
		if !ok || perr != nil {
			out = append(out, skip(r, res, rules.ReasonUnresolvedValue("unparseable-ip-rule"),
				fmt.Sprintf("%s[%d] is not a literal IPv4/IPv6 address or CIDR range", site.array, i),
				"a literal IP address or CIDR range"))
			skippedAny = true
			continue
		}
		norm := p.String()
		switch {
		case isOpen(p, s, site.cosmos):
			flagged = true
			out = append(out, finding(r, res, d, "open-range:"+site.array+":"+norm, path,
				fmt.Sprintf("%s contains the open range %s (%s)%s", site.array, norm, openMeaning(p, site.cosmos), tail),
				rec006, fix006))
		case site.private && isPrivate(p):
			flagged = true
			out = append(out, finding(r, res, d, "private-range:"+site.array+":"+norm, path,
				fmt.Sprintf("%s contains the private-network range %s, which cannot be enforced in IP rules%s", site.array, norm, tail),
				rec006, fix006))
		case !site.private && isPrivate(p):
			privateUnchecked = true
		}
	}
	if privateUnchecked {
		out = append(out, notApplicable(r, res, "private-range check is not evaluated for "+res.Type+": no verified source states that private ranges are rejected there"))
	}
	if !flagged && !skippedAny {
		out = append(out, rules.Pass())
	}
	return out
}

// parseRange reads an IPv4 or IPv6 address or CIDR range.
func parseRange(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

var zeroAddr = netip.MustParseAddr("0.0.0.0")

// isOpen reports 0.0.0.0/0 for every type and a bare 0.0.0.0 for Cosmos DB.
func isOpen(p netip.Prefix, raw string, cosmos bool) bool {
	if p.Addr() == zeroAddr && p.Bits() == 0 {
		return true
	}
	return cosmos && p.Addr() == zeroAddr && !strings.Contains(raw, "/")
}

func openMeaning(p netip.Prefix, cosmos bool) string {
	if p.Bits() == 0 {
		return "all addresses"
	}
	if cosmos {
		return "0.0.0.0 allows all Azure data centers"
	}
	return "open range"
}

// isPrivate reports a range that lies inside one of the documented private ranges.
func isPrivate(p netip.Prefix) bool {
	for _, pr := range privateRanges {
		if pr.Contains(p.Addr()) && p.Bits() >= pr.Bits() {
			return true
		}
	}
	return false
}
