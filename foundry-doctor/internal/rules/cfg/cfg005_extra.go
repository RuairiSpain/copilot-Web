package cfg

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// credentialWords are the final name tokens that name a credential itself
// (DB_PASSWORD, CLIENT_SECRET, AUTH_TOKEN). KEY alone is excluded because
// PARTITION_KEY, SORT_KEY and CACHE_KEY are not credentials; the pairs below
// cover the key forms that are.
var (
	credentialWords = map[string]bool{"PASSWORD": true, "PASSWD": true, "PWD": true, "SECRET": true,
		"TOKEN": true, "APIKEY": true, "ACCESSKEY": true, "ACCOUNTKEY": true, "SHAREDKEY": true}
	credentialKeyPrefixes = map[string]bool{"API": true, "ACCESS": true, "ACCOUNT": true, "SHARED": true,
		"PRIMARY": true, "SECONDARY": true}
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// nameTokens splits a name on _ - . and camelCase boundaries into upper-case
// tokens.
func nameTokens(name string) []string {
	name = camelBoundary.ReplaceAllString(name, "${1}_${2}")
	return strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
}

// endsInCredentialWord reports a name whose last token (or last two, as in
// API_KEY or CONNECTION_STRING) names a credential.
func endsInCredentialWord(name string) bool {
	toks := nameTokens(name)
	if len(toks) == 0 {
		return false
	}
	last := toks[len(toks)-1]
	if credentialWords[last] {
		return true
	}
	if len(toks) >= 2 {
		prev := toks[len(toks)-2]
		if last == "KEY" && credentialKeyPrefixes[prev] {
			return true
		}
		if last == "STRING" && prev == "CONNECTION" {
			return true
		}
	}
	return false
}

var placeholderWords = []string{"changeme", "change-me", "change_me", "changeit", "replace", "your-", "your_", "yourkey",
	"example", "placeholder", "dummy", "sample", "todo", "tbd", "redacted", "xxxx", "****", "none", "null", "undefined",
	"not-set", "notset", "fixme", "insert", "<", ">"}

// plausibleLiteralSecret decides whether a literal stored under a
// credential-named key looks like a real credential rather than a
// placeholder, a boolean, a URL or a plain word. It is deliberately strict:
// at least eight characters, no whitespace, not a placeholder, not a URL
// without userinfo, and at least a letter with a digit, or three character
// classes.
func plausibleLiteralSecret(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) < 8 || strings.ContainsAny(v, " \t\r\n") || strings.HasPrefix(v, "$") || strings.HasPrefix(v, "[") {
		return false
	}
	lv := strings.ToLower(v)
	for _, p := range placeholderWords {
		if strings.Contains(lv, p) {
			return false
		}
	}
	if strings.Contains(v, "://") && !secretShaped(v) {
		return false
	}
	var lower, upper, digit, other bool
	for _, r := range v {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	classes := 0
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			classes++
		}
	}
	letter := lower || upper
	return (letter && digit) || classes >= 3
}

// cfg005ARM scans the literal string properties of the compiled template's
// resources. A value with the shape of a credential, or a plausible literal
// under a property whose name ends in a credential word, is a finding. Template
// expressions are not literals and are skipped. The property path is reported;
// the value never is.
func cfg005ARM(in *sdk.Input) []sdk.Finding {
	if in == nil || in.ARM == nil {
		return nil
	}
	var fs []sdk.Finding
	for _, r := range in.ARM.Resources() {
		name := armexpr.StripScope(r.Name)
		w := armWalk{budget: 4000}
		w.walk(r.Properties, "properties", 0)
		for _, h := range w.hits {
			fs = append(fs, sdk.Finding{
				Key:      "arm-literal-credential:" + r.Type + "/" + name + ":" + h.path,
				Resource: sdk.ResourceRef{Type: r.Type, Name: name},
				Location: r.Location,
				Evidence: fmt.Sprintf("%s holds a literal %s in the compiled template", h.path, h.why),
			})
		}
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Key < fs[j].Key })
	return fs
}

type armHit struct{ path, why string }

type armWalk struct {
	budget int
	hits   []armHit
}

func (w *armWalk) walk(v any, path string, depth int) {
	if w.budget <= 0 || depth > 12 {
		return
	}
	w.budget--
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub := path + "." + k
			if s, isStr := x[k].(string); isStr {
				w.leaf(k, s, sub)
				continue
			}
			w.walk(x[k], sub, depth+1)
		}
	case []any:
		for i, it := range x {
			sub := fmt.Sprintf("%s[%d]", path, i)
			if s, isStr := it.(string); isStr {
				w.leaf("", s, sub)
				continue
			}
			w.walk(it, sub, depth+1)
		}
	}
}

func (w *armWalk) leaf(key, val, path string) {
	if armexpr.IsExpression(val) || containsRef(val) {
		return
	}
	switch {
	case secretShaped(val):
		w.hits = append(w.hits, armHit{path, "value with the shape of a credential"})
	case key != "" && endsInCredentialWord(key) && plausibleLiteralSecret(val):
		w.hits = append(w.hits, armHit{path, "value under a credential-named property"})
	}
}

// cfg005Env notes secret-shaped values in the selected azd environment. Whether
// the .env file is tracked in source control is not known to the rule, so the
// outcome is an uncertain note, never a finding. Only key names are reported.
func cfg005Env(p Providers) []string {
	if p.Env == nil {
		return nil
	}
	if _, sel := p.Env.Selected(); !sel {
		return nil
	}
	vals := p.Env.Values()
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var notes []string
	for _, k := range keys {
		v := vals[k]
		if containsRef(v) {
			continue
		}
		if secretShaped(v) || (endsInCredentialWord(k) && plausibleLiteralSecret(v)) {
			notes = append(notes, fmt.Sprintf("the selected azd environment holds a credential-like value under %s; whether its .env file is tracked in source control cannot be determined here", k))
		}
	}
	return notes
}
