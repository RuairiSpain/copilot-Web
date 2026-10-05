package azureyaml

import (
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// first returns the first child of mapping n with the given key (the same choice as model.Node.Lookup).
func first(n *model.Node, key string) *model.Node { return n.Lookup(key) }

// scalarOf returns the text of a scalar child, or "".
func scalarOf(n *model.Node, key string) string {
	c := first(n, key)
	if c == nil || c.Kind != model.NodeScalar {
		return ""
	}
	return c.Value
}

// isExtensionHost reports hosts whose service block belongs to an azure.ai.* extension (legacy microsoft.foundry included).
func isExtensionHost(h string) bool {
	return strings.HasPrefix(h, "azure.ai.") || h == "microsoft.foundry"
}

// project fills the typed fields of res.YAML and the data slices from the converted tree.
func project(res *Result) {
	a := res.YAML
	root := a.Root
	a.Name = scalarOf(root, "name")

	if infra := first(root, "infra"); infra != nil && infra.Kind == model.NodeMapping {
		a.Infra = model.Infra{
			Provider: scalarOf(infra, "provider"),
			Path:     scalarOf(infra, "path"),
			Module:   scalarOf(infra, "module"),
			Pos:      infra.KeyPos,
		}
	}
	a.Hooks = hooksOf(first(root, "hooks"))

	if svcs := first(root, "services"); svcs != nil && svcs.Kind == model.NodeMapping {
		seen := map[string]bool{}
		for _, s := range svcs.Children {
			if seen[s.Key] {
				continue
			}
			seen[s.Key] = true
			svc := model.Service{Name: s.Key, Node: s, Pos: s.KeyPos}
			if s.Kind == model.NodeMapping {
				svc.Host = scalarOf(s, "host")
				svc.Project = scalarOf(s, "project")
				svc.Hooks = hooksOf(first(s, "hooks"))
				if u := first(s, "uses"); u != nil && u.Kind == model.NodeSequence {
					for _, item := range u.Children {
						if item.Kind == model.NodeScalar {
							svc.Uses = append(svc.Uses, model.Ref{Name: item.Value, Pos: item.Pos})
						}
					}
				}
				if isExtensionHost(svc.Host) {
					res.Extensions = append(res.Extensions, ExtensionBlock{
						Service: s.Key, Host: svc.Host,
						Path: path{{s: "services"}}.key(s.Key).Dotted(),
						Pos:  s.KeyPos, Node: s,
					})
				}
			}
			a.Services = append(a.Services, svc)
		}
	}

	if r := first(root, "resources"); r != nil && r.Kind == model.NodeMapping {
		seen := map[string]bool{}
		for _, c := range r.Children {
			if !seen[c.Key] {
				seen[c.Key] = true
				res.Resources = append(res.Resources, model.Ref{Name: c.Key, Pos: c.KeyPos})
			}
		}
	}

	for _, c := range root.Children {
		if strings.HasPrefix(c.Key, "azure.ai.") {
			res.Extensions = append(res.Extensions, ExtensionBlock{Path: path{}.key(c.Key).Dotted(), Pos: c.KeyPos, Node: c})
		}
	}
	slices.SortStableFunc(res.Duplicates, func(x, y Duplicate) int {
		return comparePos(x.Positions[0], y.Positions[0])
	})
}

func comparePos(a, b model.Pos) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}

// hooksOf reads a hooks: mapping. Each event holds one hook mapping or a list of them.
func hooksOf(n *model.Node) []model.Hook {
	if n == nil || n.Kind != model.NodeMapping {
		return nil
	}
	var out []model.Hook
	for _, ev := range n.Children {
		switch ev.Kind {
		case model.NodeMapping:
			out = append(out, hookOf(ev.Key, ev, ev.KeyPos))
		case model.NodeSequence:
			for _, item := range ev.Children {
				if item.Kind == model.NodeMapping {
					out = append(out, hookOf(ev.Key, item, item.Pos))
				}
			}
		}
	}
	return out
}

func hookOf(event string, n *model.Node, pos model.Pos) model.Hook {
	return model.Hook{Event: event, Shell: scalarOf(n, "shell"), Run: scalarOf(n, "run"), Pos: pos}
}
