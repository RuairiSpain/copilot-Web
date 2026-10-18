// Package mermaid renders a projected graph as a Mermaid flowchart.
//
// The output is a restricted, deterministic subset of the flowchart syntax
// (https://mermaid.js.org/syntax/flowchart.html): opaque node IDs, quoted
// labels with Mermaid entity codes for characters that could end or alter the
// label, one `class` statement per class name, and accTitle/accDescr for
// accessibility (https://mermaid.js.org/config/accessibility.html).
package mermaid

import (
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

// classNames lists every class Render can assign, in classDef order.
var classNames = []string{"added", "azure", "baselined", "both", "changed", "collapsed", "external", "local", "missing", "removed", "unhealthy", "unobserved"}

func Render(g view.Graph) []byte {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	fmt.Fprintf(&b, "  accTitle: Foundry Doctor %s graph\n", escapeLabel(string(g.Mode)))
	fmt.Fprintf(&b, "  accDescr: %d nodes and %d edges, styled by origin and status as described in the legend\n", len(g.Nodes), len(g.Edges))
	members := map[string][]string{}
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", n.ID, escapeLabel(n.Label))
		for _, c := range view.NodeClasses(n) {
			members[c] = append(members[c], n.ID)
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
		label := escapeLabel(e.Kind)
		if e.Diff != "" {
			label += " " + escapeLabel(e.Diff)
		}
		fmt.Fprintf(&b, "  %s %s|%s| %s\n", e.From, arrow, label, e.To)
	}
	for _, line := range ClassDefs() {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, c := range classNames {
		if ids := members[c]; len(ids) > 0 {
			fmt.Fprintf(&b, "  class %s %s\n", strings.Join(ids, ","), c)
		}
	}
	return []byte(b.String())
}

// ClassDefs returns the classDef statements, one per class name returned by
// view.NodeClasses, sorted by name. Status classes also change the stroke
// width or dash pattern so they do not depend on colour alone.
func ClassDefs() []string {
	return []string{
		"classDef added fill:#dcfce7,stroke:#166534,color:#111827,stroke-width:3px",
		"classDef azure fill:#dcfce7,stroke:#166534,color:#111827",
		"classDef baselined fill:#f3e8ff,stroke:#7e22ce,color:#111827,stroke-dasharray: 2 2",
		"classDef both fill:#ede9fe,stroke:#6d28d9,color:#111827",
		"classDef changed fill:#fef9c3,stroke:#854d0e,color:#111827,stroke-width:3px",
		"classDef collapsed fill:#e5e7eb,stroke:#374151,color:#111827,stroke-dasharray: 3 3",
		"classDef external fill:#fef3c7,stroke:#92400e,color:#111827",
		"classDef local fill:#dbeafe,stroke:#1d4ed8,color:#111827",
		"classDef missing fill:#fee2e2,stroke:#991b1b,color:#111827,stroke-dasharray: 5 5",
		"classDef removed fill:#f3f4f6,stroke:#6b7280,color:#111827,stroke-dasharray: 8 4",
		"classDef unhealthy fill:#fee2e2,stroke:#991b1b,color:#111827,stroke-width:4px",
		"classDef unobserved fill:#f1f5f9,stroke:#475569,color:#111827,stroke-dasharray: 1 3",
	}
}

// labelEncoder replaces characters that could end a quoted label, start an
// entity or inject markup with Mermaid entity codes
// (https://mermaid.js.org/syntax/flowchart.html#entity-codes-to-escape-characters).
// It runs in a single pass, so the '#' it emits is never re-encoded.
var labelEncoder = strings.NewReplacer(
	"#", "#35;",
	`"`, "#quot;",
	"<", "#lt;",
	">", "#gt;",
	"&", "#amp;",
	"`", "#96;",
	"|", "#124;",
	"\r\n", " ",
	"\r", " ",
	"\n", " ",
)

func escapeLabel(s string) string {
	return labelEncoder.Replace(s)
}
