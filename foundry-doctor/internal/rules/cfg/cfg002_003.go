package cfg

import (
	"fmt"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func evalCFG002(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	svcs := serviceList(d)
	all := byName(svcs)
	type key struct{ project, name string }
	seen := map[key]service{}
	var fs []sdk.Finding
	for _, s := range svcs {
		if s.Host != hostAgent {
			continue
		}
		name := s.str("name")
		if name == "" || containsRef(name) {
			continue // not statically resolvable
		}
		proj := ""
		if p, found := projectOf(s, all); found {
			proj = p.Name
		}
		if proj == "" || containsRef(proj) {
			continue
		}
		k := key{proj, name}
		if first, dup := seen[k]; dup {
			fs = append(fs, finding(d, s, s.get("name"),
				fmt.Sprintf("agent name %q in project %q is also declared by service %q", name, proj, first.Name)))
			continue
		}
		seen[k] = s
	}
	return ok(fs)
}

func evalCFG003(in *sdk.Input) sdk.Result {
	d, okd := loadDoc(in)
	if !okd {
		return skip(sdk.SkipInputUnavailable)
	}
	svcs := serviceList(d)
	all := byName(svcs)
	var fs []sdk.Finding
	for _, s := range svcs {
		add := func(n *yaml.Node, f string, a ...any) { fs = append(fs, finding(d, s, n, fmt.Sprintf(f, a...))) }
		switch {
		case s.Host == hostAgent:
			for _, r := range refNames(s.get("toolboxes")) {
				t, found := all[r.Name]
				switch {
				case containsRef(r.Name):
				case !found || t.Host != hostToolbox:
					add(r.Node, "toolboxes entry %q is not an azure.ai.toolbox service", r.Name)
				case !s.usesName(r.Name):
					add(r.Node, "toolboxes entry %q is not listed in uses", r.Name)
				}
			}
			if s.isPromptAgent() {
				for _, r := range refNames(s.get("connections")) {
					fs = append(fs, cfg003Conn(d, s, all, r)...)
				}
			}
			if s.isVoiceAgent() {
				fs = append(fs, cfg003Voice(d, s, all)...)
			}
		case s.Host == hostToolbox:
			for _, t := range items(s.get("tools")) {
				if c := child(t, "connection"); c != nil {
					fs = append(fs, cfg003Conn(d, s, all, nameAt{scalar(c), c})...)
				}
			}
		case s.Host == "azure.ai.memory-store" || s.Host == "azure.ai.memorystore":
			fs = append(fs, cfg003Memory(d, s, all)...)
		}
	}
	return ok(fs)
}

func cfg003Conn(d document, s service, all map[string]service, r nameAt) []sdk.Finding {
	if containsRef(r.Name) || r.Name == "" {
		return nil
	}
	c, found := all[r.Name]
	if !found || c.Host != hostConnection {
		return []sdk.Finding{finding(d, s, r.Node, fmt.Sprintf("connection %q is not an azure.ai.connection service", r.Name))}
	}
	if s.Host == hostAgent && !s.usesName(r.Name) {
		return []sdk.Finding{finding(d, s, r.Node, fmt.Sprintf("connection %q is not listed in uses", r.Name))}
	}
	return nil
}

func cfg003Voice(d document, s service, all map[string]service) []sdk.Finding {
	eng := child(s.get("conversationEngine"), "name")
	if eng == nil || containsRef(scalar(eng)) {
		return nil
	}
	t, found := all[scalar(eng)]
	if !found || t.Host != hostAgent || t.isPromptAgent() || t.isVoiceAgent() {
		return []sdk.Finding{finding(d, s, eng, fmt.Sprintf("conversationEngine %q is not a hosted azure.ai.agent service", scalar(eng)))}
	}
	sp, _ := projectOf(s, all)
	tp, _ := projectOf(t, all)
	if sp.Name != tp.Name {
		return []sdk.Finding{finding(d, s, eng, fmt.Sprintf("conversationEngine %q is in a different project", scalar(eng)))}
	}
	return nil
}

func deploymentNames(p service) (map[string]bool, bool) {
	if has(p.Node, "endpoint") || has(p.Node, "$ref") {
		return nil, false
	}
	names := map[string]bool{}
	for _, dep := range items(p.get("deployments")) {
		names[str(dep, "name")] = true
	}
	return names, true
}

func cfg003Memory(d document, s service, all map[string]service) []sdk.Finding {
	p, found := projectOf(s, all)
	if !found {
		return nil
	}
	names, known := deploymentNames(p)
	if !known {
		return nil
	}
	var fs []sdk.Finding
	for _, k := range []string{"chatModel", "embeddingModel"} {
		n := s.get(k)
		v := scalar(n)
		if n == nil || v == "" || containsRef(v) || names[v] {
			continue
		}
		fs = append(fs, finding(d, s, n, fmt.Sprintf("%s %q does not match a deployment in project %q", k, v, p.Name)))
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Location.Line < fs[j].Location.Line })
	return fs
}
