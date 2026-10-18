package graphviz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

var ErrNotFound = errors.New("graphviz not found")

type Binary struct {
	LookPath func(string) (string, error)
	Command  func(context.Context, string, ...string) *exec.Cmd
}

// RenderDOT renders the graph as Graphviz DOT. Each label carries the node's
// classes as text, so origin and status do not depend on colour alone.
func RenderDOT(g view.Graph) []byte {
	var b strings.Builder
	b.WriteString("digraph foundry_doctor {\n  rankdir=LR;\n  node [shape=box];\n")
	for _, n := range g.Nodes {
		classes := view.NodeClasses(n)
		fmt.Fprintf(&b, "  %s [label=\"%s\\n(%s)\", style=\"%s\", fillcolor=\"%s\"];\n",
			n.ID, escape(n.Label), strings.Join(classes, ", "), nodeStyle(classes), fillColour(classes[0]))
	}
	for _, e := range g.Edges {
		label := escape(e.Kind)
		if e.Diff != "" {
			label += " " + escape(e.Diff)
		}
		if e.Missing || e.External || e.Diff == view.DiffRemoved {
			fmt.Fprintf(&b, "  %s -> %s [label=\"%s\", style=\"dashed\"];\n", e.From, e.To, label)
			continue
		}
		fmt.Fprintf(&b, "  %s -> %s [label=\"%s\"];\n", e.From, e.To, label)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func nodeStyle(classes []string) string {
	parts := []string{"filled"}
	switch {
	case slices.Contains(classes, "missing"), slices.Contains(classes, "collapsed"), slices.Contains(classes, "removed"):
		parts = append(parts, "dashed")
	case slices.Contains(classes, "unobserved"):
		parts = append(parts, "dotted")
	}
	if slices.Contains(classes, "unhealthy") || slices.Contains(classes, view.DiffAdded) || slices.Contains(classes, view.DiffChanged) {
		parts = append(parts, "bold")
	}
	return strings.Join(parts, ",")
}

func fillColour(origin string) string {
	switch origin {
	case "local":
		return "#dbeafe"
	case "azure":
		return "#dcfce7"
	case "both":
		return "#ede9fe"
	case "external":
		return "#fef3c7"
	case "missing":
		return "#fee2e2"
	case "unobserved":
		return "#f1f5f9"
	case "collapsed":
		return "#e5e7eb"
	default:
		return "#ffffff"
	}
}

// Render runs the optional Graphviz `dot` binary. ErrNotFound means the
// optional renderer is not installed; callers report the feature unavailable.
// Graphviz warnings on stderr never mix into the image bytes.
func (b Binary) Render(ctx context.Context, dot []byte, format string) ([]byte, error) {
	lp := b.LookPath
	if lp == nil {
		lp = exec.LookPath
	}
	path, err := lp("dot")
	if err != nil {
		return nil, ErrNotFound
	}
	cmdFn := b.Command
	if cmdFn == nil {
		cmdFn = exec.CommandContext
	}
	cmd := cmdFn(ctx, path, "-T"+format)
	cmd.Stdin = bytes.NewReader(dot)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("graphviz dot -T%s: %w (%s)", format, err, strings.TrimSpace(firstLine(stderr.String())))
	}
	return out, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, ";", ",")
	s = strings.ReplaceAll(s, "->", "→")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
