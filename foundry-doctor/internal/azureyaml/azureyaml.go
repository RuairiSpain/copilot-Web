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
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Diagnostic is a structural problem found while parsing.
type Diagnostic struct {
	Message  string       `json:"message"`
	Location sdk.Location `json:"location"`
	// Path is the dotted key path of the mapping that holds the problem, for
	// duplicate keys. Rules use it so an ordinal among identical problems does
	// not shift when an unrelated duplicate is added elsewhere.
	Path string `json:"path,omitempty"`
}

// MultiDocumentMessage is the diagnostic recorded when the file holds more
// than one YAML document. azd decodes only the first document (project.Parse
// calls yaml.Unmarshal from github.com/braydonk/yaml v0.9.0, which parses one
// document), so the rest is ignored by azd and by this tool.
const MultiDocumentMessage = "multiple YAML documents: azd reads only the first document"

// Document is a parsed azure.yaml. It implements sdk.AzureYAMLView.
type Document struct {
	path        string
	root        *yaml.Node
	resolved    *yaml.Node
	diagnostics []Diagnostic
}

var _ sdk.AzureYAMLView = (*Document)(nil)

// ErrEmpty is returned for an empty document.
var ErrEmpty = errors.New("azure.yaml is empty")

// Parse parses data strictly. Syntax errors, empty input and a non-mapping
// root return an error. Duplicate keys, unresolved uses references and
// further YAML documents after the first (which azd ignores) are recorded as
// diagnostics.
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
	extraErr := dec.Decode(&extra)
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse %s: top level must be a mapping", path)
	}
	d := &Document{path: path, root: root.Content[0]}
	if extraErr == nil || !errors.Is(extraErr, io.EOF) {
		// azd never reads past the first document, so neither a second document
		// nor a syntax error inside it can change what azd loads.
		line := extra.Line
		if line == 0 {
			line = 1
		}
		d.diagnostics = append(d.diagnostics, Diagnostic{Message: MultiDocumentMessage, Location: sdk.Location{File: path, Line: line, Column: 1}})
	}
	d.checkDuplicates(d.root, nil, 0)
	d.resolve()
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

// Resolved returns a copy of the root mapping in which every alias is
// replaced by a copy of its anchor and every merge key (<<) is expanded.
// Explicit keys win over merged keys; earlier merge sources win over later
// ones. Nodes keep the line and column of the node they were copied from.
// Expansion is bounded; when the bound is hit the remaining aliases are left
// unexpanded and a diagnostic is recorded.
func (d *Document) Resolved() *yaml.Node { return d.resolved }

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
					Path:     strings.Join(trail, "."),
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

const (
	maxResolvedNodes = 100000
	maxResolveDepth  = 64
)

type resolver struct {
	budget  int
	tripped bool
}

func (d *Document) resolve() {
	r := &resolver{budget: maxResolvedNodes}
	d.resolved = r.node(d.root, 0)
	if r.tripped {
		d.diagnostics = append(d.diagnostics, Diagnostic{
			Message:  "alias expansion limit exceeded; aliases and merge keys beyond the limit are not resolved",
			Location: d.loc(d.root),
		})
	}
}

func isMergeKey(k *yaml.Node) bool {
	return k.Kind == yaml.ScalarNode && k.Tag == "!!merge"
}

func (r *resolver) node(n *yaml.Node, depth int) *yaml.Node {
	if n == nil {
		return nil
	}
	if r.budget <= 0 || depth > maxResolveDepth {
		r.tripped = true
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "", Line: n.Line, Column: n.Column}
	}
	r.budget--
	switch n.Kind {
	case yaml.AliasNode:
		return r.node(n.Alias, depth+1)
	case yaml.MappingNode:
		out := &yaml.Node{Kind: yaml.MappingNode, Tag: n.Tag, Style: n.Style, Line: n.Line, Column: n.Column}
		seen := map[string]int{}
		put := func(k, v *yaml.Node) {
			if i, dup := seen[k.Value]; dup {
				out.Content[i+1] = v // last explicit duplicate wins, as in child()
				return
			}
			seen[k.Value] = len(out.Content)
			out.Content = append(out.Content, k, v)
		}
		var merged [][2]*yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if isMergeKey(k) {
				for _, src := range r.mergeSources(v, depth+1) {
					for j := 0; j+1 < len(src.Content); j += 2 {
						merged = append(merged, [2]*yaml.Node{src.Content[j], src.Content[j+1]})
					}
				}
				continue
			}
			put(r.node(k, depth+1), r.node(v, depth+1))
		}
		for _, m := range merged {
			if _, explicit := seen[m[0].Value]; !explicit {
				put(m[0], m[1])
			}
		}
		return out
	case yaml.SequenceNode, yaml.DocumentNode:
		out := &yaml.Node{Kind: n.Kind, Tag: n.Tag, Style: n.Style, Line: n.Line, Column: n.Column}
		for _, c := range n.Content {
			out.Content = append(out.Content, r.node(c, depth+1))
		}
		return out
	}
	cp := *n
	cp.Anchor = ""
	return &cp
}

// mergeSources returns the resolved mappings named by a merge value: a single
// mapping or alias, or a sequence of them. Non-mappings are ignored.
func (r *resolver) mergeSources(v *yaml.Node, depth int) []*yaml.Node {
	rv := r.node(v, depth)
	switch rv.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{rv}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, c := range rv.Content {
			if c.Kind == yaml.MappingNode {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
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
func (d *Document) ServiceNames() []string { return keys(child(d.resolved, "services")) }

// ResourceNames returns the sorted names under resources.
func (d *Document) ResourceNames() []string { return keys(child(d.resolved, "resources")) }

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
	n := d.resolved
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
	svc := child(child(d.resolved, "services"), service)
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
