package bicep

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Confidence of a source location.
type Confidence string

// Confidence levels.
const (
	ConfidenceCertain   Confidence = "certain"   // from a compiler diagnostic
	ConfidenceLikely    Confidence = "likely"    // file inferred (entry or module), pointer exact
	ConfidenceUncertain Confidence = "uncertain" // only the ARM pointer is known
)

// Location is a source location for an ARM-derived finding. Line 0 means
// unknown; line numbers are never fabricated because ARM JSON has no source map.
type Location struct {
	File        string     `json:"file,omitempty"`
	Line        int        `json:"line,omitempty"`
	Column      int        `json:"column,omitempty"`
	ARMPointer  string     `json:"armPointer,omitempty"`
	Confidence  Confidence `json:"confidence"`
	Guards      []string   `json:"guards,omitempty"`
	Description string     `json:"description,omitempty"`
}

// ModuleFiles maps a module deployment name (the `name:` value, as it appears
// in the ARM resource) to the module's source file.
type ModuleFiles map[string]string

// Mapper maps ARM resources to source locations.
type Mapper struct {
	Entry   string
	Modules ModuleFiles
}

// Locate returns the best location for resource r found at nesting path
// `ancestors` (outermost module deployment first). A resource inside a module
// whose deployment name is known maps to that module file with confidence
// "likely"; otherwise to the entry file with "likely" for top-level resources
// and "uncertain" for unresolved nested ones.
func (m Mapper) Locate(r Resource, ancestors []Resource) Location {
	loc := Location{ARMPointer: r.Pointer, Guards: r.Guards()}
	if len(ancestors) == 0 {
		loc.File, loc.Confidence = m.Entry, ConfidenceLikely
		if m.Entry == "" {
			loc.Confidence = ConfidenceUncertain
		}
		return loc
	}
	inner := ancestors[len(ancestors)-1]
	if f, ok := m.lookup(inner.Name); ok {
		loc.File, loc.Confidence = f, ConfidenceLikely
		loc.Description = "inside module " + inner.Name
		return loc
	}
	loc.File, loc.Confidence = m.Entry, ConfidenceUncertain
	loc.Description = "inside unresolved module deployment"
	return loc
}

// LookupModule returns the source file of the module deployment with the given
// ARM name (a literal, or an expression holding a known literal).
func (m Mapper) LookupModule(name string) (string, bool) { return m.lookup(name) }

// TemplateHash returns metadata._generator.templateHash of the nested template
// of a module deployment resource node (the decoded JSON object of the
// Microsoft.Resources/deployments resource), or "" when absent. A module file
// compiled on its own has the same hash as the template nested in the caller,
// which maps a nested template to its source file without reading Bicep.
func TemplateHash(deployment map[string]any) string {
	props, _ := deployment["properties"].(map[string]any)
	tpl, _ := props["template"].(map[string]any)
	meta, _ := tpl["metadata"].(map[string]any)
	gen, _ := meta["_generator"].(map[string]any)
	h, _ := gen["templateHash"].(string)
	return h
}

// RootTemplateHash returns metadata._generator.templateHash of a compiled ARM
// template, or "" when absent or the document is not JSON. A module file
// compiled alone has the hash that TemplateHash reads from the template nested
// in its caller.
func RootTemplateHash(arm []byte) string {
	var doc struct {
		Metadata struct {
			Generator struct {
				Hash string `json:"templateHash"`
			} `json:"_generator"`
		} `json:"metadata"`
	}
	if json.Unmarshal(arm, &doc) != nil {
		return ""
	}
	return doc.Metadata.Generator.Hash
}

// NestedTemplateHashes returns the templateHash of every module deployment
// (a Microsoft.Resources/deployments resource with an inlined template) in a
// compiled ARM document, at any depth, sorted and without duplicates. At most
// limit hashes are returned.
func NestedTemplateHashes(arm []byte, limit int) []string {
	t, err := ParseARM(arm)
	if err != nil {
		return nil
	}
	var doc any
	if json.Unmarshal(arm, &doc) != nil {
		return nil
	}
	seen := map[string]bool{}
	t.Walk(func(r Resource, _ int) {
		if r.Nested == nil || len(seen) >= limit {
			return
		}
		if h := TemplateHash(pointerNode(doc, r.Pointer)); h != "" {
			seen[h] = true
		}
	})
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// pointerNode follows an RFC 6901 pointer to a JSON object, nil if absent.
func pointerNode(doc any, ptr string) map[string]any {
	cur := doc
	for _, seg := range strings.Split(strings.TrimPrefix(ptr, "/"), "/") {
		if seg == "" {
			continue
		}
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		switch c := cur.(type) {
		case map[string]any:
			cur = c[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(c) {
				return nil
			}
			cur = c[i]
		default:
			return nil
		}
	}
	m, _ := cur.(map[string]any)
	return m
}

func (m Mapper) lookup(name string) (string, bool) {
	if f, ok := m.Modules[name]; ok {
		return f, true
	}
	// Names are often expressions such as "[format('{0}', 'single')]"; match a
	// quoted literal that equals a known key.
	for k, f := range m.Modules {
		if strings.Contains(name, "'"+k+"'") {
			return f, true
		}
	}
	return "", false
}

// FromDiagnostic converts a compiler diagnostic to an exact location.
func FromDiagnostic(d Diagnostic) Location {
	return Location{File: d.File, Line: d.Line, Column: d.Column, Confidence: ConfidenceCertain}
}

// LocatedResource pairs a resource with its location.
type LocatedResource struct {
	Resource Resource
	Location Location
	Depth    int
}

// LocateAll walks the template and maps every resource.
func (m Mapper) LocateAll(t *Template) []LocatedResource {
	var out []LocatedResource
	var walk func(t *Template, anc []Resource)
	walk = func(t *Template, anc []Resource) {
		for _, r := range t.Resources {
			out = append(out, LocatedResource{Resource: r, Location: m.Locate(r, anc), Depth: len(anc)})
			if r.Nested != nil {
				walk(r.Nested, append(append([]Resource(nil), anc...), r))
			}
		}
	}
	walk(t, nil)
	return out
}
