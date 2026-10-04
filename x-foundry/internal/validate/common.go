// Package validate holds the semantic validation rules.
//
// Declared validates the configuration as authored (before normalisation); Effective
// validates the normalised configuration after inheritance and implicit resources.
// Diagnostic codes XF001-XF025 are the numbers of the specification's rules.
package validate

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
)

const root = "x-foundry"

// path joins config path parts: path("projects[fin]", "location") = "x-foundry.projects[fin].location".
func path(parts ...string) string {
	if len(parts) == 0 {
		return root
	}
	return root + "." + strings.Join(parts, ".")
}

type named interface{ GetName() string }

// duplicates returns the sorted names that occur more than once.
func duplicates[T named](items []T) []string {
	counts := map[string]int{}
	for _, i := range items {
		counts[i.GetName()]++
	}
	var out []string
	for name, n := range counts {
		if n > 1 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// holder is one declaring scope (root, hub or a project) with the fields it can declare.
type holder struct {
	id      string
	path    string
	project string
	models  *config.ModelConfiguration
	iq      *config.FoundryIQ
	search  *config.Search
}

func (h holder) kbs() []config.KnowledgeBase {
	if h.iq == nil {
		return nil
	}
	return h.iq.KnowledgeBases
}

func scopes(cfg *config.XFoundry) []holder {
	out := []holder{{
		id: "root", path: root, models: &cfg.Models, iq: cfg.IQ, search: cfg.Search,
	}}
	if h := cfg.Hub; h != nil {
		out = append(out, holder{
			id: "hub", path: root + ".hub", models: &h.Models, iq: h.IQ, search: h.Search,
		})
	}
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		out = append(out, holder{
			id: "project:" + p.Name, path: fmt.Sprintf("%s.projects[%s]", root, p.Name), project: p.Name,
			models: &p.Models, iq: p.IQ, search: p.Search,
		})
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func nameSet[T named](items []T) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i.GetName()] = true
	}
	return m
}

// parseNet parses an IP address or CIDR range.
func parseNet(s string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Masked(), true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen()), true
	}
	return netip.Prefix{}, false
}

func subnetOf(inner, outer netip.Prefix) bool {
	return inner.Addr().Is4() == outer.Addr().Is4() && outer.Bits() <= inner.Bits() && outer.Contains(inner.Addr())
}

var privateRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func isPrivateRange(s string) bool {
	n, ok := parseNet(s)
	if !ok {
		return false
	}
	for _, p := range privateRanges {
		if subnetOf(n, p) {
			return true
		}
	}
	return false
}

func sortStrings(s []string) { sort.Strings(s) }
