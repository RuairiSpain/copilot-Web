package cfg

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// rule002 is FND-CFG-002: services of one azure.ai.* kind resolve to distinct resource names.
type rule002 struct{}

func (rule002) ID() string   { return "FND-CFG-002" }
func (rule002) Version() int { return 1 }

const (
	rec002 = "Give each service of the same kind a distinct effective name. If two services are meant to be versions of one agent, keep a single service and let azd create the new version on deploy."
	fix002 = "services:\n  triage:\n    host: azure.ai.agent\n    name: triage-agent\n  summarizer:\n    host: azure.ai.agent\n    name: summarizer-agent\n"
)

type nameEntry struct {
	svc  model.Service
	name string
}

func (r rule002) Evaluate(_ context.Context, in *model.Input) ([]rules.Result, error) {
	if yamlMissing(in) {
		return skipNoYAML(r), nil
	}
	x := newRun(r, in)
	ay := in.AzureYAML
	by := serviceByName(ay)

	groups := map[string][]nameEntry{}
	for _, s := range sortedServices(ay) {
		name, ok := effectiveName(s)
		if !ok {
			if hasRefFile(s) {
				x.skip(rules.ReasonUnresolvedValue("service-body-in-ref-file"), capRefFile, s.Name)
			}
			continue
		}
		var projects []string
		for _, p := range usedProjects(by, s) {
			projects = append(projects, p.Name)
		}
		k := s.Host + "\x00" + strings.Join(projects, ",") + "\x00" + name
		groups[k] = append(groups[k], nameEntry{s, name})
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		g := groups[k]
		name := g[0].name
		if len(g) < 2 {
			if hasRef(name) {
				x.skip(rules.ReasonUnresolvedValue("name-env-reference"), "expanded ${VAR} values", g[0].svc.Name)
			}
			continue
		}
		second := g[1].svc
		names := make([]string, len(g))
		for i, e := range g {
			names[i] = e.svc.Name
		}
		loc := x.loc(posOf(second.Node.Lookup("name"), second.Pos))
		ev := fmt.Sprintf("services %s of host %s resolve to the same effective name %q", strings.Join(names, ", "), second.Host, name)
		key := "dup-name:" + second.Host + ":" + name
		if hasRef(name) {
			x.uncertain(svcRef(second), loc, key, ev+" before ${VAR} expansion", rec002, fix002)
			continue
		}
		x.fail(svcRef(second), loc, key, sdk.ConfidenceCertain, ev, rec002, fix002)
	}
	return x.done(), nil
}

// effectiveName resolves the Foundry resource name of a service by the documented precedence. ok is false
// when the host has no name rule here or the name is not determinable from the local body.
func effectiveName(s model.Service) (string, bool) {
	switch s.Host {
	case hostAgent:
		v, n := field(s, "name")
		return v, n != nil && !blank(v)
	case hostConnection:
		if v, _ := field(s, "name"); !blank(v) {
			return strings.TrimSpace(v), true
		}
		if hasRefFile(s) {
			return "", false
		}
		return s.Name, true
	case hostToolbox, hostRoutine:
		return s.Name, true
	}
	return "", false
}
