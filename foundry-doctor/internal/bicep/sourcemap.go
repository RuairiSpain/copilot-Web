package bicep

import "strings"

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
