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

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SkipOutOfScope is the skip reason used when resources exist but none is in
// Foundry scope (FND-SEC-002, FND-SEC-003).
const SkipOutOfScope = "out-of-foundry-scope"

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

// Register returns the Phase 1 SEC rules in ID order. FND-SEC-014 is owned by
// internal/bicep and is deliberately not registered here.
func Register() []sdk.Rule {
	return []sdk.Rule{
		fn{"FND-SEC-001", evalSEC001},
		fn{"FND-SEC-002", evalSEC002},
		fn{"FND-SEC-003", evalSEC003},
		fn{"FND-SEC-004", evalSEC004},
		fn{"FND-SEC-006", evalSEC006},
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

func unavailable() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
}

func skip(reason string) sdk.Result { return sdk.Result{Skipped: &sdk.Skip{Reason: reason}} }

// acc accumulates findings and notes unresolved decisive values.
type acc struct {
	fs         []sdk.Finding
	unresolved bool
}

func (a *acc) add(r sdk.ARMResource, evidence string) {
	a.fs = append(a.fs, sdk.Finding{
		Resource: sdk.ResourceRef{Type: r.Type, Name: r.Name},
		Location: r.Location,
		Evidence: evidence,
	})
}

// result: findings win; otherwise unresolved values skip; otherwise pass.
func (a *acc) result() sdk.Result {
	if len(a.fs) > 0 {
		return sdk.Result{Findings: a.fs}
	}
	if a.unresolved {
		return skip(SkipUnresolved)
	}
	return sdk.Result{}
}

func ofType(in *sdk.Input, t string) []sdk.ARMResource {
	var out []sdk.ARMResource
	for _, r := range in.ARM.Resources() {
		if strings.EqualFold(r.Type, t) {
			out = append(out, r)
		}
	}
	return out
}

// unresolved reports an ARM language expression ("[...]" but not "[[").
func unresolved(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "[[")
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

// parentName returns the part of a child resource name before the first "/".
func parentName(child string) string {
	if i := strings.Index(child, "/"); i >= 0 {
		return child[:i]
	}
	return ""
}

// ownsChild reports whether account owns a child resource of the given types.
// Names match by "parent/child" prefix; when a name is an unresolved
// expression the match falls back to the sole-account assumption.
func ownsChild(in *sdk.Input, account sdk.ARMResource, accounts int, types ...string) bool {
	for _, t := range types {
		for _, c := range ofType(in, t) {
			p := parentName(c.Name)
			switch {
			case p != "" && strings.EqualFold(p, account.Name):
				return true
			case (strings.HasPrefix(c.Name, "[") || strings.HasPrefix(account.Name, "[")) && accounts == 1:
				return true
			}
		}
	}
	return false
}

func evalSEC001(in *sdk.Input) sdk.Result {
	accounts := ofType(in, typeAccounts)
	var a acc
	seen := false
	for _, r := range accounts {
		if !ownsChild(in, r, len(accounts), typeAcctProject, typeAcctDeploy) {
			continue
		}
		seen = true
		switch boolState(r.Properties, "disableLocalAuth") {
		case isTrue:
		case expr:
			a.unresolved = true
		case absent:
			a.add(r, "Foundry account owns projects or deployments; properties.disableLocalAuth is absent (key authentication defaults to enabled)")
		default:
			a.add(r, "Foundry account owns projects or deployments; properties.disableLocalAuth is not true")
		}
	}
	if !seen {
		return unavailable()
	}
	return a.result()
}

// foundryRefs collects lower-cased strings from Foundry connections and
// capability hosts so external services can be tied to Foundry.
func foundryRefs(in *sdk.Input) (refs []string, found bool) {
	prefix := strings.ToLower(typeAccounts)
	for _, r := range in.ARM.Resources() {
		t := strings.ToLower(r.Type)
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		if !strings.HasSuffix(t, "/connections") && !strings.HasSuffix(t, "/capabilityhosts") {
			continue
		}
		found = true
		flatten(r.Properties, &refs)
	}
	sort.Strings(refs)
	return refs, found
}

func flatten(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, strings.ToLower(x))
	case []any:
		for _, e := range x {
			flatten(e, out)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			flatten(x[k], out)
		}
	}
}

func inFoundryScope(r sdk.ARMResource, refs []string, anyRef bool) bool {
	if !anyRef {
		return false
	}
	if strings.HasPrefix(r.Name, "[") {
		return true // cannot correlate; a Foundry connection exists
	}
	n := strings.ToLower(r.Name)
	for _, s := range refs {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func evalSEC002(in *sdk.Input) sdk.Result {
	svcs := ofType(in, typeSearch)
	if len(svcs) == 0 {
		return unavailable()
	}
	refs, anyRef := foundryRefs(in)
	var a acc
	scoped := false
	for _, r := range svcs {
		if !inFoundryScope(r, refs, anyRef) {
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
		a.add(r, "Search service referenced by Foundry; properties.disableLocalAuth is not true; "+sub)
	}
	if !scoped {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC003(in *sdk.Input) sdk.Result {
	dbs := ofType(in, typeCosmos)
	if len(dbs) == 0 {
		return unavailable()
	}
	refs, anyRef := foundryRefs(in)
	var a acc
	scoped := false
	for _, r := range dbs {
		if !inFoundryScope(r, refs, anyRef) {
			continue
		}
		scoped = true
		switch boolState(r.Properties, "disableLocalAuth") {
		case isTrue:
		case expr:
			a.unresolved = true
		default:
			a.add(r, "Cosmos DB account referenced by Foundry; properties.disableLocalAuth is not true")
		}
	}
	if !scoped {
		return skip(SkipOutOfScope)
	}
	return a.result()
}

func evalSEC004(in *sdk.Input) sdk.Result {
	stores := ofType(in, typeStorage)
	if len(stores) == 0 {
		return unavailable()
	}
	var a acc
	for _, r := range stores {
		p := r.Properties
		switch boolState(p, "allowSharedKeyAccess") {
		case isFalse:
		case expr:
			a.unresolved = true
		default:
			a.add(r, "properties.allowSharedKeyAccess is not false (shared key access enabled)")
		}
		switch boolState(p, "allowBlobPublicAccess") {
		case isTrue:
			a.add(r, "properties.allowBlobPublicAccess is true (anonymous blob access allowed)")
		case expr:
			a.unresolved = true
		}
		switch boolState(p, "supportsHttpsTrafficOnly") {
		case isFalse:
			a.add(r, "properties.supportsHttpsTrafficOnly is false")
		case expr:
			a.unresolved = true
		}
		v, st := str(p, "minimumTlsVersion")
		switch {
		case st == expr:
			a.unresolved = true
		case st == absent:
			a.add(r, "properties.minimumTlsVersion is absent (older TLS versions may be accepted)")
		case st == isTrue && (strings.EqualFold(v, "TLS1_0") || strings.EqualFold(v, "TLS1_1")):
			a.add(r, "properties.minimumTlsVersion is "+v)
		}
	}
	return a.result()
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
	for _, sp := range ipSpecs() {
		for _, r := range ofType(in, sp.typ) {
			seen = true
			v, ok := get(r.Properties, sp.path...)
			if !ok {
				continue
			}
			if unresolved(v) {
				a.unresolved = true
				continue
			}
			list, _ := v.([]any)
			for _, e := range list {
				m, _ := e.(map[string]any)
				val, st := str(m, sp.key)
				if st == expr {
					a.unresolved = true
					continue
				}
				if st != isTrue {
					continue
				}
				val = strings.TrimSpace(val)
				open := val == "0.0.0.0/0"
				for _, o := range sp.openAlso {
					open = open || val == o
				}
				switch {
				case open:
					a.add(r, "IP rule "+val+" opens the resource to all addresses")
				case overlaps(val, sp.private):
					a.add(r, "IP rule "+val+" is a private or non-routable range that the service does not enforce")
				}
			}
		}
	}
	if !seen {
		return unavailable()
	}
	return a.result()
}
