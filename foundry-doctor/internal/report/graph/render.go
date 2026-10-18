// Package graphreport wraps a projected graph in Markdown and HTML with a
// legend and text tables of nodes and edges, so the graph can be read
// without rendering Mermaid and without relying on colour.
package graphreport

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/mermaid"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

// legend rows: class, meaning and the non-colour cue in the Mermaid output.
var legend = [][3]string{
	{"local", "Declared in source; not in the deployed inventory (combined mode) or source-only (source mode)", "solid outline"},
	{"azure", "Present in deployed Azure state only", "solid outline"},
	{"both", "Declared in source and correlated with deployed Azure state", "solid outline"},
	{"unobserved", "Declared in source, or referenced in scope, with a type the deployed inventory cannot observe, so its deployment is unknown", "dotted outline"},
	{"external", "Referenced outside the graphed scope", "solid outline"},
	{"missing", "Referenced but unresolved: a name that matches nothing, or an in-scope ARM ID of an observable type the inventory did not return", "dashed outline"},
	{"collapsed", "Summary bucket for nodes over the size limit", "dashed outline"},
	{"unhealthy", "Failed or canceled provisioning, connection error, or an error finding", "thick outline"},
	{"baselined", "Has baselined findings", "fine-dashed outline"},
	{"added, removed, changed", "Diff graph only: node state of target compared with base", "thick or long-dashed outline"},
}

func Markdown(g view.Graph) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Foundry Doctor graph (%s)\n\n", g.Mode)
	b.WriteString("| Class | Meaning | Non-colour cue |\n|---|---|---|\n")
	for _, row := range legend {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", row[0], row[1], row[2])
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- Nodes: %d\n- Edges: %d\n", g.Summary.NodeCount, g.Summary.EdgeCount)
	if g.Summary.CollapsedNodes > 0 {
		fmt.Fprintf(&b, "- Collapsed nodes: %d\n", g.Summary.CollapsedNodes)
	}
	if g.Summary.CollapsedEdges > 0 {
		fmt.Fprintf(&b, "- Omitted edges: %d\n", g.Summary.CollapsedEdges)
	}
	if g.Summary.UnmatchedFindings > 0 {
		fmt.Fprintf(&b, "- Findings not matched to a node: %d\n", g.Summary.UnmatchedFindings)
	}
	if g.Mode == view.ModeDiff {
		fmt.Fprintf(&b, "- Added nodes: %d\n- Removed nodes: %d\n- Changed nodes: %d\n", g.Summary.AddedNodes, g.Summary.RemovedNodes, g.Summary.ChangedNodes)
	}
	for _, note := range g.Summary.Notes {
		fmt.Fprintf(&b, "- Note: %s\n", cell(note))
	}
	b.WriteString("\n```mermaid\n")
	b.Write(mermaid.Render(g))
	b.WriteString("```\n\n## Nodes\n\n| ID | Label | Kind | Classes | Details |\n|---|---|---|---|---|\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", n.ID, cell(n.Label), cell(n.Kind), strings.Join(view.NodeClasses(n), ", "), cell(nodeDetails(n)))
	}
	b.WriteString("\n## Edges\n\n| From | To | Kind | Details |\n|---|---|---|---|\n")
	for _, e := range g.Edges {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", e.From, e.To, cell(e.Kind), edgeDetails(e))
	}
	return []byte(b.String())
}

func nodeDetails(n view.Node) string {
	var parts []string
	if n.Health != "" {
		parts = append(parts, "health "+n.Health)
	}
	if n.Findings > 0 {
		parts = append(parts, fmt.Sprintf("findings %d (max %s)", n.Findings, n.Severity))
	}
	if n.Baselined > 0 {
		parts = append(parts, fmt.Sprintf("baselined %d", n.Baselined))
	}
	if n.Conditional {
		parts = append(parts, "conditional")
	}
	return strings.Join(parts, ", ")
}

func edgeDetails(e view.Edge) string {
	var parts []string
	for _, p := range []struct {
		on   bool
		name string
	}{{e.External, "external"}, {e.Missing, "missing"}, {e.Collapsed, "collapsed"}} {
		if p.on {
			parts = append(parts, p.name)
		}
	}
	if e.Diff != "" {
		parts = append(parts, e.Diff)
	}
	return strings.Join(parts, ", ")
}

// cellEncoder makes text safe inside a Markdown table cell.
var cellEncoder = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "|", `\|`, "`", "&#96;", "\r\n", " ", "\r", " ", "\n", " ")

func cell(s string) string { return cellEncoder.Replace(s) }

func HTML(g view.Graph) []byte {
	md := Markdown(g)
	var b bytes.Buffer
	b.WriteString("<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Foundry Doctor graph</title>")
	b.WriteString("<style>body{font-family:Segoe UI,Arial,sans-serif;line-height:1.4;margin:2rem}pre{background:#0f172a;color:#e2e8f0;padding:1rem;overflow:auto}</style>")
	b.WriteString("</head><body><h1>Foundry Doctor graph</h1><p>Accessibility: the Markdown below carries the legend, the exact Mermaid source and text tables of every node and edge, so the graph can be reviewed without rendering it and without relying on colour.</p><pre>")
	b.WriteString(html.EscapeString(string(md)))
	b.WriteString("</pre></body></html>")
	return b.Bytes()
}
