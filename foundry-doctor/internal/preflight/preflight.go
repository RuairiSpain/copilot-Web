// Package preflight implements the Phase 2 deployment-preflight rules
// FND-DEP-001..FND-DEP-012.
//
// Every rule is a deterministic, read-only sdk.Rule that talks to Azure only
// through the narrow interfaces in internal/azure (ADR-006). Outcomes:
//
//   - pass: the check ran and found nothing;
//   - fail: one or more findings;
//   - skipped: the check could not run; the Skip names the exact capability
//     and the missing permission (reason prefix "input-unavailable");
//   - uncertain: the check ran but could not prove the result; it is a Skip
//     whose reason starts with UncertainPrefix. Uncertain is never a pass.
//
// No rule claims a deployment will succeed. What-if runs only when the caller
// opted in (Deps.WhatIfOptIn) and is shared by every rule that needs it.
package preflight

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// UncertainPrefix starts the Skip.Reason of an "uncertain" outcome.
const UncertainPrefix = "uncertain: "

// Target is the deployment target resolved by the caller.
type Target struct {
	TenantID       string
	SubscriptionID string
	ResourceGroup  string
	Location       string
}

// Deps carries the Azure capabilities and run options. Any nil capability makes
// the rules that need it skip, naming that capability.
type Deps struct {
	Context     azure.ContextProvider
	Inventory   azure.Inventory
	Permissions azure.Permissions
	Models      azure.Models
	Policy      azure.Policy
	WhatIf      azure.WhatIf
	Names       azure.Names
	// Optional capabilities; the rules that need one skip naming it when nil.
	Regions     azure.RegionAvailability
	Deployments azure.DeploymentHistory
	SubnetLinks azure.SubnetLinks
	Evidence    azure.PermissionEvidence

	Target Target
	// WhatIfOptIn records the explicit --what-if / --preflight flag.
	WhatIfOptIn bool
	// ApprovedScopes are extra scopes (resource IDs) that cross-resource-group
	// references may point at. By default only the target resource group is in scope.
	ApprovedScopes []string
	// AllowedDeletes lists resource IDs (case-insensitive) whose predicted
	// deletion is accepted by the baseline.
	AllowedDeletes []string
	// Template is the compiled ARM JSON used for what-if when the ARM model
	// does not expose it through RawTemplate().
	Template []byte
	// Now defaults to time.Now.
	Now func() time.Time
}

// RuleIDs returns the preflight rule IDs in order.
func RuleIDs() []string {
	ids := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		ids = append(ids, fmt.Sprintf("FND-DEP-%03d", i))
	}
	return ids
}

// Register adds every preflight rule to reg.
func Register(reg *rules.Registry, d Deps) error {
	for _, r := range Rules(d) {
		if err := reg.Register(r); err != nil {
			return err
		}
	}
	return nil
}

// Rules builds the twelve rules sharing one evaluation environment.
func Rules(d Deps) []sdk.Rule {
	if d.Now == nil {
		d.Now = time.Now
	}
	e := &env{d: d, models: map[string]modelEntry{}, existing: map[string]existingEntry{}, soft: map[azure.NameKind]softEntry{}}
	fns := []func(context.Context, *sdk.Input) (sdk.Result, error){
		e.dep001, e.dep002, e.dep003, e.dep004, e.dep005, e.dep006,
		e.dep007, e.dep008, e.dep009, e.dep010, e.dep011, e.dep012,
	}
	out := make([]sdk.Rule, len(fns))
	for i, fn := range fns {
		out[i] = rule{id: RuleIDs()[i], fn: fn}
	}
	return out
}

type rule struct {
	id string
	fn func(context.Context, *sdk.Input) (sdk.Result, error)
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if in == nil {
		return skipRes(r.id, sdk.SkipInputUnavailable+": no input"), nil
	}
	res, err := r.fn(ctx, in)
	if err != nil {
		if ctx.Err() != nil {
			return sdk.Result{}, ctx.Err()
		}
		return skipRes(r.id, azureReason(err)), nil
	}
	if res.Skipped != nil {
		res.Findings = nil
		res.Skipped.RuleID = r.id
		return res, nil
	}
	return res, nil
}

// env is the state shared by the rules of one run.
type env struct {
	d Deps

	mu       sync.Mutex
	models   map[string]modelEntry
	existing map[string]existingEntry
	soft     map[azure.NameKind]softEntry

	wiOnce sync.Once
	wi     whatIfOutcome
}

type modelEntry struct {
	list []azure.ModelAvailability
	err  error
}
type existingEntry struct {
	list azure.ResourceList
	err  error
}
type softEntry struct {
	list []azure.SoftDeleted
	err  error
}
type whatIfOutcome struct {
	res  azure.WhatIfResult
	skip string
	err  error
}

// --- results -------------------------------------------------------------

func skipRes(id, reason string) sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{RuleID: id, Reason: reason}}
}

// azureReason renders an Azure error as a skip reason naming the capability
// and the missing permission.
func azureReason(err error) string {
	if u, ok := azure.AsUnavailable(err); ok {
		s := sdk.SkipInputUnavailable + ": capability " + oneLine(u.Capability)
		if u.Permission != "" {
			s += "; missing permission " + oneLine(u.Permission)
		}
		if u.Reason != "" {
			s += "; " + oneLine(u.Reason)
		}
		return s
	}
	return sdk.SkipInputUnavailable + ": Azure call failed: " + oneLine(err.Error())
}

func missingCap(capability, permission string) string {
	s := sdk.SkipInputUnavailable + ": capability " + capability + " is not available"
	if permission != "" {
		s += "; missing permission " + permission
	}
	return s
}

func oneLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.TrimSpace(s)
}

// acc accumulates the outcome of one rule.
type acc struct {
	findings []sdk.Finding
	notes    []string
	skips    []string
}

func (a *acc) add(f sdk.Finding) { a.findings = append(a.findings, f) }
func (a *acc) note(format string, args ...any) {
	a.notes = append(a.notes, fmt.Sprintf(format, args...))
}
func (a *acc) skip(reason string) { a.skips = append(a.skips, reason) }
func (a *acc) skipErr(err error)  { a.skips = append(a.skips, azureReason(err)) }
func (a *acc) result() sdk.Result {
	if len(a.findings) > 0 {
		f := append([]sdk.Finding(nil), a.findings...)
		sort.SliceStable(f, func(i, j int) bool {
			if f[i].Resource.ID != f[j].Resource.ID {
				return f[i].Resource.ID < f[j].Resource.ID
			}
			if f[i].Resource.Name != f[j].Resource.Name {
				return f[i].Resource.Name < f[j].Resource.Name
			}
			return f[i].Evidence < f[j].Evidence
		})
		return sdk.Result{Findings: f}
	}
	if len(a.skips) > 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: joinUnique(a.skips)}}
	}
	if len(a.notes) > 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: UncertainPrefix + joinUnique(a.notes)}}
	}
	return sdk.Result{}
}

func joinUnique(in []string) string {
	seen := map[string]bool{}
	var u []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			u = append(u, s)
		}
	}
	sort.Strings(u)
	if len(u) > 5 {
		n := len(u) - 5
		u = append(u[:5], fmt.Sprintf("(+%d more)", n))
	}
	return strings.Join(u, "; ")
}

// --- template helpers ----------------------------------------------------

func isExpr(s string) bool { return strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "[[") }

func resourcesOf(in *sdk.Input) ([]sdk.ARMResource, bool) {
	if in.ARM == nil {
		return nil, false
	}
	return in.ARM.Resources(), true
}

func byType(rs []sdk.ARMResource, typ string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range rs {
		if strings.EqualFold(r.Type, typ) {
			out = append(out, r)
		}
	}
	return out
}

func tmplFinding(r sdk.ARMResource, evidence string) sdk.Finding {
	return sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
		Location: r.Location,
		Evidence: evidence,
	}
}

func idFinding(typ, id, evidence string) sdk.Finding {
	return sdk.Finding{Resource: sdk.ResourceRef{Type: typ, ID: id}, Evidence: evidence}
}

func namespaceOf(typ string) string {
	if i := strings.Index(typ, "/"); i > 0 {
		return typ[:i]
	}
	return typ
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func getMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		next, _ := cur[k].(map[string]any)
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

func getStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func (e *env) sub() string { return e.d.Target.SubscriptionID }

func (e *env) rgScope() string {
	if e.d.Target.SubscriptionID == "" || e.d.Target.ResourceGroup == "" {
		return ""
	}
	return "/subscriptions/" + e.d.Target.SubscriptionID + "/resourceGroups/" + e.d.Target.ResourceGroup
}

func (e *env) subScope() string { return "/subscriptions/" + e.d.Target.SubscriptionID }

// regionOf returns the literal region of r, else the target location.
func (e *env) regionOf(r sdk.ARMResource) string {
	if r.Region != "" && !isExpr(r.Region) {
		return r.Region
	}
	return e.d.Target.Location
}

// scopeAllowed implements the cross-resource-group scope policy: an ID is in
// scope when it lies under the target resource group or an approved scope.
func (e *env) scopeAllowed(id string) bool {
	id = strings.ToLower(strings.TrimRight(id, "/"))
	scopes := append([]string{e.rgScope()}, e.d.ApprovedScopes...)
	for _, s := range scopes {
		s = strings.ToLower(strings.TrimRight(s, "/"))
		if s == "" {
			continue
		}
		if id == s || strings.HasPrefix(id, s+"/") {
			return true
		}
	}
	return false
}

// needs returns a skip result when a required capability or target field is missing.
func needTarget(needRG bool, t Target) string {
	if t.SubscriptionID == "" {
		return sdk.SkipInputUnavailable + ": target subscription is not set (use --subscription or AZURE_SUBSCRIPTION_ID)"
	}
	if needRG && t.ResourceGroup == "" {
		return sdk.SkipInputUnavailable + ": target resource group is not set (use --resource-group or AZURE_RESOURCE_GROUP)"
	}
	return ""
}

func (e *env) modelsFor(ctx context.Context, loc, format, name string) ([]azure.ModelAvailability, error) {
	key := strings.ToLower(loc + "|" + format + "|" + name)
	e.mu.Lock()
	m, ok := e.models[key]
	e.mu.Unlock()
	if ok {
		return m.list, m.err
	}
	l, err := e.d.Models.ListModels(ctx, azure.ModelQuery{SubscriptionID: e.sub(), Location: loc, Format: format, Name: name})
	e.mu.Lock()
	e.models[key] = modelEntry{l, err}
	e.mu.Unlock()
	return l, err
}

func (e *env) listExisting(ctx context.Context, typ string) (azure.ResourceList, error) {
	key := strings.ToLower(typ)
	e.mu.Lock()
	m, ok := e.existing[key]
	e.mu.Unlock()
	if ok {
		return m.list, m.err
	}
	l, err := e.d.Inventory.ListResources(ctx, azure.InventoryQuery{Scope: e.rgScope(), Types: []string{typ}})
	e.mu.Lock()
	e.existing[key] = existingEntry{l, err}
	e.mu.Unlock()
	return l, err
}

func (e *env) softDeleted(ctx context.Context, k azure.NameKind) ([]azure.SoftDeleted, error) {
	e.mu.Lock()
	m, ok := e.soft[k]
	e.mu.Unlock()
	if ok {
		return m.list, m.err
	}
	l, err := e.d.Names.ListSoftDeleted(ctx, k, e.sub())
	e.mu.Lock()
	e.soft[k] = softEntry{l, err}
	e.mu.Unlock()
	return l, err
}

// rawTemplate returns the compiled ARM JSON, if known.
func (e *env) rawTemplate(in *sdk.Input) []byte {
	if rt, ok := in.ARM.(interface{ RawTemplate() []byte }); ok {
		if b := rt.RawTemplate(); len(b) > 0 {
			return b
		}
	}
	return e.d.Template
}

// whatIf runs the single shared what-if (once) and returns its outcome.
func (e *env) whatIf(ctx context.Context, in *sdk.Input) whatIfOutcome {
	e.wiOnce.Do(func() {
		switch {
		case !e.d.WhatIfOptIn:
			e.wi.skip = sdk.SkipInputUnavailable + ": what-if was not requested; re-run with --what-if (or --preflight)"
		case e.d.WhatIf == nil:
			e.wi.skip = missingCap("Microsoft.Resources/deployments/whatIf", "Microsoft.Resources/deployments/whatIf/action")
		default:
			if m := needTarget(true, e.d.Target); m != "" {
				e.wi.skip = m
				return
			}
			if e.d.Target.Location == "" {
				e.wi.skip = sdk.SkipInputUnavailable + ": target location is not set (use --location or AZURE_LOCATION)"
				return
			}
			tpl := e.rawTemplate(in)
			if len(tpl) == 0 {
				e.wi.skip = sdk.SkipInputUnavailable + ": compiled ARM template is not available for what-if"
				return
			}
			res, err := e.d.WhatIf.Run(ctx, azure.WhatIfRequest{OptIn: true, Scope: e.rgScope(), Location: e.d.Target.Location, Template: tpl})
			if err != nil {
				e.wi.err = err
				e.wi.skip = azureReason(err)
				return
			}
			e.wi.res = res
		}
	})
	return e.wi
}
