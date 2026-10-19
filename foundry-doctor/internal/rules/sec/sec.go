// Package sec implements the Phase 1 FND-SEC-* rules as static checks over the
// compiled ARM model. Rules never guess: an unresolved ARM expression yields a
// skip (never a pass), and a missing ARM model yields SkipInputUnavailable.
package sec

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/foundrylink"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SkipOutOfScope is the skip reason used when resources exist but none is in
// Foundry scope (FND-SEC-002, FND-SEC-003).
const SkipOutOfScope = sdk.SkipNotApplicable + ": out-of-foundry-scope"

// SkipUnresolved is the skip reason used when a decisive value is an
// unresolved ARM expression and no finding could be established.
const SkipUnresolved = "unresolved-expression"

const (
	typeAccounts    = "Microsoft.CognitiveServices/accounts"
	typeSearch      = "Microsoft.Search/searchServices"
	typeCosmos      = "Microsoft.DocumentDB/databaseAccounts"
	typeStorage     = "Microsoft.Storage/storageAccounts"
	typeKeyVault    = "Microsoft.KeyVault/vaults"
	typeAcctProject = typeAccounts + "/projects"
	typeAcctDeploy  = typeAccounts + "/deployments"
)

// Register returns the Phase 1 SEC rules in ID order, FND-SEC-014 included
// (it analyses the compiled ARM outputs; see sec014.go).
func Register() []sdk.Rule {
	return []sdk.Rule{
		fn{"FND-SEC-001", evalSEC001},
		fn{"FND-SEC-002", evalSEC002},
		fn{"FND-SEC-003", evalSEC003},
		fn{"FND-SEC-004", evalSEC004},
		fn{"FND-SEC-005", evalSEC005},
		fn{"FND-SEC-006", evalSEC006},
		fn{"FND-SEC-007", evalSEC007},
		fn{"FND-SEC-008", evalSEC008},
		fn{"FND-SEC-009", evalSEC009},
		fn{"FND-SEC-010", evalSEC010},
		fn{"FND-SEC-011", evalSEC011},
		fn{"FND-SEC-012", evalSEC012},
		fn{"FND-SEC-013", evalSEC013},
		fn{"FND-SEC-014", evalSEC014},
	}
}

type fn struct {
	id string
	f  func(*sdk.Input) sdk.Result
}

func (r fn) ID() string { return r.id }

func (r fn) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if err := ctx.Err(); err != nil {
		return sdk.Result{}, fmt.Errorf("%s: %w", r.id, err)
	}
	if in == nil || in.ARM == nil {
		return unavailable(), nil
	}
	return r.f(in), nil
}

// notApplicable is the by-design skip: the template has nothing the rule checks.
func notApplicable() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipNotApplicable + ": no applicable resources in the compiled template"}}
}

func unavailable() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": compiled ARM model unavailable"}}
}

func skip(reason string) sdk.Result { return sdk.Result{Skipped: &sdk.Skip{Reason: reason}} }

// acc accumulates findings and notes values that block a verdict.
type acc struct {
	fs         []sdk.Finding
	unresolved bool
	// scopeUncertain: Foundry usage of a resource could not be proven or
	// disproven (reason foundrylink.SkipScopeUncertain).
	scopeUncertain bool
	// conditional: a resource that would be evaluated may not be deployed
	// (reason foundrylink.SkipConditional).
	conditional bool
	// notes names resources that could not be evaluated. When a rule has
	// findings, they are returned as Skips next to the findings so the
	// unevaluated resources stay visible (review M3, ADR-008 amendment 3).
	notes []string
	// infos are informational per-resource notes that never block a pass. They
	// are returned as Skips next to the verdict (findings or a pass).
	infos []string
}

// info records an informational note about r (it does not block a pass). The
// reason reads "head: resource: detail" so it starts with a reason head.
func (a *acc) info(r sdk.ARMResource, head, why string) {
	n := armexpr.StripScope(r.Name)
	if len(n) > 60 {
		n = n[:57] + "..."
	}
	a.infos = append(a.infos, head+": "+n+": "+why)
}

// note records why r was not evaluated.
func (a *acc) note(r sdk.ARMResource, why string) {
	n := armexpr.StripScope(r.Name)
	if len(n) > 60 {
		n = n[:57] + "..."
	}
	a.notes = append(a.notes, n+": "+why)
}

func (a *acc) add(r sdk.ARMResource, evidence string) {
	a.fs = append(a.fs, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: armexpr.StripScope(r.Name)},
		Location: r.Location,
		Evidence: armexpr.StripScope(evidence),
	})
}

// addKey adds a finding with a stable Key that separates several findings of
// one rule on one resource (the property path or sub-check name).
func (a *acc) addKey(r sdk.ARMResource, key, evidence string) {
	a.add(r, evidence)
	a.fs[len(a.fs)-1].Key = key
}

// addRec adds a finding with a rule-specific recommendation.
func (a *acc) addRec(r sdk.ARMResource, key, evidence, recommendation string) {
	a.addKey(r, key, evidence)
	a.fs[len(a.fs)-1].Recommendation = recommendation
}

// maybe reports (and records) that r is deployed only under a condition or
// loop the template cannot resolve; callers skip such a resource.
func (a *acc) maybe(r sdk.ARMResource) bool {
	if presence(r) == armexpr.MaybeDeployed {
		a.conditional = true
		a.note(r, foundrylink.SkipConditional+": deployed only under a condition or loop the template cannot resolve")
		return true
	}
	return false
}

// result: findings win; otherwise uncertainty skips; otherwise pass.
func (a *acc) result() sdk.Result {
	if len(a.fs) > 0 {
		notes := append(append([]string(nil), a.notes...), a.infos...)
		if a.scopeUncertain {
			notes = append(notes, foundrylink.SkipScopeUncertain+": a resource may be Foundry-related but could not be correlated")
		}
		if a.unresolved && len(notes) == 0 {
			notes = append(notes, SkipUnresolved+": a decisive value is an unresolved expression")
		}
		sort.Strings(notes)
		var skips []sdk.Skip
		for _, n := range notes {
			skips = append(skips, sdk.Skip{Reason: n})
		}
		return sdk.Result{Findings: a.fs, Skips: skips}
	}
	switch {
	case a.scopeUncertain:
		return skip(foundrylink.SkipScopeUncertain)
	case a.conditional:
		return skip(foundrylink.SkipConditional)
	case a.unresolved:
		return skip(SkipUnresolved)
	}
	return sdk.Result{Skips: skipsOf(a.infos)}
}

// skipsOf turns informational notes into sorted Skips (nil when empty).
func skipsOf(notes []string) []sdk.Skip {
	if len(notes) == 0 {
		return nil
	}
	sorted := append([]string(nil), notes...)
	sort.Strings(sorted)
	out := make([]sdk.Skip, 0, len(sorted))
	for i, n := range sorted {
		if i > 0 && sorted[i-1] == n {
			continue
		}
		out = append(out, sdk.Skip{Reason: n})
	}
	return out
}

// resultDetailed is result with the unresolved-expression skip naming what was
// unresolved (the notes), so an operator can see what blocked the verdict.
func (a *acc) resultDetailed() sdk.Result {
	out := a.result()
	if out.Skipped != nil && out.Skipped.Reason == SkipUnresolved && len(a.notes) > 0 {
		var parts []string
		for _, s := range skipsOf(a.notes) {
			parts = append(parts, s.Reason)
		}
		out.Skipped = &sdk.Skip{Reason: SkipUnresolved + ": " + strings.Join(parts, "; ")}
	}
	return out
}

func presence(r sdk.ARMResource) armexpr.Presence {
	return armexpr.PresenceOf(r.Conditional, r.Condition, r.Looped)
}

// ofType returns the resources of type t that are not provably undeployed
// (a literal false condition removes a resource).
func ofType(in *sdk.Input, t string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range in.ARM.Resources() {
		if strings.EqualFold(r.Type, t) && presence(r) != armexpr.NotDeployed {
			out = append(out, r)
		}
	}
	return out
}

// unresolved reports an ARM language expression ("[...]" but not "[[").
func unresolved(v any) bool {
	s, ok := v.(string)
	return ok && armexpr.IsExpression(s)
}

func get(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = mm[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

type state int

const (
	absent state = iota
	isTrue
	isFalse
	other // present but neither a literal of the expected kind nor an expression
	expr  // unresolved expression
)

func boolState(m map[string]any, path ...string) state {
	v, ok := get(m, path...)
	switch {
	case !ok || v == nil:
		return absent
	case unresolved(v):
		return expr
	}
	if b, ok := v.(bool); ok {
		if b {
			return isTrue
		}
		return isFalse
	}
	return other
}

// str returns the string value; state is isTrue when a literal string.
func str(m map[string]any, path ...string) (string, state) {
	v, ok := get(m, path...)
	if !ok || v == nil {
		return "", absent
	}
	if unresolved(v) {
		return "", expr
	}
	s, ok := v.(string)
	if !ok {
		return "", other
	}
	return s, isTrue
}

const (
	ownsNone  = foundrylink.OwnsNone
	ownsYes   = foundrylink.OwnsYes
	ownsMaybe = foundrylink.OwnsMaybe
)

// owns reports whether account owns a child resource of the given types; see
// foundrylink.Owns.
func owns(in *sdk.Input, account sdk.ARMResource, accounts int, types ...string) foundrylink.Ownership {
	return foundrylink.Owns(in.ARM, account, accounts, types...)
}

func evalSEC001(in *sdk.Input) sdk.Result {
	accounts := ofType(in, typeAccounts)
	var a acc
	seen := false
	for _, r := range accounts {
		switch owns(in, r, len(accounts), typeAcctProject, typeAcctDeploy) {
		case ownsNone:
			continue
		case ownsMaybe:
			a.scopeUncertain = true
			continue
		}
		if a.maybe(r) {
			continue
		}
		seen = true
		switch boolState(r.Properties, "disableLocalAuth") {
		case isTrue:
		case expr:
			a.unresolved = true
		case absent:
			a.addKey(r, "disableLocalAuth", "Foundry account owns projects or deployments; properties.disableLocalAuth is absent (key authentication defaults to enabled)")
		default:
			a.addKey(r, "disableLocalAuth", "Foundry account owns projects or deployments; properties.disableLocalAuth is not true")
		}
	}
	if !seen && !a.scopeUncertain && !a.conditional {
		return notApplicable()
	}
	return a.result()
}

// linkOf classifies r against the Foundry link set and records uncertainty.
// It returns true only when r is provably used by Foundry and deployed.
func linkOf(a *acc, set foundrylink.Set, r sdk.ARMResource) bool {
	switch set.Classify(r) {
	case foundrylink.Linked:
		return !a.maybe(r)
	case foundrylink.Uncertain:
		a.scopeUncertain = true
	}
	return false
}

func evalSEC002(in *sdk.Input) sdk.Result {
	svcs := ofType(in, typeSearch)
	if len(svcs) == 0 {
		return notApplicable()
	}
	set := foundrylink.Collect(in.ARM)
	var a acc
	scoped := false
	for _, r := range svcs {
		if !linkOf(&a, set, r) {
			continue
		}
		scoped = true
		switch boolState(r.Properties, "disableLocalAuth") {
		case isTrue:
			continue
		case expr:
			a.unresolved = true
			continue
		}
		sub := "neither authOptions.apiKeyOnly nor authOptions.aadOrApiKey is present"
		if _, ok := get(r.Properties, "authOptions", "apiKeyOnly"); ok {
			sub = "authOptions.apiKeyOnly is present"
		} else if _, ok := get(r.Properties, "authOptions", "aadOrApiKey"); ok {
			sub = "authOptions.aadOrApiKey is present"
			if m, st := str(r.Properties, "authOptions", "aadOrApiKey", "aadAuthFailureMode"); st == isTrue {
				sub += " (aadAuthFailureMode=" + m + ")"
			}
		}
		a.addKey(r, "disableLocalAuth", "Search service referenced by Foundry; properties.disableLocalAuth is not true; "+sub)
	}
	if !scoped && len(a.fs) == 0 && !a.scopeUncertain && !a.conditional {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC003(in *sdk.Input) sdk.Result {
	dbs := ofType(in, typeCosmos)
	if len(dbs) == 0 {
		return notApplicable()
	}
	set := foundrylink.Collect(in.ARM)
	var a acc
	scoped := false
	for _, r := range dbs {
		if !linkOf(&a, set, r) {
			continue
		}
		scoped = true
		switch boolState(r.Properties, "disableLocalAuth") {
		case isTrue:
		case expr:
			a.unresolved = true
		default:
			a.addKey(r, "disableLocalAuth", "Cosmos DB account referenced by Foundry; properties.disableLocalAuth is not true")
		}
	}
	if !scoped && len(a.fs) == 0 && !a.scopeUncertain && !a.conditional {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC004(in *sdk.Input) sdk.Result {
	stores := ofType(in, typeStorage)
	if len(stores) == 0 {
		return notApplicable()
	}
	var a acc
	keyed := foundrylink.CollectWhere(in.ARM, usesKeyAuth)
	for _, r := range stores {
		if a.maybe(r) {
			continue
		}
		p := r.Properties
		switch boolState(p, "allowSharedKeyAccess") {
		case isFalse:
		case expr:
			a.unresolved = true
		default:
			ev := "properties.allowSharedKeyAccess is not false (shared key access enabled)"
			if keyed.Classify(r) == foundrylink.NotLinked {
				a.addKey(r, "allowSharedKeyAccess", ev)
			} else {
				a.addRec(r, "allowSharedKeyAccess", ev, sharedKeyRecommendation+" "+keyAuthWarning)
			}
		}
		switch boolState(p, "allowBlobPublicAccess") {
		case isTrue:
			a.addKey(r, "allowBlobPublicAccess", "properties.allowBlobPublicAccess is true (anonymous blob access allowed)")
		case expr:
			a.unresolved = true
		}
		switch boolState(p, "supportsHttpsTrafficOnly") {
		case isFalse:
			a.addKey(r, "supportsHttpsTrafficOnly", "properties.supportsHttpsTrafficOnly is false")
		case expr:
			a.unresolved = true
		}
		v, st := str(p, "minimumTlsVersion")
		switch {
		case st == expr:
			a.unresolved = true
		case st == absent:
			a.addKey(r, "minimumTlsVersion", "properties.minimumTlsVersion is absent (older TLS versions may be accepted)")
		case st == isTrue && (strings.EqualFold(v, "TLS1_0") || strings.EqualFold(v, "TLS1_1")):
			a.addKey(r, "minimumTlsVersion", "properties.minimumTlsVersion is "+v)
		}
	}
	return a.result()
}

const sharedKeyRecommendation = "Set allowSharedKeyAccess false, allowBlobPublicAccess false, supportsHttpsTrafficOnly true and minimumTlsVersion TLS1_2 on the account, and give the Foundry identity Storage data roles. Verify no client still uses account keys or SAS signed with the account key before disabling Shared Key."

const keyAuthWarning = "Warning: a Foundry connection that may target this account uses authType AccountKey or SAS; disabling Shared Key access breaks it, so move that connection to Microsoft Entra ID authentication first."

// usesKeyAuth selects Foundry connections whose literal authType is AccountKey
// or SAS (the key-based authentication types of the connection schema).
func usesKeyAuth(r sdk.ARMResource) bool {
	if !strings.HasSuffix(strings.ToLower(r.Type), "/connections") {
		return false
	}
	v, _ := str(r.Properties, "authType")
	return strings.EqualFold(v, "AccountKey") || strings.EqualFold(v, "SAS")
}

// ipSpec describes where IP rules live per resource type (catalogue FND-SEC-006).
type ipSpec struct {
	typ      string
	path     []string // path to the rule array
	key      string   // element key holding the range
	private  []netip.Prefix
	openAlso []string // extra literal "open" values
}

func pfx(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(s))
	for _, x := range s {
		out = append(out, netip.MustParsePrefix(x))
	}
	return out
}

func ipSpecs() []ipSpec {
	return []ipSpec{
		{typ: typeStorage, path: []string{"networkAcls", "ipRules"}, key: "value",
			private: pfx("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")},
		{typ: typeCosmos, path: []string{"ipRules"}, key: "ipAddressOrRange",
			private:  pfx("10.0.0.0/8", "100.64.0.0/10", "172.16.0.0/12", "192.168.0.0/16"),
			openAlso: []string{"0.0.0.0"}},
		{typ: typeKeyVault, path: []string{"networkAcls", "ipRules"}, key: "value"},
		{typ: typeSearch, path: []string{"networkRuleSet", "ipRules"}, key: "value"},
		{typ: typeAccounts, path: []string{"networkAcls", "ipRules"}, key: "value"},
	}
}

func overlaps(r string, blocked []netip.Prefix) bool {
	p, err := netip.ParsePrefix(r)
	if err != nil {
		a, err2 := netip.ParseAddr(r)
		if err2 != nil {
			return false
		}
		p = netip.PrefixFrom(a, a.BitLen())
	}
	for _, b := range blocked {
		if b.Overlaps(p) {
			return true
		}
	}
	return false
}

func evalSEC006(in *sdk.Input) sdk.Result {
	var a acc
	seen := false
	set := foundrylink.Collect(in.ARM)
	for _, sp := range ipSpecs() {
		for _, r := range ofType(in, sp.typ) {
			// Only Foundry-related resources are in scope: a Foundry account
			// (kind AIServices) or a Cosmos DB, Key Vault, Search or Storage
			// resource that a Foundry connection or capability host links to.
			if sp.typ == typeAccounts {
				if !strings.EqualFold(r.Kind, "AIServices") {
					continue
				}
			} else if !linkOf(&a, set, r) {
				continue
			}
			seen = true
			if a.maybe(r) {
				continue
			}
			v, ok := get(r.Properties, sp.path...)
			if !ok {
				continue
			}
			if unresolved(v) {
				a.unresolved = true
				a.note(r, "ipRules is an unresolved expression")
				continue
			}
			list, _ := v.([]any)
			for _, e := range list {
				m, _ := e.(map[string]any)
				val, st := str(m, sp.key)
				if st == expr {
					a.unresolved = true
					a.note(r, "an ipRules value is an unresolved expression")
					continue
				}
				if st != isTrue {
					continue
				}
				val = strings.TrimSpace(val)
				allAddr := val == "0.0.0.0/0"
				azureWide := false
				for _, o := range sp.openAlso {
					azureWide = azureWide || val == o
				}
				switch {
				case allAddr:
					a.addKey(r, "ipRule:"+val, "IP rule "+val+" opens the resource to all addresses")
				case azureWide:
					a.addKey(r, "ipRule:"+val, "IP rule "+val+" allows all Azure datacenter IPs")
				case overlaps(val, sp.private):
					a.addKey(r, "ipRule:"+val, "IP rule "+val+" is a private or non-routable range that the service does not enforce")
				}
			}
		}
	}
	if !seen && !a.scopeUncertain && !a.conditional {
		if len(ofType(in, typeStorage))+len(ofType(in, typeCosmos))+len(ofType(in, typeKeyVault))+len(ofType(in, typeSearch)) > 0 {
			return skip(SkipOutOfScope)
		}
		return notApplicable()
	}
	return a.result()
}
