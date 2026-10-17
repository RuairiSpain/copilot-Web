package preflight

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
		// The Foundry domain-availability check (checkDomainAvailability) is
		// about properties.customSubDomainName, not the ARM resource name.
		name, what := r.Name, "name"
		if spec.kind == azure.NameFoundry {
			sub, present := r.Properties["customSubDomainName"].(string)
			switch {
			case !present || sub == "":
				a.skip(sdk.SkipInputUnavailable + ": " + r.Type + " " + r.Name + " sets no customSubDomainName, so there is no subdomain to check")
				continue
			case isExpr(sub):
				a.skip(sdk.SkipInputUnavailable + ": customSubDomainName of a " + r.Type + " is an unresolved expression")
				continue
			}
			name, what = sub, "customSubDomainName"
		}
		if isExpr(r.Name) {
			a.skip(sdk.SkipInputUnavailable + ": name of a " + r.Type + " is an unresolved expression")
			continue
		}
		if spec.noCheck != "" {
			a.skip(sdk.SkipInputUnavailable + ": " + spec.noCheck + " (" + r.Type + ")")
			continue
		}
		if !spec.re.MatchString(name) {
			a.add(tmplFinding(r, fmt.Sprintf("%s %q is invalid for %s (%s)", what, name, r.Type, spec.rule)))
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
		res, err := e.d.Names.CheckName(ctx, azure.NameCheck{Kind: spec.kind, Name: name, SubscriptionID: e.sub(), Location: e.regionOf(r)})
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
		a.add(tmplFinding(r, fmt.Sprintf("%s %q is not available (%s)", what, name, res.Reason)))
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

// incrementalNote is added to every what-if based result. The request uses
// deployment mode Incremental, in which what-if essentially never reports
// Delete, and ARM has no change type for a recreation caused by an
// immutable-property change, so a clean result is never a pass.
const incrementalNote = "Incremental mode cannot report deletions; recreation from immutable-property changes is not detected"

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
	deploy := 0
	var subLevel []string
	for _, c := range o.res.Changes {
		if c.ChangeType != azure.ChangeIgnore && c.ChangeType != azure.ChangeNoChange && c.ResourceID != "" {
			if e.rgScope() == "" {
				a.skip(sdk.SkipInputUnavailable + ": capability cross-resource-group scope policy cannot evaluate " + c.ResourceID + " because the target subscription or resource group is not set")
			} else if !e.scopeAllowed(c.ResourceID) {
				// A subscription-scope template legitimately creates things that
				// are not under the target resource group: the group itself,
				// subscription-level role assignments, policy and so on. With a
				// resource group set, a non-destructive change inside the target
				// subscription-level Create is therefore an uncertain note, not a
				// "blocked" finding. Only a Create of a subscription-level
				// resource or of a resource group itself is demoted; Modify,
				// Deploy, Delete, anything inside a different resource group
				// and anything outside the subscription keep the finding.
				if o.scope == scopeSubscription && c.ChangeType == azure.ChangeCreate && e.inSubscription(c.ResourceID) && subscriptionLevelID(c.ResourceID) {
					subLevel = append(subLevel, string(c.ChangeType)+" "+c.ResourceID)
					continue
				}
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
		case c.ChangeType == azure.ChangeDeploy:
			deploy++
		case c.ChangeType == azure.ChangeIgnore, c.ChangeType == azure.ChangeUnknown, c.ChangeType == azure.ChangeUnsupported:
			a.note("what-if reported %s for %s, which cannot be classified", c.ChangeType, c.ResourceID)
		}
	}
	if len(subLevel) > 0 {
		sort.Strings(subLevel)
		a.note("the subscription-scope template changes %d resource(s) outside the target resource group but inside the subscription (%s); this is expected for subscription-level resources and was not treated as a violation, so confirm it is intended or pass --approved-scope", len(subLevel), capNames(subLevel, 5))
	}
	if deploy > 0 {
		a.note("what-if reported Deploy (properties may or may not change) for %d resource(s); the ResourceIdOnly result format carries no detail", deploy)
	}
	// potentialChanges are changes ARM could not decide: uncertain evidence only.
	for _, c := range o.res.PotentialChanges {
		a.note("what-if reported a potential %s of %s (potentialChanges: uncertain, not asserted)", c.ChangeType, c.ResourceID)
	}
	if len(o.res.DiagnosticCodes) > 0 {
		codes := append([]string(nil), o.res.DiagnosticCodes...)
		sort.Strings(codes)
		a.note("what-if returned diagnostics: %s", strings.Join(codes, ","))
	}
	a.note("%s", incrementalNote)
	return a.result(), nil
}

// subscriptionLevelID reports whether id names a subscription-level resource
// (no resourceGroups segment, such as a subscription role assignment) or a
// resource group itself (/subscriptions/<id>/resourceGroups/<name>).
func subscriptionLevelID(id string) bool {
	parts := strings.Split(strings.Trim(strings.ToLower(id), "/"), "/")
	for i, p := range parts {
		if p == "resourcegroups" {
			return i == 2 && len(parts) == 4
		}
	}
	return true
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

// maxPolicyChecks bounds the checkPolicyRestrictions POSTs of one run; the
// remaining resources are reported as not checked (uncertain).
const maxPolicyChecks = 50

// policyContent builds the candidate resource content for the policy service
// from every template field that deny policies commonly target: name,
// properties, tags, sku, kind, identity and zones. The caller sanitises it.
func policyContent(r sdk.ARMResource) map[string]any {
	c := map[string]any{"name": r.Name, "properties": r.Properties}
	if len(r.Tags) > 0 {
		c["tags"] = r.Tags
	}
	if len(r.SKU) > 0 {
		c["sku"] = r.SKU
	}
	if r.Kind != "" {
		c["kind"] = r.Kind
	}
	if len(r.Identity) > 0 {
		c["identity"] = r.Identity
	}
	if len(r.Zones) > 0 {
		z := make([]any, len(r.Zones))
		for i, v := range r.Zones {
			z[i] = v
		}
		c["zones"] = z
	}
	return c
}

// DEP-009: policy restrictions.
//
// ADR-006 point 8: the resource content sent to checkPolicyRestrictions is
// built from non-secret template content only. Properties whose value is or
// contains an unresolved ARM expression, secret-looking properties and
// credential-shaped values are omitted (azure.SanitizePolicyContent) and
// reported, and a clean answer for partial content is uncertain, never a pass.
// A deny reported for a resource whose content was partial is a note, never a
// failing finding. At most maxPolicyChecks resources are posted.
func (e *env) dep009(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Policy == nil {
		return skipRes("", missingCap("Azure Policy restrictions", "Microsoft.PolicyInsights/checkPolicyRestrictions/read")), nil
	}
	if m := needTarget(true, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	var a acc
	checked, notChecked, posts := 0, 0, 0
	var omitted []string
	for _, r := range rs {
		if strings.EqualFold(namespaceOf(r.Type), "Microsoft.Authorization") || isExpr(r.Type) {
			continue
		}
		if r.APIVersion == "" || isExpr(r.APIVersion) {
			a.note("the API version of a %s resource is not a literal value, so its policy restrictions were not checked", r.Type)
			continue
		}
		loc := e.regionOf(r)
		if posts >= maxPolicyChecks {
			notChecked++
			continue
		}
		content, dropped := azure.SanitizePolicyContent(policyContent(r))
		for _, d := range dropped {
			omitted = append(omitted, r.Type+" "+d)
		}
		partial := len(dropped) > 0
		posts++
		rst, err := e.d.Policy.CheckRestrictions(ctx, azure.PolicyRestrictionRequest{
			Scope: e.rgScope(), ResourceType: r.Type, APIVersion: r.APIVersion, Location: loc,
			Content: content,
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
			if strings.EqualFold(x.Effect, "deny") || x.Kind == azure.RestrictionObservedDenial {
				if partial {
					// Never a failing finding on knowingly partial content.
					a.note("policy %s reported a possible denial (%s) of %s, but the content sent was partial (%s), so it is not asserted as a failure", x.AssignmentID, x.Kind, r.Type, capNames(dropped, 5))
					continue
				}
				a.add(tmplFinding(r, fmt.Sprintf("policy %s likely denies this resource (%s): %s", x.AssignmentID, x.Kind, oneLine(x.Message))))
			} else {
				a.note("policy %s has effect %s on %s (informational)", x.AssignmentID, x.Effect, r.Type)
			}
		}
	}
	if notChecked > 0 {
		a.note("policy restrictions were not checked for %d resource(s): at most %d checkPolicyRestrictions calls are made per run", notChecked, maxPolicyChecks)
	}
	if len(omitted) > 0 {
		sort.Strings(omitted)
		a.note("the policy check ran on partial content: unresolved-expression or secret-like properties were not sent (%s)", capNames(omitted, 5))
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

// plannedResource is a template resource with a computable ID in the target group.
type plannedResource struct {
	typ, id string
}

// plannedIDs computes the resource ID each template resource will have in the
// target resource group from its literal type and name ("<namespace>/<type>"
// with "name" or "parent/child" segments). unresolved counts resources whose ID
// cannot be computed (expression name or type, mismatched segments).
func plannedIDs(rg string, rs []sdk.ARMResource) (out []plannedResource, unresolved int) {
	seen := map[string]bool{}
	for _, r := range rs {
		if strings.EqualFold(r.Type, "Microsoft.Authorization/roleAssignments") || strings.EqualFold(r.Type, "Microsoft.Resources/deployments") ||
			strings.EqualFold(r.Type, "Microsoft.Resources/resourceGroups") {
			continue
		}
		if isExpr(r.Name) || isExpr(r.Type) || r.Name == "" || r.Scope != "" {
			unresolved++
			continue
		}
		ns, rest, ok := strings.Cut(r.Type, "/")
		types, names := strings.Split(rest, "/"), strings.Split(r.Name, "/")
		if !ok || len(types) != len(names) {
			unresolved++
			continue
		}
		id := rg + "/providers/" + ns
		for i := range types {
			id += "/" + types[i] + "/" + names[i]
		}
		if !seen[strings.ToLower(id)] {
			seen[strings.ToLower(id)] = true
			out = append(out, plannedResource{typ: r.Type, id: id})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, unresolved
}

// maxResourceLockLookups bounds the resource-level lock reads of one run.
const maxResourceLockLookups = 50

// DEP-010: management locks.
//
// A lock list returns locks at AND BELOW its scope, so a lock is relevant only
// when its scope is the target resource group, one of its ancestors, or a
// planned resource (or an ancestor of one). Locks on unrelated resources or
// groups are ignored. Resource-level locks are read per planned resource
// (ManagementLocks_ListAtResourceLevel) so that a lock on a resource does not
// depend on how the group listing treats resources below it.
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
	rg := e.rgScope()
	planned, unresolved := plannedIDs(rg, rs)
	if unresolved > 0 {
		a.note("%d template resource(s) have an unresolved name or type, so locks on them were not evaluated", unresolved)
	}
	nested := 0
	for _, r := range rs {
		if strings.EqualFold(r.Type, "Microsoft.Resources/deployments") {
			nested++
		}
	}
	if nested > 0 {
		a.note("%d nested Microsoft.Resources/deployments resource(s) are not expanded, so locks on the resources they deploy were not evaluated", nested)
	}
	var locks []azure.Lock
	seen := map[string]bool{}
	add := func(ls []azure.Lock) {
		for _, l := range ls {
			if k := strings.ToLower(l.ID); !seen[k] {
				seen[k] = true
				locks = append(locks, l)
			}
		}
	}
	for _, scope := range []string{e.subScope(), rg} {
		ls, err := e.d.Inventory.ListLocks(ctx, scope)
		if err != nil {
			if ctx.Err() != nil {
				return sdk.Result{}, ctx.Err()
			}
			a.skipErr(err)
			continue
		}
		add(ls)
	}
	if e.d.Extras == nil {
		a.note("resource-level lock reads are unavailable; locks on individual resources come from the resource group listing only")
	} else {
		n := len(planned)
		if n > maxResourceLockLookups {
			a.note("only the first %d of %d planned resources were checked for resource-level locks", maxResourceLockLookups, n)
			n = maxResourceLockLookups
		}
		for _, p := range planned[:n] {
			ls, err := e.d.Extras.ListResourceLocks(ctx, p.id)
			if err != nil {
				if ctx.Err() != nil {
					return sdk.Result{}, ctx.Err()
				}
				a.skipErr(err)
				continue
			}
			add(ls)
		}
	}
	relevant := func(l azure.Lock) bool {
		if azure.LockCovers(l.Scope, rg) {
			return true
		}
		for _, p := range planned {
			if azure.LockCovers(l.Scope, p.id) {
				return true
			}
		}
		return false
	}
	var cnd []azure.Lock
	for _, l := range locks {
		if l.Scope == "" {
			a.note("lock %s has no readable scope, so it was not evaluated", l.Name)
			continue
		}
		switch {
		case strings.EqualFold(l.Level, "ReadOnly"):
			if relevant(l) {
				a.add(idFinding("Microsoft.Authorization/locks", l.ID, fmt.Sprintf("ReadOnly lock %s on %s blocks writes", l.Name, l.Scope)))
			}
		case strings.EqualFold(l.Level, "CanNotDelete"):
			cnd = append(cnd, l)
		}
	}
	// A CanNotDelete lock matters only for a resource ARM predicts to delete.
	hasCnd := false
	for _, l := range cnd {
		if relevantToRG(l, rg) {
			hasCnd = true
		}
	}
	if hasCnd {
		o := e.whatIf(ctx, in)
		if o.skip == "" {
			for _, c := range o.res.Changes {
				if !c.Destructive() {
					continue
				}
				for _, l := range cnd {
					if azure.LockCovers(l.Scope, c.ResourceID) {
						a.add(idFinding(typeFromID(c.ResourceID), c.ResourceID, fmt.Sprintf("what-if predicts %s of %s, covered by CanNotDelete lock %s", c.ChangeType, c.ResourceID, l.Name)))
						break
					}
				}
			}
			// Incremental-mode what-if cannot report deletions or the recreation of a
			// resource, so a lock that blocks deletes is not proven irrelevant.
			a.note("a CanNotDelete lock exists and %s", incrementalNote)
		} else {
			a.note("a CanNotDelete lock exists but deletions could not be predicted without what-if")
		}
	}
	var group []azure.Lock
	for _, l := range cnd {
		if relevantToRG(l, rg) {
			group = append(group, l)
		}
	}
	e.historyNearLimit(ctx, in, &a, group)
	if ctx.Err() != nil {
		return sdk.Result{}, ctx.Err()
	}
	return a.result(), nil
}

// relevantToRG reports whether a lock is at, above or below the resource group
// (everything the group and subscription listings return and that can matter
// to a delete inside the group).
func relevantToRG(l azure.Lock, rg string) bool {
	return azure.LockCovers(l.Scope, rg) || azure.LockCovers(rg, l.Scope)
}

// historyMargin is a Foundry Doctor product decision (not a documented
// value): a history within this many deployments of the documented limit of
// 800 counts as "near" the limit.
const historyMargin = 80

// historyNearLimit implements FND-DEP-010 case (c): a CanNotDelete lock on the
// resource group stops automatic deletion of deployment history, and at 800
// deployments new deployments fail. A CanNotDelete lock on the subscription is
// inherited by the group (Learn lock-resources.md: a lock applied at a parent
// scope is inherited by all resources within it), so any lock whose scope
// covers the group counts, not only one exactly at the group. A lock on a
// resource inside the group does not.
func (e *env) historyNearLimit(ctx context.Context, in *sdk.Input, a *acc, cnd []azure.Lock) {
	rg := strings.TrimRight(e.rgScope(), "/")
	var lock *azure.Lock
	for i := range cnd {
		if azure.LockCovers(cnd[i].Scope, rg) {
			// Prefer the lock exactly at the group when both exist.
			if lock == nil || strings.EqualFold(strings.TrimRight(cnd[i].Scope, "/"), rg) {
				lock = &cnd[i]
			}
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
		a.add(idFinding("Microsoft.Authorization/locks", lock.ID, fmt.Sprintf("CanNotDelete lock %s (scope %s, covering the resource group) prevents deployment-history cleanup and the history holds%s %d of %d deployments; new deployments fail at %d", lock.Name, lock.Scope, qual, c.Count, limit, limit)))
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
			switch {
			case p.Bits() > 27:
				fail(fmt.Sprintf("subnet prefix %s is smaller than /27", s))
			case p.Bits() >= 25:
				// FND-DEP-011 (b): the portal step requires /27 or larger, the
				// limitations section recommends /24; /25-/27 is a warning only.
				a.note("warning: subnet prefix %s is smaller than the recommended /24 (the minimum documented is /27)", s)
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
		a.skip(missingCap("subnet serviceAssociationLinks and resourceNavigationLinks read", "Microsoft.Network/virtualNetworks/subnets/serviceAssociationLinks/read and Microsoft.Network/virtualNetworks/subnets/resourceNavigationLinks/read"))
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

// checkVNetLocation compares the location of the subnet's virtual network with
// the Foundry account (FND-DEP-011 (f)) using one VirtualNetworks_Get, never a
// list of every network in the subscription.
func (e *env) checkVNetLocation(ctx context.Context, a *acc, acct sdk.ARMResource, sn azure.Subnet, fail func(string)) {
	want := normLoc(e.regionOf(acct))
	if want == "" || sn.VNetID == "" {
		a.note("virtual network location could not be compared with the Foundry location")
		return
	}
	if e.d.Extras == nil {
		a.skip(missingCap("virtual network read (VirtualNetworks_Get)", "Microsoft.Network/virtualNetworks/read"))
		return
	}
	v, err := e.d.Extras.GetVNet(ctx, sn.VNetID)
	if err != nil {
		var ae *azure.APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			a.note("virtual network %s was not found", lastSegment(sn.VNetID))
			return
		}
		a.skipErr(err)
		return
	}
	if normLoc(v.Location) != want {
		fail(fmt.Sprintf("virtual network is in %s but the Foundry account is in %s", v.Location, e.regionOf(acct)))
	}
}

func normLoc(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }
