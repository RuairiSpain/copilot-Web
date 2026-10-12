package idn

import (
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// This file holds the lexical helpers of the group. They never evaluate an ARM expression: they only
// read literal arguments of a few well-known functions and report everything else as unknown, so a
// caller can turn it into a skipped or uncertain result (ADR-002, three-state Value).

var (
	guidRe     = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	guidFullRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	providerRe = regexp.MustCompile(`^[A-Za-z0-9]+(\.[A-Za-z0-9]+)+/[A-Za-z0-9]`)
	identRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
)

func isGUID(s string) bool { return guidFullRe.MatchString(s) }

// inner returns the text between the brackets of an ARM expression.
func inner(s string) (string, bool) {
	if !model.IsARMExpression(s) {
		return "", false
	}
	return strings.TrimSpace(s[1 : len(s)-1]), true
}

// splitArgs splits a call's argument text at top-level commas.
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	inq := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inq:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
				} else {
					inq = false
				}
			}
		case c == '\'':
			inq = true
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	last := strings.TrimSpace(s[start:])
	if last == "" && len(out) == 0 {
		return nil
	}
	return append(out, last)
}

// parseCall parses `fn(args)rest` where the call closes before the end or at the end of e.
func parseCall(e string) (fn string, args []string, rest string, ok bool) {
	i := strings.IndexByte(e, '(')
	if i <= 0 || !identRe.MatchString(e[:i]) {
		return "", nil, "", false
	}
	depth := 0
	inq := false
	for j := i; j < len(e); j++ {
		c := e[j]
		switch {
		case inq:
			if c == '\'' {
				if j+1 < len(e) && e[j+1] == '\'' {
					j++
				} else {
					inq = false
				}
			}
		case c == '\'':
			inq = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return e[:i], splitArgs(e[i+1 : j]), strings.TrimSpace(e[j+1:]), true
			}
		}
	}
	return "", nil, "", false
}

// literalArg returns the content of a single-quoted ARM string literal.
func literalArg(a string) (string, bool) {
	a = strings.TrimSpace(a)
	if len(a) < 2 || a[0] != '\'' || a[len(a)-1] != '\'' {
		return "", false
	}
	body := a[1 : len(a)-1]
	body = strings.ReplaceAll(body, "''", "\x00")
	if strings.Contains(body, "'") {
		return "", false
	}
	return strings.ReplaceAll(body, "\x00", "'"), true
}

// argText is the literal content of an argument, or the argument text itself when it is not a literal.
func argText(a string) string {
	if s, ok := literalArg(a); ok {
		return s
	}
	return strings.TrimSpace(a)
}

func texts(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = argText(a)
	}
	return out
}

// ---- resource ID parsing ----

type idKind int

const (
	idUnknown idKind = iota
	idResource
	idSub
	idRG
	idMG
	idTenant
	idSymbolic
)

// idRef is what can be read lexically from a resource ID, a scope or a reference() target.
type idRef struct {
	kind  idKind
	typ   string   // full resource type, for idResource
	names []string // name arguments (literal content or expression text), outermost first
	sym   string   // symbolic name, for idSymbolic
}

func (r idRef) rootType() string {
	parts := strings.Split(r.typ, "/")
	if len(parts) <= 2 {
		return r.typ
	}
	return parts[0] + "/" + parts[1]
}

func (r idRef) isChild() bool { return r.kind == idResource && strings.Count(r.typ, "/") > 1 }

func mkRef(typ string, names []string) idRef {
	switch strings.ToLower(typ) {
	case "microsoft.resources/resourcegroups":
		return idRef{kind: idRG}
	case "microsoft.resources/subscriptions":
		return idRef{kind: idSub}
	}
	return idRef{kind: idResource, typ: typ, names: names}
}

// fromSegments reads provider/type/name/type/name... segments; name segments of the form {k} are looked up in args.
func fromSegments(segs []string, args []string) idRef {
	if len(segs) < 2 || !providerRe.MatchString(segs[0]+"/"+segs[1]) {
		return idRef{}
	}
	typ := segs[0] + "/" + segs[1]
	var names []string
	name := func(s string) string {
		if len(s) > 2 && s[0] == '{' && s[len(s)-1] == '}' {
			k := 0
			for _, c := range s[1 : len(s)-1] {
				if c < '0' || c > '9' {
					return s
				}
				k = k*10 + int(c-'0')
			}
			if k < len(args) {
				return argText(args[k])
			}
		}
		return s
	}
	if len(segs) > 2 {
		names = append(names, name(segs[2]))
	}
	for i := 3; i+1 < len(segs); i += 2 {
		if strings.Contains(segs[i], "{") {
			return idRef{}
		}
		typ += "/" + segs[i]
		names = append(names, name(segs[i+1]))
	}
	return mkRef(typ, names)
}

// parseID reads a resource ID, scope or reference target. It returns idUnknown when the text is not one of
// the forms understood.
func parseID(s string) idRef {
	s = strings.TrimSpace(s)
	if !model.IsARMExpression(s) {
		return parseLiteralID(s)
	}
	e, _ := inner(s)
	return parseIDInner(e)
}

// parseIDInner is parseID for the text of an expression without its brackets, such as a call argument.
func parseIDInner(e string) idRef {
	if lit, ok := literalArg(e); ok {
		return idRef{kind: idSymbolic, sym: lit}
	}
	fn, args, rest, ok := parseCall(e)
	if !ok {
		return idRef{}
	}
	lf := strings.ToLower(fn)
	lrest := strings.ToLower(rest)
	switch {
	case lf == "subscription" && lrest == ".id" && len(args) == 0:
		return idRef{kind: idSub}
	case lf == "resourcegroup" && lrest == ".id" && len(args) == 0:
		return idRef{kind: idRG}
	case lf == "managementgroup" && lrest == ".id" && len(args) == 0:
		return idRef{kind: idMG}
	case lf == "tenant" && len(args) == 0:
		return idRef{kind: idTenant}
	case rest != "":
		return idRef{}
	case lf == "resourceid" || lf == "subscriptionresourceid" || lf == "tenantresourceid" || lf == "extensionresourceid":
		start := 0
		if lf == "extensionresourceid" {
			start = 1
		}
		for i := start; i < len(args); i++ {
			if t, ok := literalArg(args[i]); ok && providerRe.MatchString(t) {
				return mkRef(t, texts(args[i+1:]))
			}
		}
	case lf == "format" && len(args) > 0:
		if f, ok := literalArg(args[0]); ok {
			return fromSegments(strings.Split(f, "/"), args[1:])
		}
	}
	return idRef{}
}

func parseLiteralID(s string) idRef {
	l := strings.ToLower(s)
	if i := strings.LastIndex(l, "/providers/"); i >= 0 {
		tail := s[i+len("/providers/"):]
		if strings.HasPrefix(strings.ToLower(tail), "microsoft.management/managementgroups") {
			return idRef{kind: idMG}
		}
		return fromSegments(strings.Split(tail, "/"), nil)
	}
	parts := strings.Split(strings.Trim(l, "/"), "/")
	switch {
	case len(parts) == 2 && parts[0] == "subscriptions":
		return idRef{kind: idSub}
	case len(parts) == 4 && parts[0] == "subscriptions" && parts[2] == "resourcegroups":
		return idRef{kind: idRG}
	}
	return idRef{}
}

// ---- role definitions ----

// roleGUIDText reads the GUID of a role definition ID from the raw property text. It succeeds only when a GUID is
// a literal in the text and the text names a roleDefinitions resource (or is the GUID itself).
func roleGUIDText(s string) (string, bool, string) {
	s = strings.TrimSpace(s)
	if isGUID(s) {
		return strings.ToLower(s), true, ""
	}
	l := strings.ToLower(s)
	if !strings.Contains(l, "roledefinitions") {
		if model.IsARMExpression(s) {
			return "", false, "arm-expression"
		}
		return "", false, "not-a-role-definition-id"
	}
	all := guidRe.FindAllString(s, -1)
	if len(all) == 0 {
		return "", false, "arm-expression"
	}
	// The role GUID is the last GUID: a literal path may carry a subscription GUID before it.
	return strings.ToLower(all[len(all)-1]), true, ""
}

// ---- resource access ----

// rawAt reads a raw (undecoded) value inside a body.
func rawAt(body map[string]any, path ...string) (any, bool) {
	var cur any = body
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func rawString(r model.Resource, path ...string) (string, bool) {
	v, ok := rawAt(r.Body, path...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func isType(r model.Resource, t string) bool { return strings.EqualFold(r.Type, t) }

// nameKey normalises a lexical name for comparison.
func nameKey(s string) string {
	s = strings.TrimSpace(s)
	if lit, ok := literalArg(s); ok {
		s = lit
	}
	return strings.ToLower(strings.ReplaceAll(s, " ", ""))
}

// nameSegments returns the lexical name segments of a resource name as written: the segments of a literal
// name split at '/', or the arguments of a format()/concat() expression.
func nameSegments(name string) []string {
	e, isExpr := inner(name)
	if !isExpr {
		return strings.Split(name, "/")
	}
	fn, args, rest, ok := parseCall(e)
	if ok && rest == "" {
		switch strings.ToLower(fn) {
		case "format":
			if len(args) > 0 {
				if f, ok := literalArg(args[0]); ok {
					var out []string
					for _, seg := range strings.Split(f, "/") {
						if len(seg) > 2 && seg[0] == '{' {
							k := 0
							for _, c := range seg[1 : len(seg)-1] {
								k = k*10 + int(c-'0')
							}
							if k+1 < len(args) {
								out = append(out, argText(args[k+1]))
								continue
							}
						}
						out = append(out, seg)
					}
					return out
				}
			}
		case "concat":
			var out []string
			for _, a := range args {
				if t, ok := literalArg(a); ok && strings.Contains(t, "/") {
					for _, seg := range strings.Split(t, "/") {
						if seg != "" {
							out = append(out, seg)
						}
					}
					continue
				}
				out = append(out, argText(a))
			}
			return out
		}
	}
	return []string{e}
}

// match returns the template resources a reference can denote. A unique result is certain by type and
// name; several results mean the reference is ambiguous.
func match(t *model.ARMTemplate, ref idRef) []model.Resource {
	if t == nil {
		return nil
	}
	var typed []model.Resource
	switch ref.kind {
	case idSymbolic:
		for _, r := range t.Resources {
			if r.Symbolic != "" && strings.EqualFold(r.Symbolic, ref.sym) {
				return []model.Resource{r}
			}
		}
		for _, r := range t.Resources {
			if nameKey(r.Name) == nameKey(ref.sym) {
				typed = append(typed, r)
			}
		}
		return typed
	case idResource:
		typed = t.ResourcesOfType(ref.typ)
	default:
		return nil
	}
	if len(typed) <= 1 || len(ref.names) == 0 {
		return typed
	}
	want := nameKey(ref.names[len(ref.names)-1])
	var named []model.Resource
	for _, r := range typed {
		segs := nameSegments(r.Name)
		if len(segs) > 0 && nameKey(segs[len(segs)-1]) == want {
			named = append(named, r)
		}
	}
	if len(named) == 0 {
		return typed
	}
	return named
}

func samePointer(a, b model.Resource) bool {
	return a.Meta.Pointer == b.Meta.Pointer && a.Meta.Pointer != "" || (a.Meta.Pointer == "" && b.Meta.Pointer == "" && a.Name == b.Name && a.Type == b.Type)
}

func containsRes(list []model.Resource, r model.Resource) bool {
	for _, x := range list {
		if samePointer(x, r) {
			return true
		}
	}
	return false
}
