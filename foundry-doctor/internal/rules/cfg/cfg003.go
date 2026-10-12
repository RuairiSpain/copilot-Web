package cfg

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule003 is FND-CFG-003: every inter-service reference in azure.yaml resolves inside the same file.
type rule003 struct{}

func (rule003) ID() string   { return "FND-CFG-003" }
func (rule003) Version() int { return 1 }

const (
	rec003 = "Make each referenced name match a service key defined under services, and add toolbox and connection services to the consuming agent uses list as well as its toolboxes or connections list."
	fix003 = "services:\n  github-conn:\n    host: azure.ai.connection\n    uses: [ai-project]\n  agent-tools:\n    host: azure.ai.toolbox\n    uses: [ai-project, github-conn]\n    tools:\n      - type: mcp\n        connection: github-conn\n  my-agent:\n    host: azure.ai.agent\n    uses: [ai-project, github-conn, agent-tools]\n    toolboxes: [agent-tools]\n"
)

func (r rule003) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	ay := in.AzureYAML
	by := serviceByName(ay)
	g := graph.Build(*in)

	// uses entries that match no service and no resources entry.
	infra := in.ARM != nil || in.Project.HasARM()
	for _, u := range g.UnresolvedUses() {
		owner, ok := strings.CutPrefix(u.Owner, model.NodeService+":")
		if !ok || hasRef(u.Name) {
			continue // a ${VAR} entry is known only after expansion
		}
		s := by[owner]
		var pos model.Pos
		for _, ref := range s.Uses {
			if ref.Name == u.Name {
				pos = ref.Pos
				break
			}
		}
		ev := fmt.Sprintf("service %s uses %q, which is neither a service nor a resources entry", owner, u.Name)
		key := "uses:" + owner + ":" + u.Name
		if infra {
			x.uncertain(svcRef(s), x.loc(pos), key, ev+"; it may name a Bicep or ARM resource, which is not resolved here", rec003, fix003)
			continue
		}
		x.fail(svcRef(s), x.loc(pos), key, sdk.ConfidenceCertain, ev, rec003, fix003)
	}

	for _, s := range sortedServices(ay) {
		switch s.Host {
		case hostAgent:
			r.agent(x, by, s)
		case hostToolbox:
			r.toolbox(x, by, s)
		case hostRoutine:
			r.routine(x, by, s)
		}
	}

	for _, c := range g.Cycles(model.EdgeUses) {
		names := make([]string, len(c))
		for i, id := range c {
			names[i] = strings.TrimPrefix(id, model.NodeService+":")
		}
		first := by[names[0]]
		x.fail(svcRef(first), x.loc(first.Pos), "cycle:"+strings.Join(names, ","), sdk.ConfidenceCertain,
			"uses form a cycle between services "+strings.Join(names, ", "), rec003, fix003)
	}
	return x.done(), nil
}

// target checks that name is a service of the wanted host. It returns false when the reference is
// known only after expansion.
func (rule003) target(x *run, by map[string]model.Service, s model.Service, n *model.Node, name, wantHost, what string) {
	if hasRef(name) || blank(name) {
		return
	}
	pos := posOf(n, s.Pos)
	t, ok := by[name]
	switch {
	case !ok:
		x.fail(svcRef(s), x.loc(pos), what+":"+s.Name+":"+name, sdk.ConfidenceCertain,
			fmt.Sprintf("%s of service %s names %q, which is not a service", what, s.Name, name), rec003, fix003)
	case t.Host != wantHost:
		x.fail(svcRef(s), x.loc(pos), what+":"+s.Name+":"+name, sdk.ConfidenceCertain,
			fmt.Sprintf("%s of service %s names %q, which has host %s instead of %s", what, s.Name, name, t.Host, wantHost), rec003, fix003)
	}
}

func (r rule003) agent(x *run, by map[string]model.Service, s model.Service) {
	if hasRefFile(s) {
		x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
	}
	for _, it := range seqItems(s.Node.Lookup("toolboxes")) {
		name, ok := nameOf(it)
		if !ok || hasRef(name) {
			continue
		}
		r.target(x, by, s, it, name, hostToolbox, "toolbox")
		if t, found := by[name]; found && t.Host == hostToolbox && !usesName(s, name) {
			x.fail(svcRef(s), x.loc(posOf(it, s.Pos)), "toolbox-uses:"+s.Name+":"+name, sdk.ConfidenceCertain,
				fmt.Sprintf("service %s lists toolbox %q in toolboxes but not in uses", s.Name, name), rec003, fix003)
		}
	}
	for _, it := range seqItems(s.Node.Lookup("connections")) {
		name, ok := nameOf(it)
		if !ok || hasRef(name) {
			continue
		}
		r.target(x, by, s, it, name, hostConnection, "connection")
		if t, found := by[name]; found && t.Host == hostConnection && !usesName(s, name) {
			x.fail(svcRef(s), x.loc(posOf(it, s.Pos)), "connection-uses:"+s.Name+":"+name, sdk.ConfidenceCertain,
				fmt.Sprintf("service %s lists connection %q in connections but not in uses", s.Name, name), rec003, fix003)
		}
	}
	if n := s.Node.Lookup("conversationEngine", "name"); n != nil {
		if name, ok := scalar(n); ok && !hasRef(name) && !blank(name) {
			r.engine(x, by, s, n, name)
		}
	}
	// A registry connection may be a name or an ID; a plain name that is no connection service may be an
	// existing project connection, so it is uncertain rather than failed.
	if v, n := field(s, "registryConnectionId"); n != nil && !blank(v) && !hasRef(v) && !strings.Contains(v, "/") {
		if t, ok := by[v]; !ok || t.Host != hostConnection {
			x.uncertain(svcRef(s), x.loc(posOf(n, s.Pos)), "registry-connection:"+s.Name,
				fmt.Sprintf("registryConnectionId of service %s names no azure.ai.connection service; it may be an existing project connection", s.Name), rec003, fix003)
		}
	}
	r.memory(x, by, s)
}

func (rule003) engine(x *run, by map[string]model.Service, s model.Service, n *model.Node, name string) {
	t, ok := by[name]
	key := "engine:" + s.Name + ":" + name
	loc := x.loc(posOf(n, s.Pos))
	switch {
	case !ok || t.Host != hostAgent:
		x.fail(svcRef(s), loc, key, sdk.ConfidenceCertain,
			fmt.Sprintf("conversationEngine.name of service %s names %q, which is not an azure.ai.agent service", s.Name, name), rec003, fix003)
	case kindOf(t) != "" && kindOf(t) != "hosted":
		x.fail(svcRef(s), loc, key, sdk.ConfidenceCertain,
			fmt.Sprintf("conversationEngine.name of service %s names %q, which is not a hosted agent", s.Name, name), rec003, fix003)
	}
}

func (rule003) memory(x *run, by map[string]model.Service, s model.Service) {
	stores := seqItems(s.Node.Lookup("memoryStores"))
	if len(stores) == 0 {
		return
	}
	names, why := deployments(by, s)
	for _, st := range stores {
		for _, f := range []string{"chatModel", "embeddingModel"} {
			n := st.Lookup(f)
			name, ok := scalar(n)
			if !ok || blank(name) || hasRef(name) || names[name] {
				continue
			}
			if why != "" {
				x.skip(rules.ReasonUnresolvedValue(why), "project deployment list", s.Name)
				continue
			}
			x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), "memory-"+f+":"+s.Name+":"+name, sdk.ConfidenceCertain,
				fmt.Sprintf("memory store %s of service %s names deployment %q, which no used project declares", f, s.Name, name), rec003, fix003)
		}
	}
}

func (r rule003) toolbox(x *run, by map[string]model.Service, s model.Service) {
	if hasRefFile(s) {
		x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
	}
	for _, it := range seqItems(s.Node.Lookup("tools")) {
		n := it.Lookup("connection")
		if name, ok := scalar(n); ok {
			r.target(x, by, s, n, name, hostConnection, "tool-connection")
		}
	}
}

func (rule003) routine(x *run, by map[string]model.Service, s model.Service) {
	if hasRefFile(s) {
		x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
	}
	n := s.Node.Lookup("action", "agent_name")
	name, ok := scalar(n)
	if !ok || blank(name) || hasRef(name) {
		return
	}
	t, found := by[name]
	switch {
	case !found || t.Host != hostAgent:
		x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), "routine-agent:"+s.Name+":"+name, sdk.ConfidenceCertain,
			fmt.Sprintf("routine %s invokes %q, which is not an azure.ai.agent service", s.Name, name), rec003, fix003)
	case !usesName(s, name):
		x.fail(svcRef(s), x.loc(posOf(n, s.Pos)), "routine-uses:"+s.Name+":"+name, sdk.ConfidenceCertain,
			fmt.Sprintf("routine %s invokes agent %q but does not list it in uses", s.Name, name), rec003, fix003)
	}
}
