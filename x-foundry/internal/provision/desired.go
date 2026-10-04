// Package provision computes what the Foundry data plane should contain (toolboxes and prompt
// agents), compares it with what an earlier deployment recorded, and applies the difference.
package provision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/config"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/normalise"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
)

// Kinds of data-plane item.
const (
	KindToolbox = "toolbox"
	KindAgent   = "agent"
)

// Item is one data-plane object the configuration asks for.
type Item struct {
	Project string
	Kind    string
	Name    string
	// Body is the request body of the create-version call. Agents attach toolboxes through
	// Toolboxes, because the toolbox's MCP URL contains a version that is only known when the
	// toolbox has been deployed; the engine adds those tools at apply time.
	Body      map[string]any
	Toolboxes []string
	// Hash identifies the content, including the content of the toolboxes an agent attaches.
	Hash string
}

// Key identifies an item across runs.
func (i Item) Key() string { return Key(i.Project, i.Kind, i.Name) }

// Key joins the parts that identify an item.
func Key(project, kind, name string) string { return project + "/" + kind + "/" + name }

// Desired lists the toolboxes and prompt agents of every project, toolboxes first, with
// warnings for what is skipped or needs something this phase does not create.
func Desired(p *plan.Plan) ([]Item, []diag.Diagnostic) {
	var items []Item
	var diags []diag.Diagnostic
	env := p.Config.Environment
	for _, project := range p.Config.Projects {
		toolboxes := map[string]Item{}
		var order []string
		for _, tb := range project.Toolboxes {
			item, warn := toolboxItem(project, tb)
			diags = append(diags, warn...)
			toolboxes[tb.Name] = item
			order = append(order, tb.Name)
		}
		for _, name := range order {
			items = append(items, toolboxes[name])
		}
		for _, a := range project.Agents {
			item, ok, warn := agentItem(project, a, env, toolboxes)
			diags = append(diags, warn...)
			if ok {
				items = append(items, item)
			}
		}
	}
	return items, diag.Dedupe(diags)
}

func where(project *normalise.EffectiveProject, kind, name string) string {
	return fmt.Sprintf("x-foundry.projects[%s].%s[%s]", project.Name, kind, name)
}

func mcpByName(project *normalise.EffectiveProject, name string) (config.Mcp, bool) {
	for _, m := range project.Mcps {
		if m.Name == name {
			return m, true
		}
	}
	return config.Mcp{}, false
}

// mcpTool renders an MCP server as a tool. A server with an allow-list may be called without
// asking; one without is called only after approval, the safe default.
func mcpTool(project *normalise.EffectiveProject, m config.Mcp, path string) (map[string]any, []diag.Diagnostic) {
	tool := map[string]any{"type": "mcp", "server_label": m.Name, "server_url": m.Endpoint}
	if len(m.AllowedTools) > 0 {
		tool["allowed_tools"] = m.AllowedTools
		tool["require_approval"] = "never"
	} else {
		tool["require_approval"] = "always"
	}
	if len(m.Headers) > 0 {
		tool["headers"] = m.Headers
	}
	var diags []diag.Diagnostic
	if a := m.Authentication; a != nil && a.Mode != "none" {
		tool["project_connection_id"] = m.Name
		diags = append(diags, diag.Warn("XF210", path, "MCP server '%s' uses %s authentication through a project connection named '%s'; x-foundry does not create that connection yet, so create it in the project first", m.Name, a.Mode, m.Name))
	}
	return tool, diags
}

func hash(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, _ := json.Marshal(p) // maps marshal with sorted keys, so the hash is stable
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func toolboxItem(project *normalise.EffectiveProject, tb config.Toolbox) (Item, []diag.Diagnostic) {
	var diags []diag.Diagnostic
	tools := []any{}
	for _, t := range tb.Tools {
		path := where(project, "toolboxes", tb.Name) + ".tools[" + t.Name + "]"
		switch t.Type {
		case "mcp":
			m, ok := mcpByName(project, t.Reference)
			if !ok {
				continue // validation already reported the missing MCP
			}
			tool, w := mcpTool(project, m, path)
			tools = append(tools, tool)
			diags = append(diags, w...)
		case "codeInterpreter":
			tools = append(tools, map[string]any{"type": "code_interpreter"})
		default:
			diags = append(diags, diag.Warn("XF211", path, "tool type '%s' is not deployed yet (supported: mcp, codeInterpreter); the tool is left out of toolbox '%s'", t.Type, tb.Name))
		}
	}
	body := map[string]any{"tools": tools}
	if tb.Description != "" {
		body["description"] = tb.Description
	}
	return Item{Project: project.Name, Kind: KindToolbox, Name: tb.Name, Body: body, Hash: hash(KindToolbox, body)}, diags
}

func agentItem(project *normalise.EffectiveProject, a config.Agent, env string, toolboxes map[string]Item) (Item, bool, []diag.Diagnostic) {
	path := where(project, "agents", a.Name)
	if a.Kind == "hosted" {
		return Item{}, false, []diag.Diagnostic{diag.Warn("XF211", path, "hosted agent '%s' is not deployed by x-foundry: declare it as an azd service with host azure.ai.agent", a.Name)}
	}
	var diags []diag.Diagnostic
	tools := []any{}
	for _, name := range a.Mcps {
		if m, ok := mcpByName(project, name); ok {
			tool, w := mcpTool(project, m, path)
			tools = append(tools, tool)
			diags = append(diags, w...)
		}
	}
	var attached []string
	var attachedHashes []string
	for _, name := range a.Toolboxes {
		if tb, ok := toolboxes[name]; ok {
			attached = append(attached, name)
			attachedHashes = append(attachedHashes, tb.Hash)
		}
	}
	if len(a.KnowledgeBases) > 0 {
		diags = append(diags, diag.Warn("XF211", path, "knowledge bases %v are not attached yet (Foundry IQ is Phase 4); agent '%s' is deployed without them", a.KnowledgeBases, a.Name))
	}
	model := a.Model
	if model == "" {
		model = project.Models.Default
	}
	definition := map[string]any{"kind": "prompt", "model": model, "tools": tools}
	if a.Instructions != "" {
		definition["instructions"] = a.Instructions
	}
	body := map[string]any{
		"definition": definition,
		"metadata":   metadata(project, a, env),
	}
	sort.Strings(attached)
	item := Item{Project: project.Name, Kind: KindAgent, Name: a.Name, Body: body, Toolboxes: attached}
	item.Hash = hash(KindAgent, body, attached, attachedHashes)
	return item, true, diags
}

// metadata marks what x-foundry owns; values stay within the service limits (16 pairs, 64 and
// 512 characters).
func metadata(project *normalise.EffectiveProject, a config.Agent, env string) map[string]string {
	m := map[string]string{}
	for k, v := range a.Tags {
		if len(m) < 13 && len(k) <= 64 {
			m[k] = clip(v, 512)
		}
	}
	m["managed-by"] = "x-foundry"
	m["x-foundry-env"] = env
	m["x-foundry-id"] = clip(Key(project.Name, KindAgent, a.Name), 512)
	return m
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
