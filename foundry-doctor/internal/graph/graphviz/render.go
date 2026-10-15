package graphviz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/graph/view"
)

var ErrNotFound = errors.New("graphviz not found")

type Binary struct {
	LookPath func(string) (string, error)
	Command  func(context.Context, string, ...string) *exec.Cmd
}

func RenderDOT(g view.Graph) []byte {
	var b strings.Builder
	b.WriteString("digraph foundry_doctor {\n  rankdir=LR;\n")
	for _, n := range g.Nodes {
		fmt.Fprintf(&b, "  %s [label=\"%s\"];\n", n.ID, escape(n.Label))
	}
	for _, e := range g.Edges {
		fmt.Fprintf(&b, "  %s -> %s [label=\"%s\"];\n", e.From, e.To, escape(e.Kind))
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("graphviz dot -T%s: %w", format, err)
	}
	return out, nil
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
