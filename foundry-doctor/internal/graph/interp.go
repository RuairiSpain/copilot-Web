package graph

import (
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// Form is the syntactic form of one interpolation.
type Form string

// Forms accepted by the scanner (ADR-004, FND-CFG-006).
const (
	FormBraced     Form = "${VAR}"
	FormDefault    Form = "${VAR:-x}"
	FormExpression Form = "${{expr}}"
	FormMalformed  Form = "malformed"
)

// Interpolation is one `${...}` occurrence in a scalar. Expression text is never kept.
type Interpolation struct {
	Name       string // the variable name; empty for FormExpression and FormMalformed
	Form       Form
	HasDefault bool
	Expression bool // a ${{ ... }} Foundry expression, not an azd variable
	Offset     int  // byte offset of the `$` in the scalar
}

func isNameStart(c byte) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isNameChar(c byte) bool  { return isNameStart(c) || (c >= '0' && c <= '9') }

// ScanInterpolations finds ${VAR}, ${VAR:-x} and ${{ expr }} in s. `$$` is an escape: `$${VAR}` and
// `$${{x}}` yield no interpolation. A bare $VAR is not an interpolation. A malformed `${...}` is returned
// with FormMalformed.
func ScanInterpolations(s string) []Interpolation {
	var out []Interpolation
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 >= len(s) {
			continue
		}
		switch s[i+1] {
		case '$':
			i++ // escaped dollar; the following text is literal
		case '{':
			if i+2 < len(s) && s[i+2] == '{' {
				end := strings.Index(s[i+3:], "}}")
				if end < 0 {
					out = append(out, Interpolation{Form: FormMalformed, Offset: i})
					return out
				}
				out = append(out, Interpolation{Form: FormExpression, Expression: true, Offset: i})
				i += 3 + end + 1
				continue
			}
			it, next := scanBraced(s, i)
			out = append(out, it)
			i = next
		}
	}
	return out
}

// scanBraced scans "${" at i and returns the interpolation and the index of its last byte.
func scanBraced(s string, i int) (Interpolation, int) {
	bad := Interpolation{Form: FormMalformed, Offset: i}
	j := i + 2
	if j >= len(s) || !isNameStart(s[j]) {
		return bad, skipTo(s, i)
	}
	k := j
	for k < len(s) && isNameChar(s[k]) {
		k++
	}
	name := s[j:k]
	if k < len(s) && s[k] == '}' {
		return Interpolation{Name: name, Form: FormBraced, Offset: i}, k
	}
	if strings.HasPrefix(s[k:], ":-") {
		end := strings.IndexByte(s[k+2:], '}')
		if end < 0 {
			return bad, len(s)
		}
		def := s[k+2 : k+2+end]
		if strings.Contains(def, "${") { // a reference nested in a default is refused by the extension
			return bad, k + 2 + end
		}
		return Interpolation{Name: name, Form: FormDefault, HasDefault: true, Offset: i}, k + 2 + end
	}
	return bad, skipTo(s, i)
}

// skipTo returns the index of the closing brace after i, or the end of s.
func skipTo(s string, i int) int {
	if e := strings.IndexByte(s[i:], '}'); e >= 0 {
		return i + e
	}
	return len(s)
}

// EnvRef is one interpolation found in azure.yaml.
type EnvRef struct {
	Owner      string // node ID of the service, or of the azure.yaml node for project-level values
	Name       string
	Form       Form
	HasDefault bool
	Expression bool
	Pos        model.Pos
}

func sortEnvRefs(refs []EnvRef) {
	sort.Slice(refs, func(a, b int) bool {
		x, y := refs[a], refs[b]
		if x.Owner != y.Owner {
			return x.Owner < y.Owner
		}
		if x.Pos != y.Pos {
			if x.Pos.Line != y.Pos.Line {
				return x.Pos.Line < y.Pos.Line
			}
			return x.Pos.Column < y.Pos.Column
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Form < y.Form
	})
}
