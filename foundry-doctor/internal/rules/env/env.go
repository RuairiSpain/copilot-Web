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
		return skip(r.id), nil
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	var envs []Environment
	var notes []string
	for _, e := range all {
		if e.usable() {
			envs = append(envs, e)
			continue
		}
		notes = append(notes, fmt.Sprintf("environment %s: .env %s", e.Name, e.Status))
	}
	if len(envs) < 2 {
		return uncertain(r.id, notes), nil
	}
	out := r.fn(ctx, in, envs)
	if out.skip != "" {
		return sdk.Result{Skipped: &sdk.Skip{RuleID: r.id, Reason: out.skip}}, nil
	}
	if len(out.findings) > 0 {
		return sdk.Result{Findings: out.findings}, nil
	}
	notes = append(notes, out.notes...)
	if len(notes) > 0 {
		return uncertain(r.id, notes), nil
	}
	return sdk.Result{}, nil
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
	return sdk.Result{Skipped: &sdk.Skip{RuleID: id, Reason: sdk.SkipInputUnavailable}}
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

func finding(resource, evidence string) sdk.Finding {
	return sdk.Finding{Resource: sdk.ResourceRef{Type: "azd-environment", Name: resource}, Evidence: evidence}
}

func eval001(_ context.Context, _ *sdk.Input, envs []Environment) outcome {
	var out outcome
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
			out.findings = append(out.findings, finding(strings.Join(n, ","), fmt.Sprintf("environments %s share one subscription and resource group", strings.Join(n, ", "))))
		}
	}
	for _, e := range envs {
		if len(e.LiteralRGAt) > 0 {
			out.findings = append(out.findings, finding(e.Name, fmt.Sprintf("azure.yaml pins a literal resourceGroup (%s) while multiple environments exist", strings.Join(e.LiteralRGAt, ", "))))
		}
	}
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

func classify(k, v string) int {
	u := strings.ToUpper(k)
	l := strings.ToLower(v)
	switch {
	case k == "AZURE_ENV_NAME":
		return classNone
	case u == "AZURE_SUBSCRIPTION_ID":
		return classStrong
	case strings.HasPrefix(l, "/subscriptions/"), strings.Contains(l, ".services.ai.azure.com"):
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
				switch classify(k, va) {
				case classStrong:
					out.findings = append(out.findings, finding(k, fmt.Sprintf("key %s has identical value (fingerprint %s) in environments %s and %s", k, Fingerprint(va), a.Name, b.Name)))
				case classWeak:
					out.notes = append(out.notes, fmt.Sprintf("key %s has identical value (fingerprint %s) in %s and %s; a _NAME or _ID key is not known to be environment-bound", k, Fingerprint(va), a.Name, b.Name))
				}
			}
		}
	}
	return out
}

func tiers(in *sdk.Input) (map[string]string, bool) {
	if in.Policy == nil {
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

func norm(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func targets(e Environment) map[string]string {
	m := map[string]string{}
	add := func(kind, v string) {
		if n := norm(v); n != "" {
			m[n] = kind
		}
	}
	add("resource group", e.ResourceGroup)
	for _, v := range e.Values {
		l := norm(v)
		if strings.HasPrefix(l, "/subscriptions/") || strings.Contains(l, ".azure.com") || strings.Contains(l, ".azure.net") {
			add("resource identifier", v)
		}
	}
	return m
}

func eval003(_ context.Context, in *sdk.Input, envs []Environment) outcome {
	t, ok := tiers(in)
	if !ok {
		return outcome{skip: sdk.SkipMissingPolicyKey(TierPolicyKey)}
	}
	var out outcome
	prods, others := 0, 0
	for _, e := range envs {
		switch t[e.Name] {
		case "prod":
			prods++
		case "":
			out.notes = append(out.notes, fmt.Sprintf("environment %s: no tier in %s", e.Name, TierPolicyKey))
		default:
			others++
		}
	}
	if prods == 0 {
		out.notes = append(out.notes, "no visible environment is configured as prod")
	}
	if others == 0 {
		out.notes = append(out.notes, "no visible environment is configured as non-prod")
	}
	for _, p := range envs {
		if t[p.Name] != "prod" {
			continue
		}
		pt := targets(p)
		if len(pt) == 0 {
			out.notes = append(out.notes, fmt.Sprintf("environment %s: no resource group or resource identifier values", p.Name))
			continue
		}
		for _, o := range envs {
			if o.Name == p.Name || t[o.Name] == "" || t[o.Name] == "prod" {
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
			for _, k := range ks {
				out.findings = append(out.findings, finding(p.Name, fmt.Sprintf("prod environment %s shares %s (fingerprint %s) with non-prod environment %s", p.Name, pt[k], Fingerprint(k), o.Name)))
			}
		}
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

func secretKey(k string) bool {
	u := strings.ToUpper(k)
	for _, s := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD", "PWD", "CONNECTIONSTRING"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

func eval004(_ context.Context, _ *sdk.Input, envs []Environment) outcome {
	var out outcome
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
			for _, k := range ks {
				va, ina := a.Values[k]
				vb, inb := b.Values[k]
				var ev string
				switch {
				case ina && !inb:
					ev = fmt.Sprintf("key %s only in %s", k, a.Name)
				case inb && !ina:
					ev = fmt.Sprintf("key %s only in %s", k, b.Name)
				case va != vb && secretKey(k):
					ev = fmt.Sprintf("key %s differs between %s and %s", k, a.Name, b.Name)
				case va != vb:
					ev = fmt.Sprintf("key %s differs between %s (%s) and %s (%s)", k, a.Name, Fingerprint(va), b.Name, Fingerprint(vb))
				default:
					continue
				}
				out.findings = append(out.findings, finding(k, ev))
			}
		}
	}
	return out
}
