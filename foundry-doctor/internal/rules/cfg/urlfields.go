package cfg

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// urlField is one URL-typed scalar of azure.yaml. The fields are URL-typed by schema description only
// (fact-check item 4); MCP server_url and env values are known from extension source and heuristics.
type urlField struct {
	svc   model.Service
	name  string // field label, never the value
	value string
	pos   model.Pos
}

// urlFields returns the URL-typed fields in service name and document order.
func urlFields(ay *model.AzureYAML) []urlField {
	var out []urlField
	add := func(s model.Service, label string, n *model.Node) {
		if v, ok := scalar(n); ok && !blank(v) {
			out = append(out, urlField{s, label, v, posOf(n, s.Pos)})
		}
	}
	for _, s := range sortedServices(ay) {
		switch s.Host {
		case hostConnection:
			for _, f := range []string{"target", "authorizationUrl", "tokenUrl", "refreshUrl"} {
				add(s, f, s.Node.Lookup(f))
			}
		case hostToolbox:
			add(s, "endpoint", s.Node.Lookup("endpoint"))
			for i, t := range seqItems(s.Node.Lookup("tools")) {
				add(s, fmt.Sprintf("tools[%d].server_url", i), t.Lookup("server_url"))
			}
		case hostProject, hostLegacyFoundry:
			add(s, "endpoint", s.Node.Lookup("endpoint"))
		case hostAgent:
			for i, t := range seqItems(s.Node.Lookup("toolConnections")) {
				add(s, fmt.Sprintf("toolConnections[%d].target", i), t.Lookup("target"))
			}
		}
	}
	return out
}

// envURLFields returns service env values that look like absolute URLs (a heuristic, not a schema field).
func envURLFields(ay *model.AzureYAML) []urlField {
	var out []urlField
	for _, s := range sortedServices(ay) {
		env := s.Node.Lookup("env")
		if env == nil || env.Kind != model.NodeMapping {
			continue
		}
		for _, c := range env.Children {
			if v, ok := scalar(c); ok && strings.Contains(v, "://") {
				out = append(out, urlField{s, "env." + c.Key, v, posOf(c, s.Pos)})
			}
		}
	}
	return out
}

// maskRefs replaces ${...} and ${{...}} references with a neutral token so the rest can be parsed.
func maskRefs(s string) string { return maskWith(s, refToken) }

// refToken stands in for a reference while a URL is parsed.
const refToken = "refx7q"

func maskWith(s, token string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "$${") { // escaped: literal text
			b.WriteString(s[i : i+3])
			i += 3
			continue
		}
		if strings.HasPrefix(s[i:], "${") {
			end := "}"
			if strings.HasPrefix(s[i:], "${{") {
				end = "}}"
			}
			if j := strings.Index(s[i:], end); j >= 0 {
				b.WriteString(token)
				i += j + len(end)
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// wholeRef reports whether the value consists only of references, so nothing literal can be judged.
func wholeRef(s string) bool {
	return hasRef(s) && strings.TrimSpace(maskWith(s, "")) == ""
}

// parseURL parses an absolute URL after masking references. ok is false for resource IDs, relative
// values and anything else that is not scheme://host.
func parseURL(v string) (u *url.URL, ok bool) {
	if strings.HasPrefix(v, "${") {
		return nil, false // the scheme itself is a reference
	}
	u, err := url.Parse(maskRefs(strings.TrimSpace(v)))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, false
	}
	return u, true
}
