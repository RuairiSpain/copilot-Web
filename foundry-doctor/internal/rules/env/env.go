// Package env implements the Phase 1 FND-ENV-* rules. They are static checks
// over per-environment azd values; they never call Azure.
package env

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// TierPolicyKey is the policy key mapping environment name to tier
// (dev|test|prod). Environment tier is never inferred from the azd name.
const TierPolicyKey = "environments.tiers"

const fingerprintSalt = "foundry-doctor/env/v1"

// Environment is one azd environment's static view.
type Environment struct {
	Name            string
	Subscription    string
	ResourceGroup   string
	LiteralRGInYAML bool // azure.yaml pins resourceGroup literally
	Values          map[string]string
}

// Provider supplies environments. Input does not carry them, so rules skip
// unless the coordinator supplies an implementation via RegisterWith.
type Provider interface {
	Environments() []Environment
}

type rule struct {
	id string
	p  Provider
	fn func(context.Context, *sdk.Input, []Environment) sdk.Result
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if in == nil || r.p == nil {
		return skip(r.id), nil
	}
	envs := r.p.Environments()
	if len(envs) < 2 {
		return skip(r.id), nil
	}
	sort.SliceStable(envs, func(i, j int) bool { return envs[i].Name < envs[j].Name })
	res := r.fn(ctx, in, envs)
	if res.Skipped != nil {
		res.Skipped.RuleID = r.id
	}
	return res, nil
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

func eval001(_ context.Context, _ *sdk.Input, envs []Environment) sdk.Result {
	var fs []sdk.Finding
	seen := map[string][]string{}
	var keys []string
	for _, e := range envs {
		if e.Subscription == "" || e.ResourceGroup == "" {
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
			fs = append(fs, finding(strings.Join(n, ","), fmt.Sprintf("environments %s share one subscription and resource group", strings.Join(n, ", "))))
		}
	}
	for _, e := range envs {
		if e.LiteralRGInYAML {
			fs = append(fs, finding(e.Name, "azure.yaml pins a literal resourceGroup while multiple environments exist"))
		}
	}
	return sdk.Result{Findings: fs}
}

func envBound(k, v string) bool {
	u := strings.ToUpper(k)
	l := strings.ToLower(v)
	switch {
	case k == "AZURE_ENV_NAME":
		return false
	case u == "AZURE_SUBSCRIPTION_ID":
		return true
	case strings.HasPrefix(l, "/subscriptions/"), strings.Contains(l, ".services.ai.azure.com"):
		return true
	}
	for _, s := range []string{"_NAME", "_ENDPOINT", "_ID", "_RESOURCE_GROUP"} {
		if strings.HasSuffix(u, s) {
			return true
		}
	}
	return false
}

func eval002(_ context.Context, _ *sdk.Input, envs []Environment) sdk.Result {
	var fs []sdk.Finding
	for i := 0; i < len(envs); i++ {
		for j := i + 1; j < len(envs); j++ {
			a, b := envs[i], envs[j]
			if a.Values["AZURE_ENV_NAME"] == b.Values["AZURE_ENV_NAME"] {
				continue
			}
			var ks []string
			for k := range a.Values {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			for _, k := range ks {
				va, ok := b.Values[k]
				if !ok || va == "" || va != a.Values[k] || !envBound(k, va) {
					continue
				}
				fs = append(fs, finding(k, fmt.Sprintf("key %s has identical value (fingerprint %s) in environments %s and %s", k, Fingerprint(va), a.Name, b.Name)))
			}
		}
	}
	return sdk.Result{Findings: fs}
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

func eval003(_ context.Context, in *sdk.Input, envs []Environment) sdk.Result {
	t, ok := tiers(in)
	if !ok {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipMissingPolicyKey(TierPolicyKey)}}
	}
	var fs []sdk.Finding
	for _, p := range envs {
		if t[p.Name] != "prod" {
			continue
		}
		pt := targets(p)
		for _, o := range envs {
			if o.Name == p.Name || t[o.Name] == "" || t[o.Name] == "prod" {
				continue
			}
			ot := targets(o)
			var ks []string
			for k := range pt {
				if _, hit := ot[k]; hit {
					ks = append(ks, k)
				}
			}
			sort.Strings(ks)
			for _, k := range ks {
				fs = append(fs, finding(p.Name, fmt.Sprintf("prod environment %s shares %s (fingerprint %s) with non-prod environment %s", p.Name, pt[k], Fingerprint(k), o.Name)))
			}
		}
	}
	return sdk.Result{Findings: fs}
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

func eval004(_ context.Context, _ *sdk.Input, envs []Environment) sdk.Result {
	var fs []sdk.Finding
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
				fs = append(fs, finding(k, ev))
			}
		}
	}
	return sdk.Result{Findings: fs}
}
