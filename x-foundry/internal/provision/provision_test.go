package provision

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/foundry"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

const models = `models: {default: gpt-5, allowed: [gpt-5]}`

func desired(t *testing.T, overrides ...string) ([]Item, []diag.Diagnostic) {
	t.Helper()
	return Desired(MustPlan(t, append([]string{Public, models}, overrides...)...))
}

func find(t *testing.T, items []Item, kind, name string) Item {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind && it.Name == name {
			return it
		}
	}
	t.Fatalf("no %s %s in %v", kind, name, items)
	return Item{}
}

const mcps = `mcps: [{name: graph, endpoint: "https://graph.example/mcp", allowedTools: [search], headers: {x-team: fin}}, {name: wiki, endpoint: "https://wiki.example/mcp"}]`

func TestToolboxesAndPromptAgents(t *testing.T) {
	items, warnings := desired(t, mcps,
		`toolboxes: [{name: search, description: "Find things", tools: [{name: gt, type: mcp, reference: graph}, {name: wk, type: mcp, reference: wiki}, {name: ci, type: codeInterpreter, reference: x}]}]`,
		`agents: [{name: bot, instructions: Be brief., toolboxes: [search], mcps: [wiki], tags: {team: fin}}]`)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(items) != 2 || items[0].Kind != KindToolbox || items[1].Kind != KindAgent {
		t.Fatalf("toolboxes must come first: %v", items)
	}
	tb := find(t, items, KindToolbox, "search").Body
	if tb["description"] != "Find things" {
		t.Fatal(tb)
	}
	tools := tb["tools"].([]any)
	graph, wiki := tools[0].(map[string]any), tools[1].(map[string]any)
	if graph["type"] != "mcp" || graph["server_label"] != "graph" || graph["server_url"] != "https://graph.example/mcp" ||
		graph["require_approval"] != "never" || fmt.Sprint(graph["allowed_tools"]) != "[search]" || fmt.Sprint(graph["headers"]) != "map[x-team:fin]" {
		t.Fatalf("graph = %v", graph)
	}
	if wiki["require_approval"] != "always" || wiki["allowed_tools"] != nil {
		t.Fatalf("a server without an allow-list needs approval: %v", wiki)
	}
	if tools[2].(map[string]any)["type"] != "code_interpreter" {
		t.Fatalf("tools = %v", tools)
	}
	agent := find(t, items, KindAgent, "bot")
	def := agent.Body["definition"].(map[string]any)
	if def["kind"] != "prompt" || def["model"] != "gpt-5" || def["instructions"] != "Be brief." || len(def["tools"].([]any)) != 1 {
		t.Fatalf("definition = %v", def)
	}
	if fmt.Sprint(agent.Toolboxes) != "[search]" || agent.Project != "finance" || agent.Key() != "finance/agent/bot" {
		t.Fatalf("agent = %+v", agent)
	}
	meta := agent.Body["metadata"].(map[string]string)
	if meta["managed-by"] != "x-foundry" || meta["x-foundry-id"] != "finance/agent/bot" || meta["team"] != "fin" || meta["x-foundry-env"] != "dev" {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestAgentModelFallsBackToTheProjectDefault(t *testing.T) {
	items, _ := desired(t, `agents: [{name: aa, instructions: x}, {name: bb, model: gpt-5, instructions: y}]`)
	for _, name := range []string{"aa", "bb"} {
		if m := find(t, items, KindAgent, name).Body["definition"].(map[string]any)["model"]; m != "gpt-5" {
			t.Fatalf("%s model = %v", name, m)
		}
	}
	if _, has := find(t, items, KindAgent, "bb").Body["definition"].(map[string]any)["instructions"]; !has {
		t.Fatal("instructions are sent when present")
	}
	bare, _ := desired(t, `agents: [{name: cc}]`)
	if _, has := find(t, bare, KindAgent, "cc").Body["definition"].(map[string]any)["instructions"]; has {
		t.Fatal("no instructions key when empty")
	}
}

func TestWarningsForWhatIsNotDeployed(t *testing.T) {
	_, warnings := desired(t, mcps,
		`mcps: [{name: secure, endpoint: "https://s.example/mcp", authentication: {mode: apiKey, secretRef: key}}, {name: open, endpoint: "https://o.example/mcp", authentication: {mode: none}}]`,
		`iq: {knowledgeBases: [{name: kb, sources: [{name: sr, type: web, url: "https://x.example"}]}]}`,
		`toolboxes: [{name: box, tools: [{name: sc, type: mcp, reference: secure}, {name: op, type: mcp, reference: open}, {name: kn, type: knowledgeBase, reference: kb}, {name: fn, type: function, reference: fn}]}]`,
		`agents: [{name: hosted-bot, kind: hosted, source: ./bot}, {name: rag, instructions: x, knowledgeBases: [kb], mcps: [secure]}]`)
	text := ""
	for _, w := range warnings {
		text += w.Message + "\n"
	}
	for _, want := range []string{
		"uses apiKey authentication through a project connection named 'secure'",
		"tool type 'knowledgeBase' is not deployed yet", "tool type 'function' is not deployed yet",
		"hosted agent 'hosted-bot' is not deployed by x-foundry", "knowledge bases [kb] are not attached yet",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "'open'") {
		t.Fatalf("authentication none needs no connection:\n%s", text)
	}
	secure := false
	items, _ := desired(t, `mcps: [{name: secure, endpoint: "https://s.example/mcp", authentication: {mode: apiKey, secretRef: key}}]`, `agents: [{name: aa, instructions: x, mcps: [secure]}]`)
	for _, tool := range find(t, items, KindAgent, "aa").Body["definition"].(map[string]any)["tools"].([]any) {
		secure = secure || tool.(map[string]any)["project_connection_id"] == "secure"
	}
	if !secure {
		t.Fatal("authenticated servers reference a project connection")
	}
	if items2, _ := desired(t, `agents: [{name: hosted-bot, kind: hosted, source: ./bot}]`); len(items2) != 0 {
		t.Fatalf("hosted agents are not deployed: %v", items2)
	}
}

func TestInheritedToolboxesAreDeployedIntoEveryProject(t *testing.T) {
	p := MustOK(t, RunHub(t, Public, `hub: {name: shared, models: {default: gpt-5, allowed: [gpt-5]}, mcps: [{name: graph, endpoint: "https://g.example/mcp"}], toolboxes: [{name: shared-box, tools: [{name: gt, type: mcp, reference: graph}]}]}`,
		`projects: [{name: finance, agents: [{name: bot, instructions: x, toolboxes: [shared-box]}]}, {name: hr}]`))
	items, _ := Desired(p)
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key())
	}
	want := "finance/toolbox/shared-box,finance/agent/bot,hr/toolbox/shared-box"
	if strings.Join(keys, ",") != want {
		t.Fatalf("keys = %v", keys)
	}
}

func TestHashesTrackContentAndAttachedToolboxes(t *testing.T) {
	doc := func(description, instructions string) []Item {
		items, _ := desired(t, mcps, `toolboxes: [{name: box, description: `+description+`, tools: [{name: gt, type: mcp, reference: graph}]}]`,
			`agents: [{name: bot, instructions: `+instructions+`, toolboxes: [box]}]`)
		return items
	}
	a, b := doc("one", "x"), doc("one", "x")
	if a[0].Hash != b[0].Hash || a[1].Hash != b[1].Hash {
		t.Fatal("hashes must be stable")
	}
	c := doc("two", "x")
	if c[0].Hash == a[0].Hash || c[1].Hash == a[1].Hash {
		t.Fatal("a toolbox change must change the agent that attaches it")
	}
	d := doc("one", "y")
	if d[0].Hash != a[0].Hash || d[1].Hash == a[1].Hash {
		t.Fatal("an agent change must not change the toolbox")
	}
}

func item(project, kind, name, hash string) Item {
	body := map[string]any{"tools": []any{}}
	if kind == KindAgent {
		body = map[string]any{"definition": map[string]any{"kind": "prompt", "model": "m", "tools": []any{}}}
	}
	return Item{Project: project, Kind: kind, Name: name, Hash: hash, Body: body}
}

func TestDiffAndDestroyApproval(t *testing.T) {
	st := NewState("dev")
	st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "same", Hash: "h1", Version: "1"})
	st.Set(StateItem{Project: "p", Kind: KindAgent, Name: "changed", Hash: "old", Version: "2"})
	st.Set(StateItem{Project: "p", Kind: KindAgent, Name: "gone-agent", Hash: "x", Version: "1"})
	st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "gone-box", Hash: "x", Version: "1"})
	actions := Diff([]Item{
		item("p", KindToolbox, "same", "h1"), item("p", KindAgent, "changed", "new"), item("p", KindToolbox, "fresh", "h"),
	}, st)
	var got []string
	for _, a := range actions {
		got = append(got, string(a.Op)+" "+a.Key)
	}
	want := "unchanged p/toolbox/same,update p/agent/changed,create p/toolbox/fresh,delete p/agent/gone-agent,delete p/toolbox/gone-box"
	if strings.Join(got, ",") != want {
		t.Fatalf("actions = %v", got)
	}
	blocked := CheckDestroy(actions, false)
	if len(blocked) != 2 || blocked[0].Code != "XF025" || !strings.Contains(blocked[0].Message, "--allow-destroy") {
		t.Fatalf("blocked = %v", blocked)
	}
	if CheckDestroy(actions, true) != nil || CheckDestroy(actions[:3], false) != nil {
		t.Fatal("approved or non-destructive plans are fine")
	}
	if Summary(actions) != "1 to create, 1 to update, 2 to delete, 1 unchanged" || Summary(nil) != "nothing to do" {
		t.Fatal(Summary(actions))
	}
}

// fake is an in-memory project data plane.
type fake struct {
	agents, toolboxes map[string]string // name -> latest version
	bodies            map[string]map[string]any
	calls             []string
	failOn            string
}

func newFake() *fake {
	return &fake{agents: map[string]string{}, toolboxes: map[string]string{}, bodies: map[string]map[string]any{}}
}

func (f *fake) record(call string) error {
	f.calls = append(f.calls, call)
	if f.failOn == call {
		return errors.New("boom")
	}
	return nil
}

func bump(m map[string]string, name string) string {
	var v int
	_, _ = fmt.Sscanf(m[name], "%d", &v)
	m[name] = fmt.Sprint(v + 1)
	return m[name]
}

func (f *fake) GetAgent(_ context.Context, name string) (*foundry.Resource, error) {
	if err := f.record("get agent " + name); err != nil {
		return nil, err
	}
	if v, ok := f.agents[name]; ok {
		return &foundry.Resource{Name: name, Version: v}, nil
	}
	return nil, nil
}

func (f *fake) CreateAgentVersion(_ context.Context, name string, body map[string]any) (*foundry.Resource, error) {
	if err := f.record("create agent " + name); err != nil {
		return nil, err
	}
	f.bodies["agent/"+name] = body
	return &foundry.Resource{Name: name, Version: bump(f.agents, name)}, nil
}

func (f *fake) DeleteAgent(_ context.Context, name string) error {
	delete(f.agents, name)
	return f.record("delete agent " + name)
}

func (f *fake) GetToolbox(_ context.Context, name string) (*foundry.Resource, error) {
	if err := f.record("get toolbox " + name); err != nil {
		return nil, err
	}
	if v, ok := f.toolboxes[name]; ok {
		return &foundry.Resource{Name: name, Version: v}, nil
	}
	return nil, nil
}

func (f *fake) CreateToolboxVersion(_ context.Context, name string, body map[string]any) (*foundry.Resource, error) {
	if err := f.record("create toolbox " + name); err != nil {
		return nil, err
	}
	f.bodies["toolbox/"+name] = body
	return &foundry.Resource{Name: name, Version: bump(f.toolboxes, name)}, nil
}

func (f *fake) DeleteToolbox(_ context.Context, name string) error {
	delete(f.toolboxes, name)
	return f.record("delete toolbox " + name)
}

func (f *fake) ToolboxMCPURL(name, version string) string {
	return "https://p/toolboxes/" + name + "/versions/" + version + "/mcp"
}

func engine(f *fake, st *State, saves *int) *Engine {
	return &Engine{
		API: func(string) API { return f }, State: st,
		Persist: func(*State) error { *saves++; return nil },
	}
}

func TestApplyCreatesToolboxesBeforeAgentsAndAttachesThem(t *testing.T) {
	f, st, saves := newFake(), NewState("dev"), 0
	box, agent := item("p", KindToolbox, "box", "hb"), item("p", KindAgent, "bot", "ha")
	agent.Toolboxes = []string{"box"}
	var log []string
	e := engine(f, st, &saves)
	e.Log = func(s string) { log = append(log, s) }
	if err := e.Apply(context.Background(), Diff([]Item{box, agent}, st)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, ",") != "create toolbox box,create agent bot" || saves != 2 {
		t.Fatalf("calls %v, saves %d", f.calls, saves)
	}
	tools := f.bodies["agent/bot"]["definition"].(map[string]any)["tools"].([]any)
	attached := tools[len(tools)-1].(map[string]any)
	if attached["server_url"] != "https://p/toolboxes/box/versions/1/mcp" || attached["type"] != "mcp" || attached["server_label"] != "box" || attached["require_approval"] != "never" {
		t.Fatalf("attached = %v", attached)
	}
	if len(agent.Body["definition"].(map[string]any)["tools"].([]any)) != 0 {
		t.Fatal("the desired item must not be modified")
	}
	if it, ok := st.Find("p/agent/bot"); !ok || it.Version != "1" || it.Hash != "ha" {
		t.Fatalf("state = %+v", st.Items)
	}
	if len(log) != 2 || log[0] != "create p/toolbox/box" {
		t.Fatalf("log = %v", log)
	}
}

func TestUnchangedItemsAreCheckedAndRepaired(t *testing.T) {
	f, st, saves := newFake(), NewState("dev"), 0
	st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "ok", Hash: "h", Version: "1"})
	st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "missing", Hash: "h", Version: "1"})
	st.Set(StateItem{Project: "p", Kind: KindAgent, Name: "drifted", Hash: "h", Version: "1"})
	f.toolboxes["ok"], f.agents["drifted"] = "1", "5"
	var log []string
	e := engine(f, st, &saves)
	e.Log = func(s string) { log = append(log, s) }
	items := []Item{item("p", KindToolbox, "ok", "h"), item("p", KindToolbox, "missing", "h"), item("p", KindAgent, "drifted", "h")}
	if err := e.Apply(context.Background(), Diff(items, st)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, ",") != "get toolbox ok,get toolbox missing,create toolbox missing,get agent drifted,create agent drifted" {
		t.Fatalf("calls = %v", f.calls)
	}
	text := strings.Join(log, "\n")
	for _, want := range []string{"unchanged p/toolbox/ok", "recreated: it is missing from the project p/toolbox/missing", "redeployed: the project has version 5, not the recorded 1 p/agent/drifted"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if it, _ := st.Find("p/agent/drifted"); it.Version != "6" {
		t.Fatalf("state = %+v", it)
	}
}

func TestApplyDeletesAndStopsOnTheFirstFailure(t *testing.T) {
	f, st, saves := newFake(), NewState("dev"), 0
	st.Set(StateItem{Project: "p", Kind: KindAgent, Name: "old", Hash: "x", Version: "1"})
	st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "old-box", Hash: "x", Version: "1"})
	f.agents["old"], f.toolboxes["old-box"] = "1", "1"
	e := engine(f, st, &saves)
	if err := e.Apply(context.Background(), Diff(nil, st)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.calls, ",") != "delete agent old,delete toolbox old-box" || len(st.Items) != 0 || saves != 2 {
		t.Fatalf("calls %v, state %v, saves %d", f.calls, st.Items, saves)
	}

	// A failure keeps the progress made before it.
	f, st, saves = newFake(), NewState("dev"), 0
	f.failOn = "create agent bot"
	box, agent := item("p", KindToolbox, "box", "hb"), item("p", KindAgent, "bot", "ha")
	agent.Toolboxes = []string{"box"}
	err := engine(f, st, &saves).Apply(context.Background(), Diff([]Item{box, agent}, st))
	if err == nil || !strings.Contains(err.Error(), "create p/agent/bot: boom") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := st.Find("p/toolbox/box"); !ok || len(st.Items) != 1 || saves != 1 {
		t.Fatalf("state = %v, saves %d", st.Items, saves)
	}
	// Failures on each other call.
	for _, call := range []string{"get toolbox box", "delete agent old", "create toolbox box"} {
		f, st = newFake(), NewState("dev")
		st.Set(StateItem{Project: "p", Kind: KindToolbox, Name: "box", Hash: "hb", Version: "1"})
		st.Set(StateItem{Project: "p", Kind: KindAgent, Name: "old", Hash: "x", Version: "1"})
		f.failOn = call
		desired := []Item{item("p", KindToolbox, "box", "hb")}
		if call == "create toolbox box" {
			desired = []Item{item("p", KindToolbox, "box", "new")}
		}
		if err := engine(f, st, new(int)).Apply(context.Background(), Diff(desired, st)); err == nil {
			t.Errorf("%s: expected an error", call)
		}
	}
	// Persist failures stop the run too.
	f, st = newFake(), NewState("dev")
	e = &Engine{API: func(string) API { return f }, State: st, Persist: func(*State) error { return errors.New("disk full") }}
	if err := e.Apply(context.Background(), Diff([]Item{item("p", KindToolbox, "b", "h")}, st)); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v", err)
	}
}

func TestAttachingAToolboxThatWasNotDeployedFails(t *testing.T) {
	agent := item("p", KindAgent, "bot", "h")
	agent.Toolboxes = []string{"ghost"}
	e := &Engine{API: func(string) API { return newFake() }, State: NewState("dev")}
	if err := e.Apply(context.Background(), []Action{{Op: OpCreate, Key: agent.Key(), Item: agent}}); err == nil || !strings.Contains(err.Error(), "toolbox 'ghost' has not been deployed") {
		t.Fatalf("err = %v", err)
	}
}

func TestStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dev.state.json")
	st, err := LoadState(path, "dev")
	if err != nil || len(st.Items) != 0 || st.Environment != "dev" {
		t.Fatalf("%+v %v", st, err)
	}
	st.Set(StateItem{Project: "b", Kind: KindAgent, Name: "x", Hash: "1", Version: "1"})
	st.Set(StateItem{Project: "a", Kind: KindAgent, Name: "x", Hash: "1", Version: "1"})
	st.Set(StateItem{Project: "b", Kind: KindAgent, Name: "x", Hash: "2", Version: "2"}) // replaces
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadState(path, "dev")
	if err != nil || len(back.Items) != 2 || back.Items[0].Project != "a" || back.Items[1].Hash != "2" {
		t.Fatalf("%+v %v", back, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
	back.Remove("a/agent/x")
	back.Remove("zzz")
	if len(back.Items) != 1 {
		t.Fatal(back.Items)
	}

	if _, err := LoadState(path, "prod"); err == nil || !strings.Contains(err.Error(), `belongs to environment "dev"`) {
		t.Fatalf("err = %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	for content, want := range map[string]string{"{": "not valid", `{"schemaVersion": 99, "environment": "dev"}`: "format 99"} {
		_ = os.WriteFile(bad, []byte(content), 0o600)
		if _, err := LoadState(bad, "dev"); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", content, err)
		}
	}
	if _, err := LoadState(t.TempDir(), "dev"); err == nil {
		t.Fatal("a directory is not a state file")
	}
}

func TestSaveFailures(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	_ = os.WriteFile(blocker, []byte("x"), 0o600)
	st := NewState("dev")
	if err := st.Save(filepath.Join(blocker, "x", "state.json")); err == nil {
		t.Fatal("cannot create a directory below a file")
	}
	target := filepath.Join(dir, "target")
	_ = os.MkdirAll(filepath.Join(target, "child"), 0o750)
	if err := st.Save(target); err == nil {
		t.Fatal("cannot replace a directory with a file")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestClipLimitsLongValues(t *testing.T) {
	if clip("abc", 2) != "ab" || clip("abc", 5) != "abc" {
		t.Fatal("clip")
	}
}
