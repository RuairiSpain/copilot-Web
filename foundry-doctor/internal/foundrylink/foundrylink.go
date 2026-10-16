// Package foundrylink decides whether a resource in the compiled template is
// provably used by Foundry. The evidence is the references held by Foundry
// connections, capability hosts and capabilitySettings. A resource is Linked
// only when one of those references names it; it is NotLinked only when every
// reference provably names something else; otherwise the answer is Uncertain
// and rules must skip with a scope-uncertain reason instead of guessing.
package foundrylink

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SkipScopeUncertain is the skip reason when Foundry usage of a resource
// cannot be proven or disproven from the template.
const SkipScopeUncertain = "scope-uncertain"

// SkipConditional is the skip reason when a resource may not be deployed
// because of a parameter-driven condition or a copy loop.
const SkipConditional = "conditional-resource"

// State is the answer to "is this resource used by Foundry".
type State int

// States.
const (
	NotLinked State = iota
	Linked
	Uncertain
)

type link struct {
	ref      armexpr.Ref
	definite bool
}

// Set holds the references Foundry resources make to other resources.
type Set struct {
	links  []link
	opaque bool
	any    bool
}

// domainTypes maps a service endpoint domain suffix to a resource type.
var domainTypes = []struct{ suffix, typ string }{
	{".search.windows.net", "microsoft.search/searchservices"},
	{".documents.azure.com", "microsoft.documentdb/databaseaccounts"},
	{".vault.azure.net", "microsoft.keyvault/vaults"},
	{".blob.core.windows.net", "microsoft.storage/storageaccounts"},
}

func typeOfHost(host string) (string, string, bool) {
	host = strings.ToLower(host)
	for _, d := range domainTypes {
		if strings.HasSuffix(host, d.suffix) {
			label := strings.TrimSuffix(host, d.suffix)
			if label != "" && !strings.Contains(label, ".") {
				return label, d.typ, true
			}
		}
	}
	return "", "", false
}

// Collect gathers the references of every Foundry connection, capability host
// and capabilitySettings block in the model.
func Collect(m sdk.ARMModel) Set { return CollectWhere(m, nil) }

// CollectWhere is Collect restricted to connections and capability hosts for
// which keep returns true (nil keeps all). Account and project
// capabilitySettings are not collected when keep is set.
func CollectWhere(m sdk.ARMModel, keep func(sdk.ARMResource) bool) Set {
	var s Set
	if m == nil {
		return s
	}
	for _, r := range m.Resources() {
		t := strings.ToLower(r.Type)
		isChild := strings.HasPrefix(t, "microsoft.cognitiveservices/accounts") &&
			(strings.HasSuffix(t, "/connections") || strings.HasSuffix(t, "/capabilityhosts"))
		isAcct := t == "microsoft.cognitiveservices/accounts" || t == "microsoft.cognitiveservices/accounts/projects"
		if !isChild && !isAcct {
			continue
		}
		if keep != nil && (isAcct || !keep(r)) {
			continue
		}
		pr := armexpr.PresenceOf(r.Conditional, r.Condition, r.Looped)
		if pr == armexpr.NotDeployed {
			continue
		}
		var props any = r.Properties
		if isAcct {
			cs, ok := r.Properties["capabilitySettings"]
			if !ok {
				continue
			}
			props = cs
		} else {
			s.any = true
		}
		s.walk("", props, pr == armexpr.Deployed)
	}
	return s
}

// Any reports whether a Foundry connection or capability host exists in the
// template (deployed or conditional).
func (s Set) Any() bool { return s.any }

func (s *Set) walk(key string, v any, definite bool) {
	switch x := v.(type) {
	case string:
		s.add(key, x, definite)
	case []any:
		for _, e := range x {
			s.walk(key, e, definite)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			s.walk(k, x[k], definite)
		}
	}
}

var (
	nameLikeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{1,127}$`)
	hostTplRe  = regexp.MustCompile(`(?i)^https?://\{(\d+)\}(\.[a-z0-9.-]+)(?:[:/].*)?$`)
)

// refKeyWords are substrings of property names that can hold a reference to
// another resource. A bare literal (no resource type, not a URL) under any
// other key, such as category or authType, is not a reference.
var refKeyWords = []string{"id", "resource", "target", "name", "account", "store", "service", "endpoint"}

func refKey(key string) bool {
	k := strings.ToLower(key)
	for _, w := range refKeyWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

func (s *Set) add(key, str string, definite bool) {
	str = strings.TrimSpace(str)
	if str == "" {
		return
	}
	if !armexpr.IsExpression(str) {
		switch {
		case strings.Contains(strings.ToLower(str), "/providers/"):
			s.addRef(armexpr.ParseRef(str), definite)
		case strings.Contains(str, "://"):
			if u, err := url.Parse(str); err == nil {
				if label, typ, ok := typeOfHost(u.Hostname()); ok {
					ref := armexpr.ParseName(typ, label)
					s.addRef(ref, definite)
				}
			}
		case refKey(key) && nameLikeRe.MatchString(str):
			s.addRef(armexpr.ParseRef(str), definite)
		}
		return
	}
	if c, ok := armexpr.ParseCall(str); ok && c.Fn == "format" && c.Suffix == "" && len(c.Args) > 0 {
		if f, ok := armexpr.Unquote(c.Args[0]); ok && strings.Contains(f, "://") {
			m := hostTplRe.FindStringSubmatch(f)
			if m == nil {
				return // a URL not shaped like a service endpoint: not a resource reference
			}
			_, typ, ok := typeOfHost("x" + m[2])
			if !ok {
				return
			}
			n := 0
			for _, ch := range m[1] {
				n = n*10 + int(ch-'0')
			}
			if n+1 >= len(c.Args) {
				s.opaque = true
				return
			}
			s.addRef(armexpr.ParseArg(typ, c.Args[n+1]), definite)
			return
		}
	}
	s.addRef(armexpr.ParseRef(str), definite)
}

func (s *Set) addRef(r armexpr.Ref, definite bool) {
	if r.Opaque || len(r.Segments) == 0 {
		s.opaque = true
		return
	}
	s.links = append(s.links, link{ref: r, definite: definite})
}

// Classify says whether r is used by Foundry.
func (s Set) Classify(r sdk.ARMResource) State {
	if r.Looped {
		return Uncertain
	}
	return s.ClassifyRef(armexpr.ParseName(r.Type, r.Name))
}

// ClassifyRef is Classify for a resource identity given as a reference.
func (s Set) ClassifyRef(self armexpr.Ref) State {
	uncertain := s.opaque
	for _, l := range s.links {
		switch armexpr.Compare(l.ref, self) {
		case armexpr.Same:
			if l.definite {
				return Linked
			}
			uncertain = true
		case armexpr.Unknown:
			uncertain = true
		}
	}
	if uncertain {
		return Uncertain
	}
	return NotLinked
}
