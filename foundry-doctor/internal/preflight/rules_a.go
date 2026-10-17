package preflight

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	typeDeployment = "Microsoft.CognitiveServices/accounts/deployments"
	typeAccount    = "Microsoft.CognitiveServices/accounts"
)

// networkSecuredNamespaces are registered when the agent subnet is network injected.
var networkSecuredNamespaces = []string{
	"Microsoft.KeyVault", "Microsoft.CognitiveServices", "Microsoft.Storage",
	"Microsoft.MachineLearningServices", "Microsoft.Search", "Microsoft.Network",
	"Microsoft.App", "Microsoft.ContainerService",
}

// DEP-001: subscription and tenant context.
func (e *env) dep001(ctx context.Context, _ *sdk.Input) (sdk.Result, error) {
	if e.d.Context == nil {
		return skipRes("", missingCap("azure context (sign-in and subscription read)", "Microsoft.Resources/subscriptions/read")), nil
	}
	var a acc
	c, err := e.d.Context.Context(ctx, azure.ContextRequest{TenantID: e.d.Target.TenantID, SubscriptionID: e.sub()})
	if err != nil {
		return sdk.Result{}, err
	}
	id := "/subscriptions/" + c.SubscriptionID
	if e.sub() != "" && c.SubscriptionID != "" && !strings.EqualFold(c.SubscriptionID, e.sub()) {
		a.add(idFinding("Microsoft.Resources/subscriptions", id, fmt.Sprintf("signed-in subscription %s differs from the requested subscription %s", c.SubscriptionID, e.sub())))
	}
	if c.SubscriptionState != "" && !strings.EqualFold(c.SubscriptionState, "Enabled") {
		a.add(idFinding("Microsoft.Resources/subscriptions", id, "subscription state is "+c.SubscriptionState+", not Enabled"))
	}
	switch {
	case e.d.Target.TenantID == "":
		a.note("expected tenant (AZURE_TENANT_ID) is not set, so tenant alignment was not proven")
	case c.SubscriptionTenantID == "":
		a.note("the subscription's owning tenant was not reported")
	case !strings.EqualFold(c.SubscriptionTenantID, e.d.Target.TenantID):
		a.add(idFinding("Microsoft.Resources/subscriptions", id, fmt.Sprintf("subscription belongs to tenant %s, expected %s", c.SubscriptionTenantID, e.d.Target.TenantID)))
	}
	return a.result(), nil
}

// DEP-002: resource provider registration (rules/catalog/dep/FND-DEP-002.yaml).
//
// ARM registers the providers of resources the template DEFINES by itself, so a
// template-defined namespace that is not registered is not a DEP-002 finding
// (DEP-003 reports whether the deployer may register it). The required set is
// (a) supporting providers that the template references without defining them
// and (b) the network-secured Agent Service list when a Foundry account
// declares an agent network injection. Part (a) cannot be derived from the
// compiled resource list: an `existing` resource is not a deployed resource and
// is compiled into an expression the model does not expose, so (a) is empty
// here (UNVERIFIED completeness; recorded in the report and the tests). A
// provider in the Registering state is not blocked by the catalogue, but it is
// not proven registered either, so it is reported as uncertain, never a pass.
func (e *env) dep002(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Context == nil {
		return skipRes("", missingCap("Microsoft.Resources/providers read", "Microsoft.Resources/subscriptions/providers/read")), nil
	}
	if m := needTarget(false, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	injected := false
	for _, r := range rs {
		if strings.EqualFold(r.Type, typeAccount) && len(injections(r)) > 0 {
			injected = true
		}
	}
	if !injected {
		return sdk.Result{}, nil
	}
	names := append([]string(nil), networkSecuredNamespaces...)
	sort.Strings(names)
	states, err := e.d.Context.ProviderStates(ctx, e.sub(), names)
	if err != nil {
		return sdk.Result{}, err
	}
	got := map[string]string{}
	for _, s := range states {
		got[strings.ToLower(s.Namespace)] = s.State
	}
	var a acc
	for _, n := range names {
		st, ok := got[strings.ToLower(n)]
		switch {
		case !ok:
			a.note("registration state of %s was not returned", n)
		case strings.EqualFold(st, "Registered"):
		case strings.EqualFold(st, "Registering"):
			a.note("resource provider %s is Registering: not blocked, but not yet proven Registered", n)
		default:
			a.add(idFinding("Microsoft.Resources/providers", e.subScope()+"/providers/"+n, fmt.Sprintf("resource provider %s is %s; the network-secured Agent Service setup needs it registered (this tool never registers providers)", n, st)))
		}
	}
	return a.result(), nil
}

// DEP-003: effective write permissions at the deployment scope.
//
// Resource-group templates are checked with the Permissions API at the target
// resource group (the evidence the catalogue prefers). A subscription-scope
// template is checked at the subscription: the Permissions API has no
// subscription variant, so the role-assignment evaluation
// (Permissions.EffectiveActions) is the only evidence there and it is
// non-conclusive. Management-group and tenant scopes are skipped.
func (e *env) dep003(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Permissions == nil && e.d.Evidence == nil {
		return skipRes("", missingCap("Microsoft.Authorization/permissions read", "Microsoft.Authorization/permissions/read")), nil
	}
	var a acc
	ds, why := e.deploymentScope(in)
	switch ds {
	case scopeManagementGroup, scopeTenant:
		return skipRes("", sdk.SkipInputUnavailable+": deployer permissions for a "+ds.String()+"-scope template are not evaluated (resource-group and subscription scope only)"), nil
	case scopeUnknown:
		a.note("the deployment scope is unknown (%s), so the resource group scope is assumed", why)
		ds = scopeResourceGroup
	}
	subScoped := ds == scopeSubscription
	if m := needTarget(!subScoped, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	actions := map[string]bool{"Microsoft.Resources/deployments/write": true}
	if e.d.WhatIfOptIn {
		actions["Microsoft.Resources/deployments/whatIf/action"] = true
	}
	for _, r := range rs {
		if strings.EqualFold(r.Type, "Microsoft.Authorization/roleAssignments") {
			actions["Microsoft.Authorization/roleAssignments/write"] = true
			continue
		}
		actions[r.Type+"/write"] = true
	}
	// <provider>/register/action is required for each template namespace
	// whose registration state (DEP-002 evidence) is not Registered.
	registerFor := e.unregisteredNamespaces(ctx, rs, &a)
	for _, ns := range registerFor {
		actions[ns+"/register/action"] = true
	}
	var list []string
	for act := range actions {
		list = append(list, act)
	}
	sort.Strings(list)
	scope, label := e.rgScope(), "the resource group"
	if subScoped {
		scope, label = e.subScope(), "the subscription"
	}
	var decisions []azure.ActionDecision
	var err error
	switch {
	case subScoped:
		if e.d.Permissions == nil {
			return skipRes("", missingCap("Microsoft.Authorization/roleAssignments read at subscription scope (the Permissions API has no subscription-scope variant)", "Microsoft.Authorization/roleAssignments/read")), nil
		}
		decisions, err = e.d.Permissions.EffectiveActions(ctx, azure.PermissionRequest{Scope: scope, Actions: list})
		a.note("a subscription-scope deployment cannot use the Permissions API; permissions were derived from role assignments, which is non-conclusive")
	case e.d.Evidence != nil:
		// Primary evidence per FND-DEP-003: GET .../providers/Microsoft.Authorization/permissions.
		decisions, err = e.d.Evidence.CheckReportedActions(ctx, azure.PermissionRequest{Scope: scope, Actions: list})
	default:
		decisions, err = e.d.Permissions.EffectiveActions(ctx, azure.PermissionRequest{Scope: scope, Actions: list})
		a.note("the Microsoft.Authorization/permissions read capability is unavailable; permissions were derived from role assignments instead")
	}
	if err != nil {
		return sdk.Result{}, err
	}
	for _, d := range decisions {
		switch d.Decision {
		case azure.DecisionDenied:
			why := d.Reason
			if why == "" {
				why = azure.ReasonNoGrantingRole
			}
			a.add(idFinding("Microsoft.Authorization/permissions", scope, fmt.Sprintf("action %s is not granted at %s (%s); deployment is likely to be denied", d.Action, label, why)))
		case azure.DecisionUnknown:
			a.note("could not evaluate %s (%s)", d.Action, d.Reason)
		}
	}
	if len(decisions) < len(list) {
		a.note("permission decisions were returned for %d of %d required actions", len(decisions), len(list))
	}
	a.note("permissions are reported evidence, not proof: deny assignments, ABAC conditions, locks, policy and PIM activation are not reflected")
	return a.result(), nil
}

// unregisteredNamespaces returns the template namespaces whose registration
// state is known and not Registered. Failures to read the state are recorded
// as notes (the register/action requirement then cannot be evaluated).
func (e *env) unregisteredNamespaces(ctx context.Context, rs []sdk.ARMResource, a *acc) []string {
	seen := map[string]string{}
	for _, r := range rs {
		ns := namespaceOf(r.Type)
		if ns != "" && !strings.EqualFold(ns, "Microsoft.Resources") {
			seen[strings.ToLower(ns)] = ns
		}
	}
	if len(seen) == 0 {
		return nil
	}
	var names []string
	for _, n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	if e.d.Context == nil {
		a.note("provider registration state is unavailable, so <provider>/register/action was not evaluated")
		return nil
	}
	states, err := e.d.Context.ProviderStates(ctx, e.sub(), names)
	if err != nil {
		a.note("provider registration state could not be read, so <provider>/register/action was not evaluated (%s)", oneLine(azureReason(err)))
		return nil
	}
	got := map[string]string{}
	for _, s := range states {
		got[strings.ToLower(s.Namespace)] = s.State
	}
	var out []string
	for _, n := range names {
		st, ok := got[strings.ToLower(n)]
		switch {
		case !ok:
			a.note("registration state of %s was not returned, so %s/register/action was not evaluated", n, n)
		case strings.EqualFold(st, "Registered"):
		default:
			out = append(out, n)
		}
	}
	return out
}

// DEP-004: model and SKU availability.
func (e *env) dep004(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	deps := byType(rs, typeDeployment)
	if len(deps) == 0 {
		return sdk.Result{}, nil
	}
	if e.d.Models == nil {
		return skipRes("", missingCap("Microsoft.CognitiveServices/locations/models read", "Microsoft.CognitiveServices/locations/models/read")), nil
	}
	if m := needTarget(false, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	var a acc
	for _, r := range deps {
		model := getMap(r.Properties, "model")
		name, format, version := getStr(model, "name"), getStr(model, "format"), getStr(model, "version")
		if name == "" || isExpr(name) || isExpr(format) {
			a.note("model name for %s is not a literal value", r.Name)
			continue
		}
		loc := e.regionOf(r)
		if loc == "" || isExpr(loc) {
			a.note("location for %s is unknown (use --location)", r.Name)
			continue
		}
		list, err := e.modelsFor(ctx, loc, format, name)
		if err != nil {
			return sdk.Result{}, err
		}
		var match *azure.ModelAvailability
		for i := range list {
			m := &list[i]
			if strings.EqualFold(m.Name, name) && (format == "" || strings.EqualFold(m.Format, format)) &&
				(version == "" || isExpr(version) || strings.EqualFold(m.Version, version)) {
				match = m
				break
			}
		}
		if match == nil {
			a.add(tmplFinding(r, fmt.Sprintf("model %s %s is not offered in %s", name, version, loc)))
			continue
		}
		switch {
		case strings.EqualFold(match.LifecycleStatus, "Deprecated"):
			a.add(tmplFinding(r, fmt.Sprintf("model %s %s is Deprecated in %s", name, match.Version, loc)))
			continue
		case strings.EqualFold(match.LifecycleStatus, "Deprecating"), strings.EqualFold(match.LifecycleStatus, "Legacy"):
			a.note("model %s %s lifecycle is %s", name, match.Version, match.LifecycleStatus)
		}
		if match.DeprecationDate != "" {
			if t, ok := parseDate(match.DeprecationDate); ok && !t.After(e.d.Now()) {
				a.add(tmplFinding(r, fmt.Sprintf("model %s %s deprecation date %s has passed", name, match.Version, match.DeprecationDate)))
				continue
			}
		}
		if isExpr(r.SKUName) || r.SKUName == "" {
			a.note("SKU for %s is not a literal value", r.Name)
			continue
		}
		found := false
		for _, s := range match.SKUs {
			if strings.EqualFold(s.Name, r.SKUName) {
				found = true
			}
		}
		if !found {
			a.add(tmplFinding(r, fmt.Sprintf("SKU %s is not offered for model %s %s in %s", r.SKUName, name, match.Version, loc)))
		}
	}
	return a.result(), nil
}

func parseDate(s string) (time.Time, bool) {
	for _, l := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// capacityOf reads a literal requested capacity from the resource.
func capacityOf(r sdk.ARMResource) (float64, bool) {
	if r.SKU != nil {
		if f, ok := r.SKU["capacity"].(float64); ok {
			return f, true
		}
	}
	if sku := getMap(r.Properties, "sku"); sku != nil {
		if f, ok := sku["capacity"].(float64); ok {
			return f, true
		}
	}
	if f, ok := r.Properties["capacity"].(float64); ok {
		return f, true
	}
	return 0, false
}

// splitDeploymentName splits the compiled name of an account deployment
// ("<account>/<deployment>") when both parts are literal.
func splitDeploymentName(name string) (account, deployment string, ok bool) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || isExpr(parts[0]) || isExpr(parts[1]) || isExpr(name) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// existingDeployments lists the deployments of an account once per run
// (Foundry.ListAccountDeployments, GET .../accounts/{a}/deployments). An account
// that does not exist yet has none. Any other failure is returned.
func (e *env) existingDeployments(ctx context.Context, account string) ([]azure.AccountDeployment, error) {
	key := strings.ToLower(account)
	e.mu.Lock()
	c, ok := e.acctDeps[key]
	e.mu.Unlock()
	if ok {
		return c.list, c.err
	}
	l, err := e.d.Foundry.ListAccountDeployments(ctx, e.sub(), e.d.Target.ResourceGroup, account)
	var ae *azure.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		l, err = nil, nil
	}
	e.mu.Lock()
	e.acctDeps[key] = acctDepsEntry{l, err}
	e.mu.Unlock()
	return l, err
}

// DEP-005: quota headroom.
//
// The requested capacity of each planned deployment is summed per region and
// quota line (usage name) and compared with limit minus current value. A
// planned deployment that already exists with the same account, name and SKU is
// an update, so its current capacity is added back before the comparison. The
// capacity unit is taken as the usages API reports it; a line without a unit is
// uncertain. When the offset cannot be computed (no Foundry capability, an
// unreadable account, a name that is not literal) an apparent shortfall is
// uncertain, not a finding.
func (e *env) dep005(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	deps := byType(rs, typeDeployment)
	if len(deps) == 0 {
		return sdk.Result{}, nil
	}
	if e.d.Models == nil {
		return skipRes("", missingCap("Microsoft.CognitiveServices/locations/usages read", "Microsoft.CognitiveServices/locations/usages/read")), nil
	}
	if m := needTarget(false, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	type planned struct {
		r        sdk.ARMResource
		capacity float64
		sku      string
	}
	type group struct {
		loc, usage string
		items      []planned
	}
	var a acc
	groups := map[string]*group{}
	for _, r := range deps {
		capy, ok := capacityOf(r)
		if !ok {
			a.note("requested capacity for %s is not available in the compiled model", r.Name)
			continue
		}
		model := getMap(r.Properties, "model")
		name, format := getStr(model, "name"), getStr(model, "format")
		loc := e.regionOf(r)
		if name == "" || isExpr(name) || loc == "" || isExpr(loc) || isExpr(r.SKUName) || r.SKUName == "" {
			a.note("model, SKU or location for %s is not a literal value", r.Name)
			continue
		}
		list, err := e.modelsFor(ctx, loc, format, name)
		if err != nil {
			return sdk.Result{}, err
		}
		usage := ""
		for _, m := range list {
			for _, s := range m.SKUs {
				if strings.EqualFold(s.Name, r.SKUName) {
					usage = s.UsageName
				}
			}
		}
		if usage == "" {
			a.note("quota usage name for %s %s is unknown", name, r.SKUName)
			continue
		}
		k := strings.ToLower(loc + "|" + usage)
		g := groups[k]
		if g == nil {
			g = &group{loc: loc, usage: usage}
			groups[k] = g
		}
		g.items = append(g.items, planned{r: r, capacity: capy, sku: r.SKUName})
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	usages := map[string][]azure.QuotaUsage{}
	for _, k := range keys {
		g := groups[k]
		us, ok := usages[strings.ToLower(g.loc)]
		if !ok {
			var err error
			us, err = e.d.Models.ListUsages(ctx, e.sub(), g.loc)
			if err != nil {
				return sdk.Result{}, err
			}
			usages[strings.ToLower(g.loc)] = us
		}
		var u *azure.QuotaUsage
		for i := range us {
			if strings.EqualFold(us[i].Name, g.usage) {
				u = &us[i]
			}
		}
		if u == nil || u.Unit == "" {
			a.note("quota line %s in %s has an unknown unit mapping", g.usage, g.loc)
			continue
		}
		var total float64
		for _, it := range g.items {
			total += it.capacity
		}
		avail := u.Limit - u.Current
		if total <= avail {
			continue
		}
		// Apparent shortfall: add back the capacity of deployments that already exist.
		offset, unknown := 0.0, false
		for _, it := range g.items {
			off, known, why := e.existingCapacity(ctx, it.r, it.sku)
			if !known {
				unknown = true
				a.note("quota %s in %s looks insufficient but the capacity already held by %s cannot be offset (%s)", g.usage, g.loc, it.r.Name, why)
				continue
			}
			offset += off
		}
		if unknown {
			continue
		}
		if total-offset <= avail {
			continue
		}
		first := g.items[0].r
		ev := fmt.Sprintf("requested %.0f of quota %s in %s but only %.0f of %.0f is free", total, g.usage, g.loc, avail, u.Limit)
		if offset > 0 {
			ev += fmt.Sprintf(" (after adding back %.0f already held by existing deployments of the same name and SKU)", offset)
		}
		a.add(tmplFinding(first, ev))
	}
	return a.result(), nil
}

// existingCapacity returns the capacity an existing deployment of the same
// account, name and SKU already holds (an update replaces it). known is false,
// with the reason, when existence could not be established.
func (e *env) existingCapacity(ctx context.Context, r sdk.ARMResource, sku string) (capacity float64, known bool, why string) {
	account, dep, ok := splitDeploymentName(r.Name)
	if !ok {
		return 0, false, "the deployment name is not a literal <account>/<deployment>"
	}
	if e.d.Foundry == nil {
		return 0, false, "the Microsoft.CognitiveServices/accounts/deployments/read capability is not available"
	}
	if e.d.Target.ResourceGroup == "" {
		return 0, false, "the target resource group is not set"
	}
	list, err := e.existingDeployments(ctx, account)
	if err != nil {
		return 0, false, oneLine(azureReason(err))
	}
	for _, d := range list {
		if !strings.EqualFold(lastSegment(d.Name), dep) {
			continue
		}
		if d.SKUName != "" && !strings.EqualFold(d.SKUName, sku) {
			return 0, false, "the existing deployment uses SKU " + d.SKUName + ", a different quota line"
		}
		if d.SKUName == "" {
			return 0, false, "the SKU of the existing deployment was not reported"
		}
		return float64(d.CurrentCapacity), true, ""
	}
	return 0, true, ""
}

var softDeleteKinds = map[string]azure.NameKind{
	"microsoft.keyvault/vaults":            azure.NameKeyVault,
	"microsoft.cognitiveservices/accounts": azure.NameFoundry,
	"microsoft.apimanagement/service":      azure.NameAPIM,
}

// rgOfID returns the resource group segment of an ARM ID, "" when absent.
func rgOfID(id string) string {
	parts := strings.Split(id, "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "resourceGroups") {
			return parts[i+1]
		}
	}
	return ""
}

// DEP-006: soft-deleted name collisions.
//
// The catalogue matches a planned name against the subscription-wide list of
// deleted entries. Whether a deleted entry blocks the name in another resource
// group or region is not documented, so a match whose deleted resource group
// or location differs from the planned resource is reported with a lower
// confidence and says so. The evidence names the deleted entry's ID and
// location (the purge path is keyed by location, resource group and name).
func (e *env) dep006(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	var cands []sdk.ARMResource
	for _, r := range rs {
		if _, ok := softDeleteKinds[strings.ToLower(r.Type)]; ok {
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		return sdk.Result{}, nil
	}
	if e.d.Names == nil {
		return skipRes("", missingCap("soft-deleted resource listing", "Reader on the subscription")), nil
	}
	if m := needTarget(false, e.d.Target); m != "" {
		return skipRes("", m), nil
	}
	var a acc
	for _, r := range cands {
		if isExpr(r.Name) {
			a.note("name of %s is not a literal value", r.Type)
			continue
		}
		kind := softDeleteKinds[strings.ToLower(r.Type)]
		list, err := e.softDeleted(ctx, kind)
		if err != nil {
			return sdk.Result{}, err
		}
		for _, s := range list {
			if !strings.EqualFold(s.Name, r.Name) {
				continue
			}
			ev := fmt.Sprintf("name %s is held by soft-deleted %s %s", r.Name, kind, s.ID)
			if s.Location != "" {
				ev += " in " + s.Location
			}
			if s.ScheduledPurgeDate != "" {
				ev += " (scheduled purge " + s.ScheduledPurgeDate + ")"
			}
			if s.PurgeProtection {
				ev += "; purge protection is on, so it cannot be purged early"
			}
			f := tmplFinding(r, ev)
			conf := sdk.ConfidenceHigh
			if kind == azure.NameKeyVault {
				f.Evidence += " (likely conflict)"
				conf = sdk.ConfidenceMedium
			}
			var differs []string
			if drg := rgOfID(s.ID); drg != "" && e.d.Target.ResourceGroup != "" && !strings.EqualFold(drg, e.d.Target.ResourceGroup) {
				differs = append(differs, "resource group "+drg+" is not the target group "+e.d.Target.ResourceGroup)
			}
			if want := e.regionOf(r); s.Location != "" && want != "" && !isExpr(want) && normLoc(s.Location) != normLoc(want) {
				differs = append(differs, "location "+s.Location+" is not the planned location "+want)
			}
			if len(differs) > 0 {
				f.Evidence += "; the deleted entry's " + strings.Join(differs, " and ") + ", and the documentation does not say whether it blocks this name there"
				conf = sdk.ConfidenceLow
			}
			f.Confidence = conf
			a.add(f)
		}
	}
	return a.result(), nil
}

func injections(r sdk.ARMResource) []map[string]any {
	raw, _ := r.Properties["networkInjections"].([]any)
	var out []map[string]any
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok && strings.EqualFold(getStr(m, "scenario"), "agent") {
			out = append(out, m)
		}
	}
	return out
}
