package model

import "strings"

// Pos is a 1-based source position. The zero value means unknown.
type Pos struct {
	Line   int
	Column int
}

// NodeKind is the YAML node type.
type NodeKind int

// NodeKind values.
const (
	NodeScalar NodeKind = iota
	NodeMapping
	NodeSequence
)

// Node is a lossless view of one YAML node with the position of its key (for mapping values) or itself.
// For a mapping, Children alternate in declaration order and each child carries its Key.
type Node struct {
	Kind     NodeKind
	Key      string // the mapping key that holds this node; empty for the root and sequence items
	KeyPos   Pos
	Value    string // scalar text; empty for collections. Never printed in reports unless a rule says so.
	Pos      Pos    // position of the value
	Children []*Node
}

// Lookup follows mapping keys from n. It returns nil if any key is missing or a node is not a mapping.
func (n *Node) Lookup(path ...string) *Node {
	cur := n
	for _, k := range path {
		if cur == nil || cur.Kind != NodeMapping {
			return nil
		}
		var next *Node
		for _, c := range cur.Children {
			if c.Key == k {
				next = c
				break
			}
		}
		cur = next
	}
	return cur
}

// Keys returns the mapping keys in declaration order.
func (n *Node) Keys() []string {
	if n == nil || n.Kind != NodeMapping {
		return nil
	}
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Key)
	}
	return out
}

// Ref is a reference from one azure.yaml element to another, such as a services.*.uses entry.
type Ref struct {
	Name string
	Pos  Pos
}

// Service is one entry of services:.
type Service struct {
	Name    string
	Host    string
	Project string
	Uses    []Ref
	Hooks   []Hook
	Node    *Node // the whole service mapping, for rules that read extension-specific keys
	Pos     Pos   // position of the service name key
}

// Hook is one azure.yaml hook entry.
type Hook struct {
	Event string // for example "postprovision"
	Shell string
	Run   string // may hold secrets or commands; rules must not echo it
	Pos   Pos
}

// Infra is the infra: block.
type Infra struct {
	Provider string
	Path     string
	Module   string
	Pos      Pos
}

// AzureYAML is the parsed azure.yaml. It never holds raw secrets by design: values stay in Root.
type AzureYAML struct {
	File     string // relative path
	Name     string
	Infra    Infra
	Services []Service
	Hooks    []Hook // project-level hooks
	Root     *Node
}

// Service returns the named service.
func (a *AzureYAML) Service(name string) (Service, bool) {
	if a == nil {
		return Service{}, false
	}
	for _, s := range a.Services {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

// IsEnvReference reports whether s is a ${VAR} style reference rather than a literal.
func IsEnvReference(s string) bool { return strings.Contains(s, "${") }
