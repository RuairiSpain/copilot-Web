package provision

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/foundry"
)

// Op is what the engine will do to an item.
type Op string

// Operations.
const (
	OpCreate    Op = "create"
	OpUpdate    Op = "update"
	OpUnchanged Op = "unchanged"
	OpDelete    Op = "delete"
)

// Action is one step of a deployment.
type Action struct {
	Op    Op
	Key   string
	Item  Item      // the desired item (not for OpDelete)
	Prior StateItem // what the state recorded (for OpUpdate, OpUnchanged and OpDelete)
}

// Diff compares the desired items with the state. It uses no network: the state is the record
// of what x-foundry deployed. Creates, updates and unchanged items keep the desired order
// (toolboxes before the agents that attach them); deletes come last, agents before toolboxes.
func Diff(desired []Item, st *State) []Action {
	var actions []Action
	wanted := map[string]bool{}
	for _, it := range desired {
		wanted[it.Key()] = true
		prior, ok := st.Find(it.Key())
		switch {
		case !ok:
			actions = append(actions, Action{Op: OpCreate, Key: it.Key(), Item: it})
		case prior.Hash != it.Hash:
			actions = append(actions, Action{Op: OpUpdate, Key: it.Key(), Item: it, Prior: prior})
		default:
			actions = append(actions, Action{Op: OpUnchanged, Key: it.Key(), Item: it, Prior: prior})
		}
	}
	var deletes []Action
	for _, s := range st.Items {
		if !wanted[s.Key()] {
			deletes = append(deletes, Action{Op: OpDelete, Key: s.Key(), Prior: s})
		}
	}
	sort.Slice(deletes, func(i, j int) bool {
		a, b := deletes[i].Prior, deletes[j].Prior
		if a.Kind != b.Kind {
			return a.Kind == KindAgent // agents go before the toolboxes they may use
		}
		return a.Key() < b.Key()
	})
	return append(actions, deletes...)
}

// CheckDestroy implements rule XF025: removing an item deletes it from Foundry, including every
// version, so it needs explicit approval.
func CheckDestroy(actions []Action, approved bool) []diag.Diagnostic {
	if approved {
		return nil
	}
	var out []diag.Diagnostic
	for _, a := range actions {
		if a.Op == OpDelete {
			out = append(out, diag.Err("XF025", "x-foundry.projects["+a.Prior.Project+"]."+a.Prior.Kind+"s["+a.Prior.Name+"]",
				"the %s '%s' is no longer in the configuration, so deploying deletes it from project '%s' with all its versions; approve with --allow-destroy", a.Prior.Kind, a.Prior.Name, a.Prior.Project))
		}
	}
	return out
}

// API is the data plane of one project.
type API interface {
	GetAgent(ctx context.Context, name string) (*foundry.Resource, error)
	CreateAgentVersion(ctx context.Context, name string, body map[string]any) (*foundry.Resource, error)
	DeleteAgent(ctx context.Context, name string) error
	GetToolbox(ctx context.Context, name string) (*foundry.Resource, error)
	CreateToolboxVersion(ctx context.Context, name string, body map[string]any) (*foundry.Resource, error)
	DeleteToolbox(ctx context.Context, name string) error
	ToolboxMCPURL(name, version string) string
}

// Engine applies actions.
type Engine struct {
	// API returns the client for a project.
	API func(project string) API
	// State is updated as each action succeeds, so a failure keeps the progress made.
	State *State
	// Persist saves the state; it is called after every change. May be nil.
	Persist func(*State) error
	// Log receives one line per action. May be nil.
	Log func(string)
}

func (e *Engine) log(format string, args ...any) {
	if e.Log != nil {
		e.Log(fmt.Sprintf(format, args...))
	}
}

func (e *Engine) save() error {
	if e.Persist == nil {
		return nil
	}
	return e.Persist(e.State)
}

// Apply runs the actions in order and stops at the first failure.
func (e *Engine) Apply(ctx context.Context, actions []Action) error {
	for _, a := range actions {
		if err := e.apply(ctx, a); err != nil {
			return fmt.Errorf("%s %s: %w", a.Op, a.Key, err)
		}
	}
	return nil
}

func (e *Engine) apply(ctx context.Context, a Action) error {
	switch a.Op {
	case OpDelete:
		return e.remove(ctx, a)
	case OpUnchanged:
		remote, err := e.get(ctx, a.Item)
		if err != nil {
			return err
		}
		if remote != nil && remote.Version == a.Prior.Version {
			e.log("unchanged %s", a.Key)
			return nil
		}
		why := "recreated: it is missing from the project"
		if remote != nil {
			why = fmt.Sprintf("redeployed: the project has version %s, not the recorded %s", remote.Version, a.Prior.Version)
		}
		e.log("%s %s", why, a.Key)
	default:
		e.log("%s %s", a.Op, a.Key)
	}
	return e.deploy(ctx, a.Item)
}

func (e *Engine) get(ctx context.Context, it Item) (*foundry.Resource, error) {
	api := e.API(it.Project)
	if it.Kind == KindToolbox {
		return api.GetToolbox(ctx, it.Name)
	}
	return api.GetAgent(ctx, it.Name)
}

func (e *Engine) deploy(ctx context.Context, it Item) error {
	api := e.API(it.Project)
	var created *foundry.Resource
	var err error
	if it.Kind == KindToolbox {
		created, err = api.CreateToolboxVersion(ctx, it.Name, it.Body)
	} else {
		var body map[string]any
		if body, err = e.agentBody(api, it); err == nil {
			created, err = api.CreateAgentVersion(ctx, it.Name, body)
		}
	}
	if err != nil {
		return err
	}
	e.State.Set(StateItem{Project: it.Project, Kind: it.Kind, Name: it.Name, Hash: it.Hash, Version: created.Version})
	return e.save()
}

// agentBody adds the MCP tools that attach the agent's toolboxes, now that their versions are known.
func (e *Engine) agentBody(api API, it Item) (map[string]any, error) {
	body := cloneBody(it.Body)
	definition := body["definition"].(map[string]any)
	tools := append([]any{}, definition["tools"].([]any)...)
	for _, name := range it.Toolboxes {
		tb, ok := e.State.Find(Key(it.Project, KindToolbox, name))
		if !ok {
			return nil, fmt.Errorf("toolbox '%s' has not been deployed", name)
		}
		tools = append(tools, map[string]any{
			"type": "mcp", "server_label": name, "server_url": api.ToolboxMCPURL(name, tb.Version), "require_approval": "never",
		})
	}
	definition["tools"] = tools
	return body, nil
}

func cloneBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}
	definition := make(map[string]any, len(body["definition"].(map[string]any)))
	for k, v := range body["definition"].(map[string]any) {
		definition[k] = v
	}
	out["definition"] = definition
	return out
}

func (e *Engine) remove(ctx context.Context, a Action) error {
	e.log("delete %s", a.Key)
	api := e.API(a.Prior.Project)
	var err error
	if a.Prior.Kind == KindToolbox {
		err = api.DeleteToolbox(ctx, a.Prior.Name)
	} else {
		err = api.DeleteAgent(ctx, a.Prior.Name)
	}
	if err != nil {
		return err
	}
	e.State.Remove(a.Key)
	return e.save()
}

// Summary counts the operations in a list of actions, for example "2 to create, 1 to update".
func Summary(actions []Action) string {
	counts := map[Op]int{}
	for _, a := range actions {
		counts[a.Op]++
	}
	var parts []string
	for _, op := range []Op{OpCreate, OpUpdate, OpDelete, OpUnchanged} {
		if counts[op] > 0 {
			label := map[Op]string{OpCreate: "to create", OpUpdate: "to update", OpDelete: "to delete", OpUnchanged: "unchanged"}[op]
			parts = append(parts, fmt.Sprintf("%d %s", counts[op], label))
		}
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}
