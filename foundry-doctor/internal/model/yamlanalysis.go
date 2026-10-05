package model

import (
	"cmp"
	"slices"
	"strings"
)

// YAMLAnalysis is everything the reader found. YAML is the view the rule engine puts into Input; the other fields
// are data that rules turn into findings. Slices are in document order unless stated otherwise.
type YAMLAnalysis struct {
	YAML *AzureYAML

	// Duplicates lists every mapping key that occurs more than once in the same mapping. The model keeps all
	// occurrences in YAML.Root; typed fields (Services, Hooks) use the first occurrence.
	Duplicates []Duplicate
	// Interpolations lists every ${...} / ${{...}} reference found in scalar values. Nothing is evaluated.
	Interpolations []Interpolation
	// Resources are the names under the top-level resources: mapping (valid `uses` targets besides services).
	Resources []Ref
	// Extensions are the raw azure.ai.* (and legacy microsoft.foundry) service blocks and top-level azure.ai.* keys.
	Extensions []ExtensionBlock
	// UnknownTopLevel are top-level keys the vendored schema does not declare. The schema allows them.
	UnknownTopLevel []Ref
	// Issues are structural problems and notes, sorted by position. They are data, not parse errors.
	Issues []Issue
}

// Duplicate is a mapping key that appears more than once in one mapping.
type Duplicate struct {
	Path      string // dotted path of the key, for example services.assistant.name
	Key       string
	Positions []Pos // one per occurrence, in document order; len >= 2
}

// Form says which reference syntax an Interpolation uses.
type Form string

// Form values.
const (
	FormEnv            Form = "env"             // ${VAR}
	FormEnvDefault     Form = "env-default"     // ${VAR:-x} or ${VAR-x}
	FormEnvOperator    Form = "env-operator"    // another shell parameter operator, for example ${VAR:=x}, ${VAR:?}
	FormFoundry        Form = "foundry"         // ${{ ... }}, a Foundry runtime expression passed through by azd
	FormEscapedEnv     Form = "escaped-env"     // $${VAR}, a literal ${VAR}
	FormEscapedFoundry Form = "escaped-foundry" // $${{ ... }}, a literal ${{ ... }}
	FormMalformed      Form = "malformed"       // an unterminated or nameless ${...}
)

// Interpolation is one reference inside a scalar value. Pos is the position of the whole scalar: the exact column
// inside a quoted or multi-line scalar is not reliable, so Offset gives the byte offset within the decoded value.
type Interpolation struct {
	Path     string
	Pos      Pos
	Form     Form
	Name     string // variable name for the env forms and escaped-env; empty for foundry forms
	Operator string // for FormEnvDefault and FormEnvOperator, for example ":-"
	Nested   bool   // another ${ appears inside this reference's operator part
	Offset   int
}

// needsValue reports whether the reference fails when its variable has no value. Only the plain ${VAR} form and the
// operator forms that do not supply a default (":?", "?" and the others) can be unresolved; ${VAR:-x} cannot.
func (i Interpolation) needsValue() bool {
	switch i.Form {
	case FormEnv:
		return true
	case FormEnvOperator:
		return i.Operator == ":?" || i.Operator == "?"
	default:
		return false
	}
}

// ExtensionBlock is a raw extension-owned block with its location.
type ExtensionBlock struct {
	Service string // service name; empty for a top-level azure.ai.* key
	Host    string
	Path    string
	Pos     Pos // position of the key
	Node    *Node
}

// Level is the weight of an Issue.
type Level string

// Level values.
const (
	LevelError Level = "error" // the schema or azd rejects this
	LevelInfo  Level = "info"  // allowed by the schema, worth recording
)

// IssueCode identifies a kind of Issue.
type IssueCode string

// IssueCode values.
const (
	CodeMissingRequired    IssueCode = "missing-required"
	CodeInvalidType        IssueCode = "invalid-type"
	CodeInvalidEnum        IssueCode = "invalid-enum"
	CodePatternMismatch    IssueCode = "pattern-mismatch"
	CodeLength             IssueCode = "invalid-length"
	CodeMinProperties      IssueCode = "min-properties"
	CodeUnknownProperty    IssueCode = "unknown-property"
	CodeForbiddenProperty  IssueCode = "forbidden-property"
	CodeUnsupportedShape   IssueCode = "unsupported-shape"
	CodeUnknownTopLevelKey IssueCode = "unknown-top-level-key"
	CodeUnknownHost        IssueCode = "unknown-host"
	CodeAnchor             IssueCode = "yaml-anchor"
	CodeAlias              IssueCode = "yaml-alias"
	CodeMergeKey           IssueCode = "yaml-merge-key"
	CodeNonScalarKey       IssueCode = "yaml-non-scalar-key"
)

// Issue is one structural finding. Messages name keys and schema values but never echo a document value.
type Issue struct {
	Code    IssueCode
	Level   Level
	Path    string // dotted path, for example services.api.host
	Pointer string // JSON pointer (RFC 6901) to the same place, for example /services/api/host
	Keyword string // schema keyword that produced it, such as required, enum, type; empty for YAML-level issues
	Schema  string // vendored schema file the keyword comes from; empty for YAML-level issues
	Pos     Pos
	Message string
}

// UndefinedUse is a `uses` entry that names neither a service nor a resource.
type UndefinedUse struct {
	Service string
	Index   int
	Name    string
	Pos     Pos
}

// UndefinedUses returns `uses` entries whose name is not a key of services: or resources:. Entries that contain a
// ${...} reference cannot be judged offline and are skipped.
func (r *YAMLAnalysis) UndefinedUses() []UndefinedUse {
	if r == nil || r.YAML == nil {
		return nil
	}
	known := map[string]bool{}
	for _, s := range r.YAML.Services {
		known[s.Name] = true
	}
	for _, x := range r.Resources {
		known[x.Name] = true
	}
	var out []UndefinedUse
	for _, s := range r.YAML.Services {
		for i, u := range s.Uses {
			if known[u.Name] || strings.Contains(u.Name, "$") {
				continue
			}
			out = append(out, UndefinedUse{Service: s.Name, Index: i, Name: u.Name, Pos: u.Pos})
		}
	}
	return out
}

// UnresolvedRefs returns the references that need a value and for which has(name) is false. has is typically a
// lookup in the selected azd environment, then the process environment (ADR-004 decision 4). References with a
// default, escaped literals and Foundry expressions are never returned.
func (r *YAMLAnalysis) UnresolvedRefs(has func(name string) bool) []Interpolation {
	if r == nil {
		return nil
	}
	var out []Interpolation
	for _, i := range r.Interpolations {
		if i.needsValue() && !has(i.Name) {
			out = append(out, i)
		}
	}
	return out
}

func SortIssues(is []Issue) {
	slices.SortStableFunc(is, func(a, b Issue) int {
		return cmp.Or(
			cmp.Compare(a.Pos.Line, b.Pos.Line),
			cmp.Compare(a.Pos.Column, b.Pos.Column),
			cmp.Compare(a.Code, b.Code),
			cmp.Compare(a.Path, b.Path),
		)
	})
}
