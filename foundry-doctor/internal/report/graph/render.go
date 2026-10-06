package graphreport

import (
	"bytes"
	"fmt"
	"html"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/mermaid"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func Markdown(g view.Graph) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Foundry Doctor graph (%s)\n\n", g.Mode)
	b.WriteString("| Origin | Meaning |\n|---|---|\n")
	b.WriteString("| local | Present in source only |\n")
	b.WriteString("| azure | Present in deployed Azure state only |\n")
	b.WriteString("| both | Correlated between source and Azure |\n")
	b.WriteString("| external | Referenced outside the project scope |\n")
	b.WriteString("| missing | Referenced but unresolved |\n\n")
	fmt.Fprintf(&b, "- Nodes: %d\n- Edges: %d\n", g.Summary.NodeCount, g.Summary.EdgeCount)
	if g.Summary.CollapsedNodes > 0 {
		fmt.Fprintf(&b, "- Collapsed nodes: %d\n", g.Summary.CollapsedNodes)
	}
	b.WriteString("\n```mermaid\n")
	b.Write(mermaid.Render(g))
	b.WriteString("```\n")
	return []byte(b.String())
}

func HTML(g view.Graph) []byte {
	md := Markdown(g)
	var b bytes.Buffer
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Foundry Doctor graph</title>")
	b.WriteString("<style>body{font-family:Segoe UI,Arial,sans-serif;line-height:1.4;margin:2rem}pre{background:#0f172a;color:#e2e8f0;padding:1rem;overflow:auto}table{border-collapse:collapse}td,th{border:1px solid #cbd5e1;padding:.4rem .6rem}</style>")
	b.WriteString("</head><body><h1>Foundry Doctor graph</h1><p>Accessibility: the Mermaid source below is kept as text so screen readers and code review tools can inspect the exact rendered graph.</p><pre>")
	b.WriteString(html.EscapeString(string(md)))
	b.WriteString("</pre></body></html>")
	return b.Bytes()
}
