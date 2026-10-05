package preflight

import (
	"context"
	"fmt"
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

// DEP-002: resource provider registration.
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
	need := map[string]bool{}
	injected := false
	for _, r := range rs {
		if ns := namespaceOf(r.Type); ns != "" && !strings.EqualFold(ns, "Microsoft.Resources") {
			need[strings.ToLower(ns)] = true
		}
		if strings.EqualFold(r.Type, typeAccount) && len(injections(r)) > 0 {
			injected = true
		}
	}
	display := map[string]string{}
	for _, r := range rs {
		display[strings.ToLower(namespaceOf(r.Type))] = namespaceOf(r.Type)
	}
	if injected {
		for _, ns := range networkSecuredNamespaces {
			need[strings.ToLower(ns)] = true
			display[strings.ToLower(ns)] = ns
		}
	}
	if len(need) == 0 {
		return sdk.Result{}, nil
	}
	var names []string
	for k := range need {
		names = append(names, display[k])
	}
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
		case strings.EqualFold(st, "Registered"), strings.EqualFold(st, "Registering"):
		default:
			a.add(idFinding("Microsoft.Resources/providers", e.subScope()+"/providers/"+n, fmt.Sprintf("resource provider %s is %s; registration is required (this tool never registers providers)", n, st)))
		}
	}
	return a.result(), nil
}

// DEP-003: effective write permissions at the target scope.
func (e *env) dep003(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	rs, ok := resourcesOf(in)
	if !ok {
		return skipRes("", sdk.SkipInputUnavailable+": compiled ARM template is not available"), nil
	}
	if e.d.Permissions == nil && e.d.Evidence == nil {
		return skipRes("", missingCap("Microsoft.Authorization/permissions read", "Microsoft.Authorization/permissions/read")), nil
	}
	if m := needTarget(true, e.d.Target); m != "" {
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
	var a acc
	// <provider>/register/action is required for each template namespace
	// whose registration state (DEP-002 evidence) is not Registered.
	registerFor := e.unregisteredNamespaces(ctx, rs, &a)
	for _, ns := range registerFor {
		actions[ns+"/register/action"] = true
	}
	var list []string
	for a := range actions {
		list = append(list, a)
	}
	sort.Strings(list)
	scope := e.rgScope()
	var ds []azure.ActionDecision
	var err error
	if e.d.Evidence != nil {
		// Primary evidence per FND-DEP-003: GET .../providers/Microsoft.Authorization/permissions.
		ds, err = e.d.Evidence.CheckReportedActions(ctx, azure.PermissionRequest{Scope: scope, Actions: list})
	} else {
		ds, err = e.d.Permissions.EffectiveActions(ctx, azure.PermissionRequest{Scope: scope, Actions: list})
		a.note("the Microsoft.Authorization/permissions read capability is unavailable; permissions were derived from role assignments instead")
	}
	if err != nil {
		return sdk.Result{}, err
	}
	for _, d := range ds {
		switch d.Decision {
		case azure.DecisionDenied:
			why := d.Reason
			if why == "" {
				why = azure.ReasonNoGrantingRole
			}
			a.add(idFinding("Microsoft.Authorization/permissions", scope, fmt.Sprintf("action %s is not granted at the resource group (%s); deployment is likely to be denied", d.Action, why)))
		case azure.DecisionUnknown:
			a.note("could not evaluate %s (%s)", d.Action, d.Reason)
		}
	}
	if len(ds) < len(list) {
		a.note("permission decisions were returned for %d of %d required actions", len(ds), len(list))
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

// DEP-005: quota headroom.
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
	type req struct {
		total float64
		first sdk.ARMResource
		names []string
		loc   string
	}
	var a acc
	reqs := map[string]*req{}
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
			if !strings.EqualFold(m.Name, name) {
				continue
			}
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
		q := reqs[k]
		if q == nil {
			q = &req{first: r, loc: loc}
			reqs[k] = q
		}
		q.total += capy
		q.names = append(q.names, r.Name)
	}
	keys := make([]string, 0, len(reqs))
	for k := range reqs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	usages := map[string][]azure.QuotaUsage{}
	for _, k := range keys {
		q := reqs[k]
		usage := k[strings.Index(k, "|")+1:]
		us, ok := usages[strings.ToLower(q.loc)]
		if !ok {
			var err error
			us, err = e.d.Models.ListUsages(ctx, e.sub(), q.loc)
			if err != nil {
				return sdk.Result{}, err
			}
			usages[strings.ToLower(q.loc)] = us
		}
		var u *azure.QuotaUsage
		for i := range us {
			if strings.EqualFold(us[i].Name, usage) {
				u = &us[i]
			}
		}
		if u == nil || u.Unit == "" {
			a.note("quota line %s in %s has an unknown unit mapping", usage, q.loc)
			continue
		}
		avail := u.Limit - u.Current
		if q.total <= avail {
			continue
		}
		if e.existingDeployment(ctx, q.names) {
			a.note("quota %s in %s looks insufficient (%.0f requested, %.0f free) but a deployment of the same name already exists, so its capacity cannot be offset", usage, q.loc, q.total, avail)
			continue
		}
		a.add(tmplFinding(q.first, fmt.Sprintf("requested %.0f of quota %s in %s but only %.0f of %.0f is free", q.total, usage, q.loc, avail, u.Limit)))
	}
	return a.result(), nil
}

// existingDeployment reports whether any named deployment exists (best effort).
func (e *env) existingDeployment(ctx context.Context, names []string) bool {
	if e.d.Inventory == nil || e.rgScope() == "" {
		return false
	}
	l, err := e.listExisting(ctx, typeDeployment)
	if err != nil {
		return false
	}
	for _, n := range names {
		for _, r := range l.Resources {
			if strings.EqualFold(lastSegment(r.Name), lastSegment(n)) {
				return true
			}
		}
	}
	return false
}

var softDeleteKinds = map[string]azure.NameKind{
	"microsoft.keyvault/vaults":            azure.NameKeyVault,
	"microsoft.cognitiveservices/accounts": azure.NameFoundry,
	"microsoft.apimanagement/service":      azure.NameAPIM,
}

// DEP-006: soft-deleted name collisions.
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
			if s.ScheduledPurgeDate != "" {
				ev += " (scheduled purge " + s.ScheduledPurgeDate + ")"
			}
			if s.PurgeProtection {
				ev += "; purge protection is on, so it cannot be purged early"
			}
			if kind == azure.NameKeyVault {
				ev += " (likely conflict)"
			}
			a.add(tmplFinding(r, ev))
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
