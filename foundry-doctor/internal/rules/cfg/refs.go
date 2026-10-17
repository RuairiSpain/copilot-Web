package cfg

import (
	"regexp"
	"strings"
)

// valueShape classifies how a scalar value is built from literal text and
// ${VAR} references.
type valueShape int

const (
	shapeEmpty          valueShape = iota // nothing set
	shapeRef                              // only references, or a reference with an empty or reference default
	shapeLiteral                          // no reference at all
	shapeDefaultLiteral                   // ${VAR:-literal}
	shapeMixed                            // literal text around one or more references
)

var (
	wholeVarWithDefaultRe = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*(?::-(.*))?\}$`)
	anyRefRe              = regexp.MustCompile(`\$\{\{.*?\}\}|\$\{[^}]*\}`)
)

// classifyValue returns the shape of v and, for shapeLiteral and
// shapeDefaultLiteral, the literal text (the default part for the latter).
func classifyValue(v string) (valueShape, string) {
	t := strings.TrimSpace(v)
	switch {
	case t == "":
		return shapeEmpty, ""
	case !strings.Contains(t, "${"):
		return shapeLiteral, t
	}
	if m := wholeVarWithDefaultRe.FindStringSubmatch(t); m != nil && !strings.Contains(anyRefRe.ReplaceAllString(m[1], ""), "}") {
		def := strings.TrimSpace(m[1])
		if def == "" {
			return shapeRef, ""
		}
		if isWholeRef(def) {
			return shapeRef, ""
		}
		return shapeDefaultLiteral, def
	}
	if isWholeRef(t) {
		return shapeRef, ""
	}
	if strings.TrimSpace(anyRefRe.ReplaceAllString(t, "")) == "" {
		return shapeRef, ""
	}
	return shapeMixed, ""
}
