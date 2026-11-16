// Package env implements the Phase 1 FND-ENV-* rules. They are static checks
// over per-environment azd values; they never call Azure.
//
// A rule never passes on input it could not read. Environments whose .env file
// is missing, unreadable, over-size, truncated or unparseable, pairs that
// cannot be told apart, and environments without the values a check needs are
// reported as an uncertain skip that names them (see report.UncertainPrefix).
package env

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// TierPolicyKey is the policy key mapping environment name to tier
// (dev|test|prod). Environment tier is never inferred from the azd name.
const TierPolicyKey = "environments.tiers"

const fingerprintSalt = "foundry-doctor/env/v1"

// Status reports whether an environment's .env file could be used. Any status
// other than StatusOK means the environment's values are absent or partial, so
// no rule may treat the environment as having "no differences".
type Status string

// Environment value file statuses.
const (
	StatusOK          Status = "" // zero value: file read and parsed completely
	StatusMissing     Status = "missing"
	StatusUnreadable  Status = "unreadable"
	StatusOversize    Status = "over-size"
	StatusTruncated   Status = "truncated"
	StatusUnparseable Status = "unparseable"
)

// Environment is one azd environment's static view.
type Environment struct {
	Name          string
	Subscription  string
	ResourceGroup string
	// LiteralRGAt lists the azure.yaml locations whose resourceGroup is a
	// literal without variable reference, for example "resourceGroup" or
	// "services.web.resourceGroup".
	LiteralRGAt []string
	// Status is the state of the environment's .env file.
	Status Status
	Values map[string]string
	// Shapes holds the shape of values that were replaced by a digest. A key
	// absent here has its shape derived from the value.
	Shapes map[string]Shape
	// Origin says where the values came from. The zero value is the azd
	// environment's own .azure/<name>/.env. A non-zero origin is a source whose
	// mapping to an environment name is not an azd convention (UNVERIFIED), so
	// a rule never reports a finding that depends on it: it reports a note.
	Origin Origin
}

// Origin names the source of an Environment's values.
type Origin string

// Environment value origins.
const (
	OriginAzdEnv        Origin = ""               // .azure/<name>/.env
	OriginParameterFile Origin = "parameter-file" // infra/main.parameters.<name>.json or main.<name>.parameters.json
)

// soft reports whether a pair involving e can only give notes, not findings.
func (e Environment) soft() bool { return e.Origin != OriginAzdEnv }

func softPair(a, b Environment) bool { return a.soft() || b.soft() }

// Shape says what kind of non-secret identifier a value is. The app layer
// digests values under secret-looking key names before the rules see them; it
// classifies the value first and carries the shape next to the digest, so a
// Key Vault resource ID under AZURE_KEYVAULT_ID is treated like the same ID
// under AZURE_VAULT_ID. A value that is not one of these shapes stays plain.
type Shape string

// Value shapes.
const (
	ShapePlain           Shape = ""
	ShapeResourceID      Shape = "resource-id"
	ShapeFoundryEndpoint Shape = "foundry-endpoint" // https URL on *.services.ai.azure.com
	ShapeEndpoint        Shape = "endpoint"         // other https URL on *.azure.com or *.azure.net
)

// ClassifyShape returns the strict shape of v: an ARM resource ID, or a bare
// https URL with no user info, query or fragment. Anything else, including
// connection strings and SAS URLs, is plain.
func ClassifyShape(v string) Shape {
	t := strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(t), "/subscriptions/") && !strings.ContainsAny(t, " ;?=") {
		return ShapeResourceID
	}
	u, err := url.Parse(t)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return ShapePlain
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case strings.HasSuffix(h, ".services.ai.azure.com"):
		return ShapeFoundryEndpoint
	case strings.HasSuffix(h, ".azure.com"), strings.HasSuffix(h, ".azure.net"):
		return ShapeEndpoint
	}
	return ShapePlain
}

// shapeOf returns the shape of key k in e: the app-supplied shape when the
// value was digested, otherwise derived from the value.
func (e Environment) shapeOf(k, v string) Shape {
	if s, ok := e.Shapes[k]; ok {
		return s
	}
	l := strings.ToLower(v)
	switch {
	case strings.HasPrefix(l, "/subscriptions/"):
		return ShapeResourceID
	case strings.Contains(l, ".services.ai.azure.com"):
		return ShapeFoundryEndpoint
	case strings.Contains(l, ".azure.com"), strings.Contains(l, ".azure.net"):
		return ShapeEndpoint
	}
	return ShapePlain
}

func (e Environment) usable() bool { return e.Status == StatusOK }

// Provider supplies environments. Input does not carry them, so rules skip
// unless the coordinator supplies an implementation via RegisterWith.
type Provider interface {
	Environments() []Environment
}

// outcome is what a rule body returns: findings, plus notes naming what it
// could not decide. When there are no findings but there are notes, the rule
// is reported as an uncertain skip, never as a pass.
type outcome struct {
	findings []sdk.Finding
	notes    []string
	// skip, when set, is a complete skip reason (for example a missing policy key).
	skip string
}

type rule struct {
	id string
	p  Provider
	fn func(context.Context, *sdk.Input, []Environment) outcome
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if in == nil || r.p == nil {
		return skip(r.id), nil
	}
	all := r.p.Environments()
	if len(all) < 2 {
		// Nothing to compare: the project has fewer than two environments.
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: sdk.SkipNotApplicable + ": fewer than two environments with visible values" + declaredHint(in, all)}}, nil
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	var envs []Environment
	var notes []string
	for _, e := range all {
		if e.usable() {
			envs = append(envs, e)
			continue
		}
		label := ".env"
		if e.Origin == OriginParameterFile {
			label = "parameter file"
		}
		notes = append(notes, fmt.Sprintf("environment %s: %s %s", e.Name, label, e.Status))
	}
	if len(envs) < 2 {
		return uncertain(r.id, notes), nil
	}
	out := r.fn(ctx, in, envs)
	if out.skip != "" {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: out.skip}}, nil
	}
	notes = dedupe(append(notes, out.notes...))
	if len(out.findings) > 0 {
		// Notes ride next to the findings as uncertain skips; a finding never
		// hides what the rule could not decide (ADR-008 amendment 3).
		return sdk.Result{Findings: out.findings, Skips: uncertainSkips(r.id, notes)}, nil
	}
	if len(notes) > 0 {
		return uncertain(r.id, notes), nil
	}
	return sdk.Result{}, nil
}

// uncertainSkips builds one uncertain skip per note, capped like uncertain.
func uncertainSkips(id string, notes []string) []sdk.Skip {
	const limit = 8
	if len(notes) > limit {
		notes = append(notes[:limit:limit], fmt.Sprintf("and %d more", len(notes)-limit))
	}
	var out []sdk.Skip
	for _, n := range notes {
		out = append(out, sdk.Skip{RuleID: id, Reason: report.UncertainPrefix + "cannot decide: " + n})
	}
	return out
}

// uncertain builds a skip that readiness reports as uncertain. Notes name
// environments and keys only, never values.
func uncertain(id string, notes []string) sdk.Result {
	const limit = 8
	if len(notes) > limit {
		notes = append(notes[:limit:limit], fmt.Sprintf("and %d more", len(notes)-limit))
	}
	return sdk.Result{Skipped: &sdk.Skip{RuleID: id, Reason: report.UncertainPrefix + "cannot decide: " + strings.Join(notes, "; ")}}
}

func skip(id string) sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{RuleID: id, Reason: sdk.SkipInputUnavailable + ": no azd environment provider is wired for this run"}}
}

// Register returns rules with no provider; they skip until wired.
func Register() []sdk.Rule { return RegisterWith(nil) }

// RegisterWith returns the rules bound to p.
func RegisterWith(p Provider) []sdk.Rule {
	return []sdk.Rule{
		rule{"FND-ENV-001", p, eval001},
		rule{"FND-ENV-002", p, eval002},
		rule{"FND-ENV-003", p, eval003},
		rule{"FND-ENV-004", p, eval004},
	}
}

// Fingerprint is a salted, truncated hash; raw values are never reported.
func Fingerprint(v string) string {
	h := sha256.Sum256([]byte(fingerprintSalt + "\x00" + v))
	return hex.EncodeToString(h[:])[:12]
}

// finding builds a finding. key is the stable discriminator: check kind, then
// the variable name when there is one, then the sorted environment pair. It
// never contains a value or a value fingerprint, so a rotated value does not
// make a baselined finding reappear.
func finding(key, resource, evidence string) sdk.Finding {
	return sdk.Finding{Key: key, Resource: sdk.ResourceRef{Type: "azd-environment", Name: resource}, Evidence: evidence}
}

func eval001(_ context.Context, _ *sdk.Input, all []Environment) outcome {
	var out outcome
	// Subscription and resource group are azd .env values; a parameter-file
	// environment has neither, so it does not take part.
	var envs []Environment
	for _, e := range all {
		if !e.soft() {
			envs = append(envs, e)
		}
	}
	if len(envs) < 2 {
		out.notes = append(out.notes, "fewer than two environments with .env values (subscription and resource group come only from azd .env files)")
		return out
	}
	seen := map[string][]string{}
	var keys []string
	for _, e := range envs {
		if e.Subscription == "" || e.ResourceGroup == "" {
			out.notes = append(out.notes, fmt.Sprintf("environment %s: no AZURE_SUBSCRIPTION_ID or AZURE_RESOURCE_GROUP value", e.Name))
			continue
		}
		k := strings.ToLower(e.Subscription) + "/" + strings.ToLower(e.ResourceGroup)
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
		seen[k] = append(seen[k], e.Name)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if n := seen[k]; len(n) > 1 {
			out.findings = append(out.findings, finding("shared-subscription-resource-group:"+strings.Join(n, ","), strings.Join(n, ","), fmt.Sprintf("environments %s share one subscription and resource group", strings.Join(n, ", "))))
		}
	}
	for _, e := range envs {
		var services []string
		for _, at := range e.LiteralRGAt {
			if at != "resourceGroup" {
				services = append(services, at)
			}
		}
		if len(services) < len(e.LiteralRGAt) {
			out.findings = append(out.findings, finding("literal-resource-group:"+e.Name, e.Name, "azure.yaml pins a literal resourceGroup (resourceGroup) while multiple environments exist"))
		}
		if len(services) > 0 {
			// The azd schema describes a service-level resourceGroup as the group
			// that contains the resource; no source says a literal value breaks
			// environment isolation, so this is uncertain, not an error.
			out.notes = append(out.notes, fmt.Sprintf("azure.yaml sets a literal %s; the azd schema does not say whether that defeats environment isolation", strings.Join(services, ", ")))
		}
	}
	// A service-level literal is identical in every environment; say it once.
	out.notes = dedupe(out.notes)
	return out
}

// Classes of key for FND-ENV-002.
const (
	classNone   = iota
	classStrong // resource ID, endpoint, subscription or resource group
	classWeak   // _NAME or _ID suffix only: a heuristic, reported as uncertain
)

// excludedFromBound lists name fragments of keys that identify tenants,
// identities or model deployments. Those are shared on purpose far more often
// than not, so they are never reported.
var excludedFromBound = []string{"TENANT", "PRINCIPAL", "DEPLOYMENT", "MODEL"}

func classify(k string, shape Shape) int {
	u := strings.ToUpper(k)
	switch {
	case k == "AZURE_ENV_NAME":
		return classNone
	case u == "AZURE_SUBSCRIPTION_ID":
		return classStrong
	case shape == ShapeResourceID, shape == ShapeFoundryEndpoint:
		return classStrong
	case strings.HasSuffix(u, "_ENDPOINT"), strings.HasSuffix(u, "_RESOURCE_GROUP"):
		return classStrong
	case strings.HasSuffix(u, "_NAME"), strings.HasSuffix(u, "_ID"):
		for _, x := range excludedFromBound {
			if strings.Contains(u, x) {
				return classNone
			}
		}
		return classWeak
	}
	return classNone
}

// softNote explains why a would-be finding is only a note: one of the pair's
// values come from a source whose mapping to an environment name is not an azd
// convention. Names and counts only, never values.
func softNote(a, b Environment, what string, keys []string) string {
	const top = 5
	sort.Strings(keys)
	more := ""
	if len(keys) > top {
		more = fmt.Sprintf(" and %d more", len(keys)-top)
		keys = keys[:top]
	}
	return fmt.Sprintf("environments %s and %s: %s (%s%s), but the values of %s come from a %s, and azd defines no environment-specific parameter file naming (UNVERIFIED), so this is not reported as a finding",
		a.Name, b.Name, what, strings.Join(keys, ", "), more, softName(a, b), softOrigin(a, b))
}

func softName(a, b Environment) string {
	switch {
	case a.soft() && b.soft():
		return a.Name + " and " + b.Name
	case a.soft():
		return a.Name
	}
	return b.Name
}

func softOrigin(a, b Environment) string {
	if a.soft() {
		return string(a.Origin)
	}
	return string(b.Origin)
}

func eval002(_ context.Context, _ *sdk.Input, envs []Environment) outcome {
	var out outcome
	for i := 0; i < len(envs); i++ {
		for j := i + 1; j < len(envs); j++ {
			a, b := envs[i], envs[j]
			na, nb := a.Values["AZURE_ENV_NAME"], b.Values["AZURE_ENV_NAME"]
			if na == "" || nb == "" || na == nb {
				out.notes = append(out.notes, fmt.Sprintf("environments %s and %s: AZURE_ENV_NAME is empty or equal, so a copied value cannot be told from a shared one", a.Name, b.Name))
				continue
			}
			if softPair(a, b) {
				// Parameter names and .env keys are different namespaces, so a
				// shared identifier is matched by value, not by key.
				if keys := sharedIdentifierKeys(a, b); len(keys) > 0 {
					out.notes = append(out.notes, softNote(a, b, fmt.Sprintf("%d identifier value(s) are identical", len(keys)), keys))
				}
				continue
			}
			var ks []string
			for k := range a.Values {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			for _, k := range ks {
				va, ok := b.Values[k]
				if !ok || va == "" || va != a.Values[k] {
					continue
				}
				switch classify(k, a.shapeOf(k, va)) {
				case classStrong:
					out.findings = append(out.findings, finding("identical-value:"+k+":"+envPair(a.Name, b.Name), k, fmt.Sprintf("key %s has identical value (fingerprint %s) in environments %s and %s", k, Fingerprint(va), a.Name, b.Name)))
				case classWeak:
					out.notes = append(out.notes, fmt.Sprintf("key %s has identical value (fingerprint %s) in %s and %s; a _NAME or _ID key is not known to be environment-bound", k, Fingerprint(va), a.Name, b.Name))
				}
			}
		}
	}
	return out
}

// sharedIdentifierKeys returns the key names in a whose identifier-shaped
// value (resource ID or Azure endpoint) also appears under any key in b.
func sharedIdentifierKeys(a, b Environment) []string {
	inB := map[string]bool{}
	for k, v := range b.Values {
		if b.shapeOf(k, v) != ShapePlain {
			inB[norm(v)] = true
		}
	}
	var keys []string
	for k, v := range a.Values {
		if a.shapeOf(k, v) != ShapePlain && inB[norm(v)] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func tiers(in *sdk.Input) (map[string]string, bool) {
	if in == nil || in.Policy == nil {
		return nil, false
	}
	v, ok := in.Policy.Get(TierPolicyKey)
	if !ok {
		return nil, false
	}
	out := map[string]string{}
	switch m := v.(type) {
	case map[string]string:
		for k, t := range m {
			out[k] = strings.ToLower(t)
		}
	case map[string]any:
		for k, t := range m {
			if s, ok := t.(string); ok {
				out[k] = strings.ToLower(s)
			}
		}
	default:
		return nil, false
	}
	return out, len(out) > 0
}

// envPair names two environments in sorted order.
func envPair(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return a + "," + b
}

// targetVar names the variable that carries the normalised target value k in e
// (the lexicographically smallest, so the choice is stable).
func targetVar(e Environment, k string) string {
	if norm(e.ResourceGroup) == k {
		return "AZURE_RESOURCE_GROUP"
	}
	best := ""
	for name, v := range e.Values {
		if e.shapeOf(name, v) != ShapePlain && norm(v) == k && (best == "" || name < best) {
			best = name
		}
	}
	return best
}

func norm(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func targets(e Environment) map[string]string {
	m := map[string]string{}
	add := func(kind, v string) {
		if n := norm(v); n != "" {
			m[n] = kind
		}
	}
	add("resource group", e.ResourceGroup)
	for k, v := range e.Values {
		if e.shapeOf(k, v) != ShapePlain {
			add("resource identifier", v)
		}
	}
	return m
}

// eval003 needs no policy key: the tier of an environment comes from
// environments.tiers (wins), then the environments.production / nonProduction /
// development lists, then the documented name derivation (DerivedTierDoc).
// When the derivation was used and the rule reports findings, a note names the
// derived tiers so the classification is visible.
func eval003(_ context.Context, in *sdk.Input, envs []Environment) outcome {
	t := resolveTiers(in, envs)
	var out outcome
	prods, others := 0, 0
	var derived []string
	for _, e := range envs {
		ti, ok := t[e.Name]
		switch {
		case !ok:
			out.notes = append(out.notes, fmt.Sprintf("environment %s: no tier in %s or the environments lists, and the name does not indicate one", e.Name, TierPolicyKey))
			continue
		case ti.tier == tierProd:
			prods++
		default:
			others++
		}
		if ti.derived {
			derived = append(derived, e.Name+"="+ti.tier)
		}
	}
	if prods == 0 {
		out.notes = append(out.notes, "no visible environment is configured as prod")
	}
	if others == 0 {
		out.notes = append(out.notes, "no visible environment is configured as non-prod")
	}
	for _, p := range envs {
		if t[p.Name].tier != tierProd {
			continue
		}
		pt := targets(p)
		if len(pt) == 0 {
			out.notes = append(out.notes, fmt.Sprintf("environment %s: no resource group or resource identifier values", p.Name))
			continue
		}
		for _, o := range envs {
			if o.Name == p.Name || t[o.Name].tier == "" || t[o.Name].tier == tierProd {
				continue
			}
			ot := targets(o)
			if len(ot) == 0 {
				out.notes = append(out.notes, fmt.Sprintf("environment %s: no resource group or resource identifier values", o.Name))
				continue
			}
			var ks []string
			for k := range pt {
				if _, hit := ot[k]; hit {
					ks = append(ks, k)
				}
			}
			sort.Strings(ks)
			if softPair(p, o) && len(ks) > 0 {
				names := make([]string, 0, len(ks))
				for _, k := range ks {
					names = append(names, targetVar(p, k))
				}
				out.notes = append(out.notes, softNote(p, o, fmt.Sprintf("prod environment %s shares %d target(s) with non-prod environment %s", p.Name, len(ks), o.Name), names))
				continue
			}
			for _, k := range ks {
				ev := fmt.Sprintf("prod environment %s shares %s (fingerprint %s) with non-prod environment %s", p.Name, pt[k], Fingerprint(k), o.Name)
				if t[p.Name].derived || t[o.Name].derived {
					ev += " (a tier was derived from the environment name)"
				}
				out.findings = append(out.findings, finding("prod-shares-target:"+pt[k]+":"+targetVar(p, k)+":"+envPair(p.Name, o.Name), p.Name, ev))
			}
		}
	}
	if len(derived) > 0 && len(out.findings) > 0 {
		sort.Strings(derived)
		out.notes = append(out.notes, fmt.Sprintf("tiers of %s were not set in policy; %s", strings.Join(derived, ", "), DerivedTierDoc))
	}
	out.notes = dedupe(out.notes)
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// MaxDiffKeys is the number of differing keys named in one FND-ENV-004 summary.
const MaxDiffKeys = 10

// eval004 emits at most one summary finding per environment pair (key
// "env-diff-summary:<a>,<b>", stable per pair): the counts of keys only in
// either environment and of keys with different values, and the first
// MaxDiffKeys differing keys in name order. Values and fingerprints are not
// printed, so a rotated value does not change the finding.
func eval004(_ context.Context, _ *sdk.Input, all []Environment) outcome {
	var out outcome
	// Parameter-file environments carry parameter names, not azd .env keys, so a
	// key-level difference against them is meaningless; they are not compared.
	var envs []Environment
	for _, e := range all {
		if !e.soft() {
			envs = append(envs, e)
		}
	}
	if len(envs) < 2 {
		out.notes = append(out.notes, "fewer than two environments with azd .env values to compare")
		return out
	}
	for i := 0; i < len(envs); i++ {
		for j := i + 1; j < len(envs); j++ {
			a, b := envs[i], envs[j]
			set := map[string]bool{}
			for k := range a.Values {
				set[k] = true
			}
			for k := range b.Values {
				set[k] = true
			}
			var ks []string
			for k := range set {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			var onlyA, onlyB, differs int
			var listed []string
			add := func(s string) {
				if len(listed) < MaxDiffKeys {
					listed = append(listed, s)
				}
			}
			for _, k := range ks {
				va, ina := a.Values[k]
				vb, inb := b.Values[k]
				switch {
				case ina && !inb:
					onlyA++
					add(fmt.Sprintf("%s (only in %s)", k, a.Name))
				case inb && !ina:
					onlyB++
					add(fmt.Sprintf("%s (only in %s)", k, b.Name))
				case va != vb:
					differs++
					add(fmt.Sprintf("%s (differs between %s and %s)", k, a.Name, b.Name))
				}
			}
			total := onlyA + onlyB + differs
			if total == 0 {
				continue
			}
			more := ""
			if total > len(listed) {
				more = fmt.Sprintf(" and %d more", total-len(listed))
			}
			ev := fmt.Sprintf("environments %s and %s differ in %d key(s): %d only in %s, %d only in %s, %d with different values; first %d: %s%s",
				a.Name, b.Name, total, onlyA, a.Name, onlyB, b.Name, differs, len(listed), strings.Join(listed, ", "), more)
			out.findings = append(out.findings, finding("env-diff-summary:"+envPair(a.Name, b.Name), envPair(a.Name, b.Name), ev))
		}
	}
	return out
}
