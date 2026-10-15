package armexpr

import (
	"regexp"
	"strings"
)

// Segment is one name segment of a resource name or reference.
type Segment struct {
	// Literal is true when Text is the exact (lower-cased) segment value.
	Literal bool
	// Text is the lower-cased literal value, or the normalised expression text.
	Text string
	// Prefix is, for an expression, text the evaluated value is known to start
	// with (the literal head of a format() string); empty when unknown.
	Prefix string
}

// Ref is a resource reference or resource identity reduced to a type and name
// segments.
type Ref struct {
	// Type is the lower-cased resource type; empty when the text carries none.
	Type string
	// Segments are the name segments in order (one per type level).
	Segments []Segment
	// Opaque is true when the text could not be reduced to name segments
	// (parameters, reference(), module outputs, concat() and so on).
	Opaque bool
}

// Literal reports whether every segment is a literal.
func (r Ref) Literal() bool {
	if r.Opaque || len(r.Segments) == 0 {
		return false
	}
	for _, s := range r.Segments {
		if !s.Literal {
			return false
		}
	}
	return true
}

// Last returns the final name segment.
func (r Ref) Last() (Segment, bool) {
	if len(r.Segments) == 0 {
		return Segment{}, false
	}
	return r.Segments[len(r.Segments)-1], true
}

// Result is the outcome of comparing two references.
type Result int

// Comparison outcomes.
const (
	// Unknown means the references cannot be correlated from the template.
	Unknown Result = iota
	// Same means the references provably name the same resource.
	Same
	// Different means the references provably name different resources.
	Different
)

var typeRe = regexp.MustCompile(`(?i)^[a-z0-9]+(\.[a-z0-9]+)+/[a-z0-9._/-]+$`)

// ParseName reduces a resource "name" property (literal, "parent/child",
// format('{0}/{1}', ...), variables(...), ...) to name segments. typ is the
// resource type to record on the result and may be empty.
func ParseName(typ, name string) Ref {
	ref := Ref{Type: strings.ToLower(strings.TrimSpace(typ))}
	name = strings.TrimSpace(name)
	if name == "" {
		ref.Opaque = true
		return ref
	}
	if !IsExpression(name) {
		for _, p := range strings.Split(name, "/") {
			ref.Segments = append(ref.Segments, Segment{Literal: true, Text: strings.ToLower(strings.TrimSpace(p))})
		}
		return ref
	}
	ref.Segments = segmentsOf(name)
	if len(ref.Segments) == 0 {
		ref.Opaque = true
	}
	return ref
}

// segmentsOf expands one expression into name segments.
func segmentsOf(expr string) []Segment {
	if v, ok := Fold(expr); ok {
		var out []Segment
		for _, p := range strings.Split(v, "/") {
			out = append(out, Segment{Literal: true, Text: strings.ToLower(p)})
		}
		return out
	}
	c, ok := ParseCall(expr)
	if ok && c.Fn == "format" && c.Suffix == "" && len(c.Args) >= 1 {
		if f, ok := Unquote(c.Args[0]); ok && strings.Contains(f, "/") {
			return formatSegments(f, c.Args[1:])
		}
	}
	return []Segment{exprSegment(expr)}
}

// exprSegment builds the segment for a name that is one expression value.
func exprSegment(expr string) Segment {
	if v, ok := Unquote(strings.TrimSpace(expr)); ok {
		return Segment{Literal: true, Text: strings.ToLower(v)}
	}
	return Segment{Text: Normalize(expr), Prefix: prefixOf(expr)}
}

var placeholderRe = regexp.MustCompile(`\{(\d+)\}`)

// formatSegments splits a format() string at "/" and substitutes arguments.
func formatSegments(f string, args []string) []Segment {
	var out []Segment
	for _, piece := range strings.Split(f, "/") {
		idx := placeholderRe.FindAllStringSubmatch(piece, -1)
		if len(idx) == 0 {
			out = append(out, Segment{Literal: true, Text: strings.ToLower(piece)})
			continue
		}
		if m := placeholderRe.FindString(piece); m == piece && len(idx) == 1 {
			n := atoi(idx[0][1])
			if n < len(args) {
				out = append(out, exprSegment(args[n]))
				continue
			}
		}
		// Mixed piece: literal text and arguments. Fold when all literal.
		lit := piece
		allLit := true
		var used []string
		for _, m := range idx {
			n := atoi(m[1])
			if n >= len(args) {
				allLit = false
				break
			}
			if v, ok := Unquote(args[n]); ok {
				lit = strings.ReplaceAll(lit, m[0], v)
			} else {
				allLit = false
				used = append(used, Normalize(args[n]))
			}
		}
		if allLit {
			out = append(out, Segment{Literal: true, Text: strings.ToLower(lit)})
			continue
		}
		head := piece
		if i := strings.Index(head, "{"); i >= 0 {
			head = head[:i]
		}
		out = append(out, Segment{Text: "piece:" + strings.ToLower(piece) + "|" + strings.Join(used, "|"), Prefix: strings.ToLower(head)})
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return 1 << 20
		}
	}
	return n
}

// prefixOf returns the literal text an expression's value is known to start
// with: the head of a format() string, following a leading {0} argument.
func prefixOf(expr string) string {
	c, ok := ParseCall(expr)
	if !ok || c.Suffix != "" {
		return ""
	}
	switch c.Fn {
	case "format":
		if len(c.Args) == 0 {
			return ""
		}
		f, ok := Unquote(c.Args[0])
		if !ok {
			return ""
		}
		if i := strings.Index(f, "{"); i >= 0 {
			head := strings.ToLower(f[:i])
			if i == 0 && strings.HasPrefix(f, "{0}") && len(c.Args) > 1 {
				if v, ok := Unquote(c.Args[1]); ok {
					return strings.ToLower(v)
				}
				return prefixOf(c.Args[1])
			}
			return head
		}
		return strings.ToLower(f)
	case "tolower":
		if len(c.Args) == 1 {
			return prefixOf(c.Args[0])
		}
	}
	return ""
}

// resourceIDFns are the ARM functions whose trailing arguments are a resource
// type followed by name segments.
var resourceIDFns = map[string]bool{
	"resourceid": true, "subscriptionresourceid": true, "extensionresourceid": true,
	"tenantresourceid": true, "managementgroupresourceid": true,
}

// ParseRef reduces a reference to a resource to type and name segments. It
// understands resourceId-style calls (also behind reference(...) and a property
// suffix), full literal resource IDs, and bare literal names. Anything else is
// Opaque.
func ParseRef(s string) Ref {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ref{Opaque: true}
	}
	if !IsExpression(s) {
		if strings.Contains(strings.ToLower(s), "/providers/") {
			return parseLiteralID(s)
		}
		if strings.Contains(s, "://") || strings.ContainsAny(s, " \t") {
			return Ref{Opaque: true}
		}
		return ParseName("", s)
	}
	c, ok := ParseCall(s)
	if !ok {
		return Ref{Opaque: true}
	}
	if c.Fn == "reference" && len(c.Args) >= 1 {
		return ParseRef(c.Args[0])
	}
	if !resourceIDFns[c.Fn] {
		return Ref{Opaque: true}
	}
	typeIdx := -1
	for i, a := range c.Args {
		if v, ok := Unquote(a); ok && typeRe.MatchString(v) {
			typeIdx = i
			break
		}
	}
	if typeIdx < 0 || typeIdx == len(c.Args)-1 {
		return Ref{Opaque: true}
	}
	typ, _ := Unquote(c.Args[typeIdx])
	ref := Ref{Type: strings.ToLower(typ)}
	for _, a := range c.Args[typeIdx+1:] {
		ref.Segments = append(ref.Segments, segmentsOfArg(a)...)
	}
	return ref
}

// segmentsOfArg handles one name argument of a resourceId call: a quoted
// literal, or a function call such as format('ai-{0}', variables('t')).
func segmentsOfArg(a string) []Segment {
	a = strings.TrimSpace(a)
	if v, ok := Unquote(a); ok {
		var out []Segment
		for _, p := range strings.Split(v, "/") {
			out = append(out, Segment{Literal: true, Text: strings.ToLower(p)})
		}
		return out
	}
	return segmentsOf("[" + a + "]")
}

// ParseArg reduces one call argument (a quoted literal, or a call such as
// format('ai-{0}', variables('t'))) to a reference of the given type.
func ParseArg(typ, arg string) Ref {
	ref := Ref{Type: strings.ToLower(strings.TrimSpace(typ)), Segments: segmentsOfArg(arg)}
	if len(ref.Segments) == 0 {
		ref.Opaque = true
	}
	return ref
}

func parseLiteralID(id string) Ref {
	l := strings.ToLower(strings.Trim(id, "/"))
	i := strings.LastIndex(l, "providers/")
	if i < 0 {
		return Ref{Opaque: true}
	}
	toks := strings.Split(l[i+len("providers/"):], "/")
	if len(toks) < 3 || (len(toks)-1)%2 != 0 {
		return Ref{Opaque: true}
	}
	typ := toks[0]
	ref := Ref{}
	for j := 1; j < len(toks); j += 2 {
		typ += "/" + toks[j]
		ref.Segments = append(ref.Segments, Segment{Literal: true, Text: toks[j+1]})
	}
	ref.Type = typ
	return ref
}

// Compare correlates reference a with b. See the package comment for the
// meaning of the three results.
func Compare(a, b Ref) Result {
	if a.Opaque || b.Opaque || len(a.Segments) == 0 || len(b.Segments) == 0 {
		return Unknown
	}
	if a.Type != "" && b.Type != "" && a.Type != b.Type {
		return Different
	}
	if len(a.Segments) != len(b.Segments) {
		if a.Literal() && b.Literal() {
			return Different
		}
		return Unknown
	}
	res := Same
	for i := range a.Segments {
		switch compareSegment(a.Segments[i], b.Segments[i]) {
		case Different:
			return Different
		case Unknown:
			res = Unknown
		}
	}
	return res
}

func compareSegment(a, b Segment) Result {
	switch {
	case a.Literal && b.Literal:
		if a.Text == b.Text {
			return Same
		}
		return Different
	case a.Literal != b.Literal:
		lit, ex := a, b
		if b.Literal {
			lit, ex = b, a
		}
		if ex.Prefix != "" && !strings.HasPrefix(lit.Text, ex.Prefix) {
			return Different
		}
		return Unknown
	}
	if a.Text == b.Text {
		return Same
	}
	if a.Prefix != "" && b.Prefix != "" && !strings.HasPrefix(a.Prefix, b.Prefix) && !strings.HasPrefix(b.Prefix, a.Prefix) {
		return Different
	}
	return Unknown
}

// Presence says whether a resource is deployed.
type Presence int

// Presence values.
const (
	// Deployed means the resource is deployed whenever the template is.
	Deployed Presence = iota
	// NotDeployed means a literal false condition: the resource never exists.
	NotDeployed
	// MaybeDeployed means the condition or loop depends on a parameter or
	// expression; the template alone cannot tell.
	MaybeDeployed
)

// PresenceOf classifies a resource from its Conditional, Condition and Looped
// fields. A literal true condition counts as deployed.
func PresenceOf(conditional bool, condition string, looped bool) Presence {
	if looped {
		return MaybeDeployed
	}
	if !conditional {
		return Deployed
	}
	switch strings.ToLower(strings.TrimSpace(condition)) {
	case "false", "[false()]":
		return NotDeployed
	case "true", "[true()]":
		return Deployed
	}
	return MaybeDeployed
}

var guidRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// RoleDefinitionGUID extracts the role definition GUID from a roleDefinitionId
// value: a literal resource ID or GUID, or a compiled
// subscriptionResourceId/extensionResourceId/resourceId/format() expression
// whose last argument is a literal GUID. ok is false when it is not literal.
func RoleDefinitionGUID(def string) (string, bool) {
	def = strings.TrimSpace(def)
	if def == "" {
		return "", false
	}
	if !IsExpression(def) {
		i := strings.LastIndex(def, "/")
		g := def[i+1:]
		return strings.ToLower(g), guidRe.MatchString(g)
	}
	c, ok := ParseCall(def)
	if !ok || c.Suffix != "" || len(c.Args) == 0 {
		return "", false
	}
	switch {
	case resourceIDFns[c.Fn]:
	case c.Fn == "format":
		f, ok := Unquote(c.Args[0])
		if !ok || !strings.Contains(strings.ToLower(f), "roledefinitions") || !strings.HasSuffix(strings.TrimSpace(f), "{"+itoa(len(c.Args)-2)+"}") {
			return "", false
		}
	default:
		return "", false
	}
	v, ok := Unquote(c.Args[len(c.Args)-1])
	if !ok || !guidRe.MatchString(v) {
		return "", false
	}
	return strings.ToLower(v), true
}

// ScopeLevel classifies a role assignment scope.
type ScopeLevel int

// Scope levels.
const (
	ScopeUnknown ScopeLevel = iota
	// ScopeBroad is a subscription, management group or resource group.
	ScopeBroad
	// ScopeResource is below a resource group.
	ScopeResource
)

// ClassifyScope classifies a literal scope ID or a compiled scope expression:
// resourceGroup().id, subscription().id, managementGroup().id, a format()
// string with literal path text, or a resourceId-style call (resource scope).
// An empty scope is the deployment scope, which is a resource group,
// subscription or management group and so broad.
func ClassifyScope(s string) ScopeLevel {
	s = strings.TrimSpace(s)
	if s == "" {
		return ScopeBroad
	}
	if !IsExpression(s) {
		return classifyPath(s)
	}
	n := Normalize(s)
	switch n {
	case "resourcegroup().id", "subscription().id", "managementgroup().id":
		return ScopeBroad
	}
	c, ok := ParseCall(s)
	if !ok {
		return ScopeUnknown
	}
	switch {
	case resourceIDFns[c.Fn]:
		if c.Suffix == "" {
			if r := ParseRef(s); !r.Opaque && r.Type != "" {
				return ScopeResource
			}
		}
	case c.Fn == "format" && c.Suffix == "" && len(c.Args) > 0:
		if f, ok := Unquote(c.Args[0]); ok {
			return classifyPath(placeholderRe.ReplaceAllString(f, "x"))
		}
	}
	return ScopeUnknown
}

func classifyPath(s string) ScopeLevel {
	parts := strings.Split(strings.ToLower(strings.Trim(s, "/")), "/")
	switch {
	case len(parts) == 2 && parts[0] == "subscriptions":
		return ScopeBroad
	case len(parts) == 4 && parts[0] == "subscriptions" && parts[2] == "resourcegroups":
		return ScopeBroad
	case len(parts) == 4 && parts[0] == "providers" && parts[1] == "microsoft.management" && parts[2] == "managementgroups":
		return ScopeBroad
	case len(parts) > 4 && parts[0] == "subscriptions" && parts[2] == "resourcegroups":
		return ScopeResource
	}
	return ScopeUnknown
}

// ReadsClientID reports whether an expression reads a client (application) ID:
// a property access .clientId, .appId or .applicationId, or a parameters() /
// variables() name whose camel-case words include client+id, app+id or
// application+id as whole words ("uamiClientId", "appId"). A name such as
// "appIdentityPrincipalId" does not match.
func ReadsClientID(expr string) bool {
	if !IsExpression(expr) {
		return false
	}
	rest := stripQuoted(expr)
	for _, p := range []string{".clientid", ".appid", ".applicationid"} {
		i := 0
		l := strings.ToLower(rest)
		for {
			j := strings.Index(l[i:], p)
			if j < 0 {
				break
			}
			end := i + j + len(p)
			if end >= len(l) || !isIdentChar(l[end]) {
				return true
			}
			i = end
		}
	}
	for _, name := range identifierArgs(expr) {
		if nameIsClientID(name) {
			return true
		}
	}
	return false
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// stripQuoted removes the contents of string literals.
func stripQuoted(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			in = !in
			b.WriteByte(' ')
			continue
		}
		if !in {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

var identCallRe = regexp.MustCompile(`(?i)(?:parameters|variables)\(\s*'([^']*)'\s*\)`)

func identifierArgs(expr string) []string {
	var out []string
	for _, m := range identCallRe.FindAllStringSubmatch(expr, -1) {
		out = append(out, m[1])
	}
	return out
}

// nameIsClientID splits a camelCase / snake_case identifier into words and
// looks for the whole-word pairs client+id, app+id, application+id or the
// single words clientid, appid, applicationid.
func nameIsClientID(name string) bool {
	words := splitWords(name)
	for i, w := range words {
		switch w {
		case "clientid", "appid", "applicationid":
			return true
		case "client", "app", "application":
			if i+1 < len(words) && words[i+1] == "id" {
				return true
			}
		}
	}
	return false
}

func splitWords(s string) []string {
	var words []string
	var cur []byte
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '-' || c == '.' || c == ' ':
			flush()
		case c >= 'A' && c <= 'Z':
			// Start a new word at a lower-to-upper boundary, or at the last
			// capital of an acronym run followed by a lowercase letter.
			if len(cur) > 0 {
				prev := cur[len(cur)-1]
				nextLower := i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z'
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') || (prev >= 'A' && prev <= 'Z' && nextLower) {
					flush()
				}
			}
			cur = append(cur, c)
		default:
			cur = append(cur, c)
		}
	}
	flush()
	return words
}
