package preflight

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type nameSpec struct {
	kind    azure.NameKind
	re      *regexp.Regexp
	rule    string
	noCheck string // non-empty: no availability API exists; skip with this reason
}

var nameSpecs = map[string]nameSpec{
	"microsoft.keyvault/vaults":              {kind: azure.NameKeyVault, re: regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{1,22}[a-zA-Z0-9]$`), rule: "3-24 characters, letters, digits and hyphens, starting with a letter"},
	"microsoft.storage/storageaccounts":      {kind: azure.NameStorage, re: regexp.MustCompile(`^[a-z0-9]{3,24}$`), rule: "3-24 lowercase letters and digits"},
	"microsoft.containerregistry/registries": {kind: azure.NameACR, re: regexp.MustCompile(`^[a-z0-9]{5,50}$`), rule: "5-50 lowercase letters and digits"},
	"microsoft.apimanagement/service":        {kind: azure.NameAPIM, re: regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]{0,49}$`), rule: "1-50 characters, letters, digits and hyphens, starting with a letter"},
	"microsoft.search/searchservices":        {kind: azure.NameSearch, re: regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,58}[a-z0-9]$`), rule: "2-60 lowercase letters, digits and hyphens"},
	"microsoft.cognitiveservices/accounts":   {kind: azure.NameFoundry, re: regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,62}[a-zA-Z0-9]$`), rule: "2-64 characters, letters, digits and hyphens"},
	"microsoft.documentdb/databaseaccounts":  {noCheck: "no Cosmos DB name-availability check is implemented"},
}

// DEP-007: name availability.
func (e *env) dep007(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	var cands []sdk.ARMResource
	for _, r := range rs {
		if _, ok := nameSpecs[strings.ToLower(r.Type)]; ok {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return sdk.Result{}, nil
	}
	var a acc
	for _, r := range cands {
		spec := nameSpecs[strings.ToLower(r.Type)]
		if isExpr(r.Name) {
			a.skip(sdk.SkipInputUnavailable + ": name of a " + r.Type + " is an unresolved expression")
			continue
		}
		if spec.noCheck != "" {
			a.skip(sdk.SkipInputUnavailable + ": " + spec.noCheck + " (" + r.Type + ")")
			continue
		}
		if !spec.re.MatchString(r.Name) {
			a.add(tmplFinding(r, fmt.Sprintf("name %q is invalid for %s (%s)", r.Name, r.Type, spec.rule)))
			continue
		}
		if e.d.Names == nil {
			a.skip(missingCap("name availability checks", "Reader on the subscription"))
			continue
		}
		if m := needTarget(false, e.d.Target); m != "" {
			a.skip(m)
			continue
		}
		if spec.kind == azure.NameFoundry && e.regionOf(r) == "" {
			a.skip(sdk.SkipInputUnavailable + ": location for the Foundry name check is not set (use --location)")
			continue
		}
		res, err := e.d.Names.CheckName(ctx, azure.NameCheck{Kind: spec.kind, Name: r.Name, SubscriptionID: e.sub(), Location: e.regionOf(r)})
		if err != nil {
			if ctx.Err() != nil {
				return sdk.Result{}, ctx.Err()
			}
			a.skipErr(err)
			continue
		}
		if res.Available {
			continue
		}
		if e.isExisting(ctx, r) {
			continue // a redeploy of our own resource is not a conflict
		}
		a.add(tmplFinding(r, fmt.Sprintf("name %q is not available (%s)", r.Name, res.Reason)))
	}
	return a.result(), nil
}

// isExisting reports whether a resource of the template's type and name already
// exists in the target resource group (best effort).
func (e *env) isExisting(ctx context.Context, r sdk.ARMResource) bool {
	if e.d.Inventory == nil || e.rgScope() == "" {
		return false
	}
	l, err := e.listExisting(ctx, r.Type)
	if err != nil {
		return false
	}
	for _, x := range l.Resources {
		if strings.EqualFold(lastSegment(x.Name), lastSegment(r.Name)) {
			return true
		}
	}
	return false
}

// DEP-008: what-if destructive changes.
func (e *env) dep008(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	o := e.whatIf(ctx, in)
	if o.err != nil && ctx.Err() != nil {
		return sdk.Result{}, ctx.Err()
	}
	if o.skip != "" {
		return skipRes("", o.skip), nil
	}
	allowed := map[string]bool{}
	for _, id := range e.d.AllowedDeletes {
		allowed[strings.ToLower(id)] = true
	}
	var a acc
	for _, c := range o.res.Changes {
		if c.ChangeType != azure.ChangeIgnore && c.ChangeType != azure.ChangeNoChange && c.ResourceID != "" {
			if e.rgScope() == "" {
				a.skip(sdk.SkipInputUnavailable + ": capability cross-resource-group scope policy cannot evaluate " + c.ResourceID + " because the target subscription or resource group is not set")
			} else if !e.scopeAllowed(c.ResourceID) {
				a.add(idFinding(typeFromID(c.ResourceID), c.ResourceID,
					fmt.Sprintf("what-if predicts %s of %s outside the target resource group %s and not in an approved scope; pass --approved-scope to allow it", c.ChangeType, c.ResourceID, e.rgScope())))
				continue
			}
		}
		switch {
		case c.Destructive():
			if allowed[strings.ToLower(c.ResourceID)] {
				continue
			}
			a.add(idFinding(typeFromID(c.ResourceID), c.ResourceID, fmt.Sprintf("what-if predicts %s of %s", c.ChangeType, c.ResourceID)))
		case c.ChangeType == azure.ChangeIgnore, c.ChangeType == azure.ChangeUnknown, c.ChangeType == azure.ChangeDeploy:
			a.note("what-if reported %s for %s, which cannot be classified", c.ChangeType, c.ResourceID)
		}
	}
	if len(o.res.DiagnosticCodes) > 0 {
		codes := append([]string(nil), o.res.DiagnosticCodes...)
		sort.Strings(codes)
		a.note("what-if returned diagnostics: %s", strings.Join(codes, ","))
	}
	return a.result(), nil
}

func typeFromID(id string) string {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i < len(parts); i++ {
		if strings.EqualFold(parts[i], "providers") && i+2 < len(parts) {
			t := parts[i+1]
			for j := i + 2; j < len(parts); j += 2 {
				t += "/" + parts[j]
			}
			return t
		}
	}
	return "Microsoft.Resources/resourceGroups"
}

// DEP-009: policy restrictions.
func (e *env) dep009(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Policy == nil {
		return skipRes("", missingCap("Azure Policy restrictions", "Microsoft.PolicyInsights/policyStates/queryResults/action (Reader)")), nil
	}
	if m := needTarget(true, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	var a acc
	checked := 0
	for _, r := range rs {
		if strings.EqualFold(namespaceOf(r.Type), "Microsoft.Authorization") || isExpr(r.Type) {
			continue
		}
		loc := e.regionOf(r)
		rst, err := e.d.Policy.CheckRestrictions(ctx, azure.PolicyRestrictionRequest{
			Scope: e.rgScope(), ResourceType: r.Type, APIVersion: r.APIVersion, Location: loc,
			Content: map[string]any{"name": r.Name, "location": loc, "properties": r.Properties},
		})
		if err != nil {
			if ctx.Err() != nil {
				return sdk.Result{}, ctx.Err()
			}
			if _, un := azure.AsUnavailable(err); un {
				a.skipErr(err)
				continue
			}
			return sdk.Result{}, err
		}
		checked++
		for _, x := range rst {
			if strings.EqualFold(x.Effect, "deny") {
				a.add(tmplFinding(r, fmt.Sprintf("policy %s likely denies this resource (%s): %s", x.AssignmentID, x.Kind, oneLine(x.Message))))
			} else {
				a.note("policy %s has effect %s on %s (informational)", x.AssignmentID, x.Effect, r.Type)
			}
		}
	}
	if len(a.findings) == 0 && len(a.skips) == 0 && checked > 0 {
		a.note("no restriction was reported, but policy evaluation cannot prove the deployment will not be denied")
	}
	if checked == 0 && len(a.skips) == 0 && len(a.findings) == 0 {
		as, err := e.d.Policy.ListAssignments(ctx, e.rgScope())
		if err != nil {
			return sdk.Result{}, err
		}
		a.note("%d policy assignment(s) apply to the resource group; their effect on this template was not evaluated", len(as))
	}
	return a.result(), nil
}

// DEP-010: management locks.
func (e *env) dep010(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Inventory == nil {
		return skipRes("", missingCap("management lock listing", "Microsoft.Authorization/locks/read")), nil
	}
	if m := needTarget(true, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	var a acc
	scopes := []string{e.subScope(), e.rgScope()}
	seen := map[string]bool{}
	for _, r := range rs {
		if isExpr(r.Name) || isExpr(r.Type) || strings.EqualFold(r.Type, typeDeployment) {
			continue
		}
		l, err := e.listExisting(ctx, r.Type)
		if err != nil {
			a.skipErr(err)
			continue
		}
		for _, x := range l.Resources {
			if strings.EqualFold(lastSegment(x.Name), lastSegment(r.Name)) && !seen[strings.ToLower(x.ID)] {
				seen[strings.ToLower(x.ID)] = true
				scopes = append(scopes, x.ID)
			}
		}
	}
	var locks []azure.Lock
	for _, s := range scopes {
		ls, err := e.d.Inventory.ListLocks(ctx, s)
		if err != nil {
			if ctx.Err() != nil {
				return sdk.Result{}, ctx.Err()
			}
			a.skipErr(err)
			continue
		}
		locks = append(locks, ls...)
	}
	ro := map[string]bool{}
	for _, l := range locks {
		if strings.EqualFold(l.Level, "ReadOnly") && !ro[strings.ToLower(l.ID)] {
			ro[strings.ToLower(l.ID)] = true
			a.add(idFinding("Microsoft.Authorization/locks", l.ID, fmt.Sprintf("ReadOnly lock %s on %s blocks writes", l.Name, l.Scope)))
		}
	}
	var cnd []azure.Lock
	for _, l := range locks {
		if strings.EqualFold(l.Level, "CanNotDelete") {
			cnd = append(cnd, l)
		}
	}
	if len(cnd) > 0 {
		o := e.whatIf(ctx, in)
		if o.skip == "" {
			for _, c := range o.res.Changes {
				if !c.Destructive() {
					continue
				}
				for _, l := range cnd {
					if strings.HasPrefix(strings.ToLower(c.ResourceID)+"/", strings.ToLower(strings.TrimRight(l.Scope, "/"))+"/") {
						a.add(idFinding(typeFromID(c.ResourceID), c.ResourceID, fmt.Sprintf("what-if predicts %s of %s, covered by CanNotDelete lock %s", c.ChangeType, c.ResourceID, l.Name)))
						break
					}
				}
			}
		} else {
			a.note("a CanNotDelete lock exists but deletions could not be predicted without what-if")
		}
	}
	e.historyNearLimit(ctx, in, &a, cnd)
	if ctx.Err() != nil {
		return sdk.Result{}, ctx.Err()
	}
	return a.result(), nil
}

// historyMargin is a Foundry Doctor product decision (not a documented
// value): a history within this many deployments of the documented limit of
// 800 counts as "near" the limit.
const historyMargin = 80

// historyNearLimit implements FND-DEP-010 case (c): a CanNotDelete lock on the
// resource group stops automatic deletion of deployment history, and at 800
// deployments new deployments fail.
func (e *env) historyNearLimit(ctx context.Context, in *sdk.Input, a *acc, cnd []azure.Lock) {
	rg := strings.ToLower(strings.TrimRight(e.rgScope(), "/"))
	var lock *azure.Lock
	for i := range cnd {
		if strings.ToLower(strings.TrimRight(cnd[i].Scope, "/")) == rg {
			lock = &cnd[i]
			break
		}
	}
	if lock == nil {
		return
	}
	if e.d.Deployments == nil {
		a.skip(missingCap("deployment history count (Deployments_ListByResourceGroup)", "Microsoft.Resources/deployments/read"))
		return
	}
	c, err := e.d.Deployments.CountDeployments(ctx, e.sub(), e.d.Target.ResourceGroup)
	if err != nil {
		if ctx.Err() == nil {
			a.skipErr(err)
		}
		return
	}
	limit := azure.DeploymentHistoryLimit
	margin := policyInt(in, "preflight.deploymentHistoryMargin", historyMargin)
	switch {
	case c.Count >= limit-margin:
		qual := ""
		if c.Truncated {
			qual = " (at least)"
		}
		a.add(idFinding("Microsoft.Authorization/locks", lock.ID, fmt.Sprintf("CanNotDelete lock %s on the resource group prevents deployment-history cleanup and the history holds%s %d of %d deployments; new deployments fail at %d", lock.Name, qual, c.Count, limit, limit)))
	case c.Truncated:
		a.note("deployment history count was truncated below the configured deployment-history margin")
	}
}

var reservedRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"169.254.0.0/16", "172.30.0.0/16", "172.31.0.0/16", "192.0.2.0/24", "0.0.0.0/8", "127.0.0.0/8", "100.100.0.0/17", "100.100.192.0/19", "100.100.224.0/19"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

func isPrivate(p netip.Prefix) bool {
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if netip.MustParsePrefix(s).Contains(p.Addr()) {
			return true
		}
	}
	return false
}

// DEP-011: agent subnet.
func (e *env) dep011(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	type target struct {
		acct sdk.ARMResource
		id   string
	}
	var ts []target
	var a acc
	for _, r := range byType(rs, typeAccount) {
		for _, inj := range injections(r) {
			id := getStr(inj, "subnetArmId")
			if id == "" || isExpr(id) {
				a.skip(sdk.SkipInputUnavailable + ": agent subnet ID of " + r.Name + " is not a literal value")
				continue
			}
			ts = append(ts, target{r, id})
		}
	}
	if len(ts) == 0 && len(a.skips) == 0 {
		return sdk.Result{}, nil
	}
	if len(ts) > 0 && e.d.Inventory == nil {
		return skipRes("", missingCap("subnet read", "Microsoft.Network/virtualNetworks/subnets/read")), nil
	}
	for _, t := range ts {
		if e.rgScope() == "" {
			a.skip(sdk.SkipInputUnavailable + ": capability cross-resource-group scope policy cannot evaluate subnet " + t.id + " because the target subscription or resource group is not set")
			continue
		}
		if !e.scopeAllowed(t.id) {
			a.add(idFinding("Microsoft.Network/virtualNetworks/subnets", t.id,
				"subnet "+t.id+" is outside the target resource group "+e.rgScope()+" and is not in an approved scope; pass --approved-scope to allow it"))
			continue
		}
		sn, err := e.d.Inventory.GetSubnet(ctx, t.id)
		if err != nil {
			if ctx.Err() != nil {
				return sdk.Result{}, ctx.Err()
			}
			a.skipErr(err)
			continue
		}
		if len(sn.Name) == 0 {
			sn.Name = lastSegment(t.id)
		}
		fail := func(msg string) { a.add(idFinding("Microsoft.Network/virtualNetworks/subnets", t.id, msg)) }
		del := false
		for _, d := range sn.DelegationServices {
			if strings.EqualFold(d, "Microsoft.App/environments") {
				del = true
			}
		}
		if !del {
			fail("subnet is not delegated to Microsoft.App/environments")
		}
		if len(sn.Name) > 63 {
			fail("subnet name exceeds 63 bytes")
		}
		for _, s := range sn.AddressPrefixes {
			p, err := netip.ParsePrefix(s)
			if err != nil || !p.Addr().Is4() {
				a.note("address prefix %q could not be evaluated", s)
				continue
			}
			p = p.Masked()
			if p.Bits() > 27 {
				fail(fmt.Sprintf("subnet prefix %s is smaller than /27", s))
			}
			if netip.MustParsePrefix("100.64.0.0/10").Contains(p.Addr()) {
				a.skip(sdk.SkipInputUnavailable + ": 100.64.0.0/10 is not evaluated because the documentation conflicts")
				continue
			}
			for _, rr := range reservedRanges() {
				if rr.Overlaps(p) {
					fail(fmt.Sprintf("subnet prefix %s overlaps reserved range %s", s, rr))
				}
			}
			if !isPrivate(p) {
				fail(fmt.Sprintf("subnet prefix %s is outside RFC1918 address space", s))
			}
		}
		e.checkVNetLocation(ctx, &a, t.acct, sn, fail)
		if err := e.checkSubnetLinks(ctx, &a, t.acct, t.id, fail); err != nil {
			return sdk.Result{}, err
		}
	}
	return a.result(), nil
}

// checkSubnetLinks implements FND-DEP-011 case (d): the subnet must not carry
// a service association link or resource navigation link owned by another
// resource. The result is "likely": the links are the documented exclusivity
// evidence but the format is not documented as the marker.
func (e *env) checkSubnetLinks(ctx context.Context, a *acc, acct sdk.ARMResource, subnetID string, fail func(string)) error {
	if e.d.SubnetLinks == nil {
		a.skip(missingCap("subnet serviceAssociationLinks and resourceNavigationLinks read", "Microsoft.Network/virtualNetworks/subnets/read"))
		return nil
	}
	ls, err := e.d.SubnetLinks.SubnetLinks(ctx, subnetID)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		a.skipErr(err)
		return nil
	}
	own := ""
	if !isExpr(acct.Name) && acct.Name != "" && e.rgScope() != "" {
		own = strings.ToLower(strings.TrimRight(e.rgScope(), "/") + "/providers/" + typeAccount + "/" + acct.Name)
	}
	unresolvedOwner := own == ""
	check := func(kind string, links []azure.NetworkLink) {
		if unresolvedOwner && len(links) > 0 {
			a.note("subnet link ownership could not be resolved for account %q because its resource ID is not a literal value", acct.Name)
			return
		}
		for _, l := range links {
			if own != "" && strings.HasPrefix(strings.ToLower(l.Link)+"/", own+"/") {
				continue
			}
			fail(fmt.Sprintf("subnet is already bound by %s %s (%s, linked to %s); a subnet cannot be shared with another service (likely, not certain)", kind, l.Name, l.LinkedResourceType, l.Link))
		}
	}
	check("service association link", ls.ServiceAssociationLinks)
	check("resource navigation link", ls.ResourceNavigationLinks)
	return nil
}

func (e *env) checkVNetLocation(ctx context.Context, a *acc, acct sdk.ARMResource, sn azure.Subnet, fail func(string)) {
	want := normLoc(e.regionOf(acct))
	if want == "" || sn.VNetID == "" {
		a.note("virtual network location could not be compared with the Foundry location")
		return
	}
	parts := strings.Split(strings.Trim(sn.VNetID, "/"), "/")
	if len(parts) < 2 {
		a.note("virtual network ID is malformed")
		return
	}
	l, err := e.d.Inventory.ListResources(ctx, azure.InventoryQuery{Scope: "/subscriptions/" + parts[1], Types: []string{"Microsoft.Network/virtualNetworks"}})
	if err != nil {
		a.skipErr(err)
		return
	}
	for _, v := range l.Resources {
		if strings.EqualFold(v.ID, sn.VNetID) {
			if normLoc(v.Location) != want {
				fail(fmt.Sprintf("virtual network is in %s but the Foundry account is in %s", v.Location, e.regionOf(acct)))
			}
			return
		}
	}
	a.note("virtual network %s was not found in the inventory", lastSegment(sn.VNetID))
}

func normLoc(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }
