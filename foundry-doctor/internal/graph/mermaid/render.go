package mermaid

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

func Render(g view.Graph) []byte {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %s[%q]\n", n.ID, escapeLabel(n.Label))
		class := classifyNode(n)
		if class != "" {
			fmt.Fprintf(&b, "  class %s %s\n", n.ID, class)
		}
	}
	for _, e := range g.Edges {
		arrow := "-->"
		switch e.Kind {
		case "dependsOn":
			arrow = "-.->"
		case "deploys":
			arrow = "==>"
		}
		fmt.Fprintf(&b, "  %s %s|%s| %s\n", e.From, arrow, escapeEdgeLabel(e.Kind), e.To)
	}
	for _, line := range classDefs() {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func classDefs() []string {
	defs := []string{
		"classDef local fill:#dbeafe,stroke:#1d4ed8,color:#111827",
		"classDef azure fill:#dcfce7,stroke:#166534,color:#111827",
		"classDef both fill:#ede9fe,stroke:#6d28d9,color:#111827",
		"classDef external fill:#fef3c7,stroke:#92400e,color:#111827",
		"classDef missing fill:#fee2e2,stroke:#991b1b,color:#111827,stroke-dasharray: 5 5",
		"classDef unhealthy fill:#fee2e2,stroke:#991b1b,color:#111827",
		"classDef baselined fill:#f3e8ff,stroke:#7e22ce,color:#111827",
		"classDef collapsed fill:#e5e7eb,stroke:#374151,color:#111827,stroke-dasharray: 3 3",
	}
	sort.Strings(defs)
	return defs
}

func classifyNode(n view.Node) string {
	var classes []string
	switch n.Origin {
	case view.OriginAzure:
		classes = append(classes, "azure")
	case view.OriginBoth:
		classes = append(classes, "both")
	case view.OriginExternal:
		classes = append(classes, "external")
	case view.OriginMissing:
		classes = append(classes, "missing")
	case view.OriginCollapsed:
		classes = append(classes, "collapsed")
	default:
		classes = append(classes, "local")
	}
	if n.Unhealthy || n.Severity == "error" {
		classes = append(classes, "unhealthy")
	}
	if n.Baselined > 0 {
		classes = append(classes, "baselined")
	}
	return strings.Join(classes, ",")
}

func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "`", "'")
	repl := strings.NewReplacer("[", "(", "]", ")", "{", "(", "}", ")", "-->", "→", "==>", "⇒", "-.->", "⇢", "|", "/")
	s = repl.Replace(s)
	return s
}

func escapeEdgeLabel(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	return escapeLabel(s)
}
