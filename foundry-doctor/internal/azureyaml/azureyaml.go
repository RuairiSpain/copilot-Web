// Package azureyaml parses azure.yaml into a lossless yaml.v3 node tree that
// keeps line and column positions, detects duplicate keys and resolves
// services.*.uses references. It never evaluates or expands values.
package azureyaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Diagnostic is a structural problem found while parsing.
type Diagnostic struct {
	Message  string       `json:"message"`
	Location sdk.Location `json:"location"`
}

// Document is a parsed azure.yaml. It implements sdk.AzureYAMLView.
type Document struct {
	path        string
	root        *yaml.Node
	diagnostics []Diagnostic
}

var _ sdk.AzureYAMLView = (*Document)(nil)

// ErrEmpty is returned for an empty document.
var ErrEmpty = errors.New("azure.yaml is empty")

// Parse parses data strictly. Syntax errors, multiple documents, empty input
// and a non-mapping root return an error. Duplicate keys and unresolved
// uses references are recorded as diagnostics.
func Parse(data []byte, path string) (*Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: %w", path, ErrEmpty)
		}
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("parse %s: multiple YAML documents are not supported", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse %s: top level must be a mapping", path)
	}
	d := &Document{path: path, root: root.Content[0]}
	d.checkDuplicates(d.root, nil, 0)
	d.checkUses()
	sort.SliceStable(d.diagnostics, func(i, j int) bool {
		a, b := d.diagnostics[i].Location, d.diagnostics[j].Location
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return d, nil
}

// Path returns the path the document was parsed from.
func (d *Document) Path() string { return d.path }

// Root returns the lossless root mapping node.
func (d *Document) Root() *yaml.Node { return d.root }

// Diagnostics returns structural problems found at parse time.
func (d *Document) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), d.diagnostics...)
}

func (d *Document) loc(n *yaml.Node) sdk.Location {
	return sdk.Location{File: d.path, Line: n.Line, Column: n.Column}
}

const maxDepth = 64

func (d *Document) checkDuplicates(n *yaml.Node, trail []string, depth int) {
	if depth > maxDepth {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if seen[k.Value] {
				d.diagnostics = append(d.diagnostics, Diagnostic{
					Message:  fmt.Sprintf("duplicate key %q", k.Value),
					Location: d.loc(k),
				})
			}
			seen[k.Value] = true
			d.checkDuplicates(v, append(trail, k.Value), depth+1)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			d.checkDuplicates(c, trail, depth+1)
		}
	case yaml.AliasNode:
		// Aliases are not expanded; their anchors were already visited.
	}
}

func child(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			found = n.Content[i+1] // last duplicate wins, as in yaml.v3 maps
		}
	}
	return found
}

func keys(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := n.Content[i].Value; !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ServiceNames returns the sorted names under services.
func (d *Document) ServiceNames() []string { return keys(child(d.root, "services")) }

// ResourceNames returns the sorted names under resources.
func (d *Document) ResourceNames() []string { return keys(child(d.root, "resources")) }

// Lookup returns the scalar text at path and its location. Non-scalar nodes
// report ok=false; use LookupAny for those.
func (d *Document) Lookup(path ...string) (string, sdk.Location, bool) {
	v, loc, ok := d.LookupAny(path...)
	if !ok {
		return "", loc, false
	}
	switch v.(type) {
	case map[string]any, []any, nil:
		return "", loc, false
	}
	return fmt.Sprint(v), loc, true
}

// LookupAny walks path through mappings and returns the decoded value and the
// location of the value node.
func (d *Document) LookupAny(path ...string) (any, sdk.Location, bool) {
	n := d.root
	for _, p := range path {
		n = child(n, p)
		if n == nil {
			return nil, sdk.Location{}, false
		}
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, d.loc(n), false
	}
	return v, d.loc(n), true
}

// Uses returns the uses references of a service in document order.
func (d *Document) Uses(service string) []Ref {
	svc := child(child(d.root, "services"), service)
	u := child(svc, "uses")
	if u == nil || u.Kind != yaml.SequenceNode {
		return nil
	}
	var out []Ref
	for _, it := range u.Content {
		if it.Kind == yaml.ScalarNode {
			out = append(out, Ref{Name: it.Value, Location: d.loc(it)})
		}
	}
	return out
}

// Ref is a named reference with its location.
type Ref struct {
	Name     string
	Location sdk.Location
}

func (d *Document) checkUses() {
	known := map[string]bool{}
	for _, n := range d.ServiceNames() {
		known[n] = true
	}
	for _, n := range d.ResourceNames() {
		known[n] = true
	}
	for _, s := range d.ServiceNames() {
		for _, r := range d.Uses(s) {
			if !known[r.Name] {
				d.diagnostics = append(d.diagnostics, Diagnostic{
					Message:  fmt.Sprintf("services.%s.uses references unknown name %q", s, r.Name),
					Location: r.Location,
				})
			}
		}
	}
}
