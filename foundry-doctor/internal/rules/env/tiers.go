package env

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Policy keys that classify environment names (internal/config, ADR-007).
const (
	policyProduction    = "environments.production"
	policyNonProduction = "environments.nonProduction"
	policyDevelopment   = "environments.development"
)

// Tier values. tierNonProd is "some tier that is not prod" (environments listed
// under environments.nonProduction, or named like a pre-production stage).
const (
	tierProd    = "prod"
	tierNonProd = "nonprod"
)

// Name tokens that derive a tier when the policy does not classify an
// environment. This mapping is a Foundry Doctor product opinion, not an azd or
// Azure fact: azd treats an environment name as an opaque label. A name is
// split into tokens at every character that is not a letter or digit, and each
// token is looked up here ("contoso-prod-eu" has the token "prod").
var (
	prodTokens    = map[string]bool{"prod": true, "production": true, "prd": true}
	nonProdTokens = map[string]bool{"dev": true, "development": true, "test": true, "testing": true, "stage": true, "staging": true, "uat": true, "qa": true}
)

// DerivedTierDoc is the documented derivation, shown in notes.
const DerivedTierDoc = "tier derived from the environment name tokens (prod, production, prd are prod; dev, development, test, testing, stage, staging, uat, qa are non-prod; a prod token after pre or non is non-prod; a name with both kinds is not classified); set " + TierPolicyKey + " to override"

// tierFromName derives a tier from an environment name; "" when the name does
// not decide one.
func tierFromName(name string) string {
	tokens := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	prod, nonProd := false, false
	for i, t := range tokens {
		switch {
		case prodTokens[t]:
			// "pre-prod" and "non-prod" are not production.
			if i > 0 && (tokens[i-1] == "pre" || tokens[i-1] == "non") {
				nonProd = true
			} else {
				prod = true
			}
		case nonProdTokens[t]:
			nonProd = true
		}
	}
	switch {
	case prod && !nonProd:
		return tierProd
	case nonProd && !prod:
		return tierNonProd
	}
	return ""
}

// tierInfo is the resolved tier of one environment and where it came from.
type tierInfo struct {
	tier    string
	derived bool
}

// policyList returns a string list policy value.
func policyList(in *sdk.Input, key string) []string {
	if in == nil || in.Policy == nil {
		return nil
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return nil
	}
	var out []string
	switch xs := v.(type) {
	case []string:
		out = append(out, xs...)
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// resolveTiers classifies each environment. Precedence, per environment: an
// explicit environments.tiers entry, then the environments.production /
// nonProduction / development lists, then the name derivation. An explicit
// policy entry always wins over the derivation.
func resolveTiers(in *sdk.Input, envs []Environment) map[string]tierInfo {
	explicit, _ := tiers(in)
	listed := map[string]string{}
	for _, n := range policyList(in, policyProduction) {
		listed[strings.ToLower(n)] = tierProd
	}
	for _, n := range policyList(in, policyNonProduction) {
		listed[strings.ToLower(n)] = tierNonProd
	}
	for _, n := range policyList(in, policyDevelopment) {
		listed[strings.ToLower(n)] = "dev"
	}
	out := map[string]tierInfo{}
	for _, e := range envs {
		if t := explicit[e.Name]; t != "" {
			out[e.Name] = tierInfo{tier: t}
			continue
		}
		if t := listed[strings.ToLower(e.Name)]; t != "" {
			out[e.Name] = tierInfo{tier: t}
			continue
		}
		if t := tierFromName(e.Name); t != "" {
			out[e.Name] = tierInfo{tier: t, derived: true}
		}
	}
	return out
}

// declaredHint names the environments the policy declares (tiers keys and the
// production, nonProduction and development lists) that have no visible
// values, so a not-applicable skip says what to provide instead of only
// "fewer than two".
func declaredHint(in *sdk.Input, visible []Environment) string {
	have := map[string]bool{}
	for _, e := range visible {
		have[strings.ToLower(e.Name)] = true
	}
	declared := map[string]bool{}
	for _, k := range []string{policyProduction, policyNonProduction, policyDevelopment} {
		for _, n := range policyList(in, k) {
			declared[n] = true
		}
	}
	t, _ := tiers(in)
	for n := range t {
		declared[n] = true
	}
	var missing []string
	for n := range declared {
		if !have[strings.ToLower(n)] && n != "" {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)
	more := ""
	if len(missing) > 8 {
		more = fmt.Sprintf(" and %d more", len(missing)-8)
		missing = missing[:8]
	}
	return fmt.Sprintf("; the policy declares environments without visible values: %s%s (azd .env files are normally git-ignored; run where .azure/<env>/.env exist)", strings.Join(missing, ", "), more)
}
