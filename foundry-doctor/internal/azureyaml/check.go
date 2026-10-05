package azureyaml

import (
	"fmt"
	pathpkg "path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// maxSchemaDepth bounds how deeply allOf and $ref may nest for one instance node.
const maxSchemaDepth = 16

// checker applies the subset of JSON-schema keywords that CFG-001 states it checks: type, enum, const, pattern,
// minLength, maxLength, minProperties, required, properties, additionalProperties (false or a schema), items,
// $ref, allOf, if/then/else (properties, required, const, enum, not) and `then: {not: {}}` / not.required.
// Every other keyword (oneOf, anyOf except for hooks, format, ...) is ignored, so the checks never claim more
// than the schema text supports. It is deliberately not a general engine (ADR-011).
type checker struct {
	res   *Result
	s     *schemaSet
	re    map[string]*regexp.Regexp
	seenI map[string]bool
}

// ctx is the schema location an instance node is checked against.
type ctx struct {
	doc  map[string]any // the schema file's top-level object, for #/definitions refs
	file string         // vendored path of that file
}

func checkStructure(res *Result, top *yaml.Node) {
	s, err := loadSchemas()
	if err != nil {
		res.Issues = append(res.Issues, Issue{Code: CodeUnsupportedShape, Level: LevelError, Message: "structural checks unavailable: " + err.Error()})
		return
	}
	c := &checker{res: res, s: s, re: map[string]*regexp.Regexp{}, seenI: map[string]bool{}}
	for _, is := range res.Issues {
		c.seenI[issueKey(is)] = true
	}
	c.node(top, s.root, ctx{doc: s.root, file: rootFile}, path{}, 0)

	props, _ := s.root["properties"].(map[string]any)
	seen := map[string]bool{}
	for i := 0; i+1 < len(top.Content); i += 2 {
		k := top.Content[i]
		if k.Kind != yaml.ScalarNode || seen[k.Value] {
			continue
		}
		seen[k.Value] = true
		if _, ok := props[k.Value]; !ok {
			res.UnknownTopLevel = append(res.UnknownTopLevel, model.Ref{Name: k.Value, Pos: posOf(k)})
			c.add(CodeUnknownTopLevelKey, LevelInfo, path{}.key(k.Value), posOf(k), "", "",
				fmt.Sprintf("top-level key %q is not declared by the azd schema (the schema allows additional properties)", k.Value))
		}
	}
	c.hosts(top)
}

func issueKey(i Issue) string {
	return string(i.Code) + "|" + i.Path + "|" + i.Keyword + "|" + i.Message
}

func (c *checker) add(code IssueCode, lvl Level, p path, pos model.Pos, keyword, file, msg string) {
	is := Issue{Code: code, Level: lvl, Path: p.Dotted(), Pointer: p.Pointer(), Keyword: keyword, Schema: file, Pos: pos, Message: msg}
	k := issueKey(is)
	if c.seenI[k] {
		return
	}
	c.seenI[k] = true
	c.res.Issues = append(c.res.Issues, is)
}

// hosts reports services whose host the schema does not list. The schema lists hosts as examples, so this is info.
func (c *checker) hosts(top *yaml.Node) {
	svcs := mapGet(top, "services")
	if svcs == nil || svcs.Kind != yaml.MappingNode {
		return
	}
	for _, pr := range pairs(svcs) {
		if pr.v.Kind != yaml.MappingNode {
			continue
		}
		h := mapGet(pr.v, "host")
		if h == nil || h.Kind != yaml.ScalarNode || h.Value == "" || strings.Contains(h.Value, "$") || h.ShortTag() == "!!null" {
			continue
		}
		if c.s.knownHosts[h.Value] {
			continue
		}
		c.add(CodeUnknownHost, LevelInfo, path{{s: "services"}}.key(pr.k.Value).key("host"), posOf(h), "examples", rootFile,
			fmt.Sprintf("host %q is not in the azd schema's list of known hosts; rules for this service are skipped", h.Value))
	}
}

type pair struct{ k, v *yaml.Node }

// pairs returns the key/value pairs of a mapping, first occurrence of each key only.
func pairs(m *yaml.Node) []pair {
	var out []pair
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Kind == yaml.ScalarNode {
			if seen[k.Value] {
				continue
			}
			seen[k.Value] = true
		}
		out = append(out, pair{k, m.Content[i+1]})
	}
	return out
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Kind == yaml.ScalarNode && m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// resolve follows a $ref to a schema in the same file (#/definitions/x) or another vendored file.
func (c *checker) resolve(sch map[string]any, cx ctx) (map[string]any, ctx) {
	for range 8 {
		ref, ok := sch["$ref"].(string)
		if !ok {
			return sch, cx
		}
		switch {
		case strings.HasPrefix(ref, "#/"):
			t, _ := dig(cx.doc, strings.Split(strings.TrimPrefix(ref, "#/"), "/")...).(map[string]any)
			if t == nil {
				return map[string]any{}, cx
			}
			sch = t
		case strings.HasSuffix(ref, ".json"):
			f := pathpkg.Join(pathpkg.Dir(cx.file), ref)
			d := c.s.files[f]
			if d == nil {
				return map[string]any{}, cx
			}
			sch, cx = d, ctx{doc: d, file: f}
		default:
			return map[string]any{}, cx
		}
	}
	return sch, cx
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" }

func interpolated(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "${")
}

func (c *checker) node(n *yaml.Node, sch map[string]any, cx ctx, p path, sd int) {
	if sd > maxSchemaDepth || n.Kind == yaml.AliasNode {
		return
	}
	sch, cx = c.resolve(sch, cx)

	if alts, ok := sch["anyOf"].([]any); ok && sch["type"] == nil {
		c.anyOfByKind(n, alts, cx, p, sd)
		return
	}
	if ts := typesOf(sch["type"]); len(ts) > 0 && !typeOK(n, ts) {
		c.add(CodeInvalidType, LevelError, p, posOf(n), "type", cx.file,
			fmt.Sprintf("expected %s, found %s", strings.Join(ts, " or "), kindName(n)))
		return
	}
	if isNull(n) {
		if slices.Contains(typesOf(sch["type"]), "object") {
			for _, r := range stringList(sch["required"]) {
				c.add(CodeMissingRequired, LevelError, p, posOf(n), "required", cx.file, fmt.Sprintf("required property %q is missing", r))
			}
		}
		if mp, ok := numberOf(sch["minProperties"]); ok && mp > 0 {
			c.add(CodeMinProperties, LevelError, p, posOf(n), "minProperties", cx.file,
				fmt.Sprintf("must have at least %d entr%s", mp, plural(mp, "y", "ies")))
		}
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		c.scalar(n, sch, cx, p)
	case yaml.MappingNode:
		c.mapping(n, sch, cx, p, sd)
	case yaml.SequenceNode:
		if items, ok := sch["items"].(map[string]any); ok {
			for i, it := range n.Content {
				c.node(it, items, cx, p.index(i), sd+1)
			}
		}
	}
}

// anyOfByKind handles the one anyOf the root schema uses for hooks: a single hook object or a list of them.
func (c *checker) anyOfByKind(n *yaml.Node, alts []any, cx ctx, p path, sd int) {
	if isNull(n) {
		return
	}
	want := "object"
	if n.Kind == yaml.SequenceNode {
		want = "array"
	}
	for _, a := range alts {
		am, _ := a.(map[string]any)
		r, rcx := c.resolve(am, cx)
		t := typesOf(r["type"])
		if len(t) == 0 {
			t = typesOf(am["type"])
		}
		if slices.Contains(t, want) || (want == "object" && len(t) == 0) {
			c.node(n, am, cx, p, sd+1)
			_ = rcx
			return
		}
	}
	c.add(CodeInvalidType, LevelError, p, posOf(n), "anyOf", cx.file, "expected a mapping or a list of mappings, found "+kindName(n))
}

func (c *checker) scalar(n *yaml.Node, sch map[string]any, cx ctx, p path) {
	if interpolated(n) {
		return // the value is known only after expansion
	}
	if cv, ok := sch["const"]; ok {
		if fmt.Sprint(cv) != n.Value {
			c.add(CodeInvalidEnum, LevelError, p, posOf(n), "const", cx.file, fmt.Sprintf("must be %q", fmt.Sprint(cv)))
		}
	}
	if en, ok := sch["enum"].([]any); ok && len(en) > 0 {
		var vals []string
		match := false
		for _, e := range en {
			s := fmt.Sprint(e)
			vals = append(vals, s)
			if s == n.Value {
				match = true
			}
		}
		if !match {
			c.add(CodeInvalidEnum, LevelError, p, posOf(n), "enum", cx.file, "must be one of: "+strings.Join(vals, ", "))
		}
	}
	rc := utf8.RuneCountInString(n.Value)
	if mn, ok := numberOf(sch["minLength"]); ok && rc < mn {
		c.add(CodeLength, LevelError, p, posOf(n), "minLength", cx.file, fmt.Sprintf("must be at least %d characters", mn))
	}
	if mx, ok := numberOf(sch["maxLength"]); ok && rc > mx {
		c.add(CodeLength, LevelError, p, posOf(n), "maxLength", cx.file, fmt.Sprintf("must be at most %d characters", mx))
	}
	if pat, ok := sch["pattern"].(string); ok {
		re, cached := c.re[pat]
		if !cached {
			re, _ = regexp.Compile(pat) // a pattern RE2 cannot compile is skipped, not guessed at
			c.re[pat] = re
		}
		if re != nil && !re.MatchString(n.Value) {
			c.add(CodePatternMismatch, LevelError, p, posOf(n), "pattern", cx.file, "does not match the required pattern "+pat)
		}
	}
}

func (c *checker) mapping(n *yaml.Node, sch map[string]any, cx ctx, p path, sd int) {
	ps := pairs(n)
	has := func(k string) *pair {
		for i := range ps {
			if ps[i].k.Value == k {
				return &ps[i]
			}
		}
		return nil
	}
	merged := hasMerge(n) // a merge key adds keys the reader does not expand: absence cannot be asserted
	for _, r := range stringList(sch["required"]) {
		if e := has(r); e == nil {
			if merged {
				continue
			}
			c.add(CodeMissingRequired, LevelError, p, posOf(n), "required", cx.file, fmt.Sprintf("required property %q is missing", r))
		} else if isNull(e.v) || (e.v.Kind == yaml.ScalarNode && strings.TrimSpace(e.v.Value) == "") {
			c.add(CodeMissingRequired, LevelError, p.key(r), posOf(e.v), "required", cx.file, fmt.Sprintf("required property %q is empty", r))
		}
	}
	if mp, ok := numberOf(sch["minProperties"]); ok && len(ps) < mp && !merged {
		c.add(CodeMinProperties, LevelError, p, posOf(n), "minProperties", cx.file, fmt.Sprintf("must have at least %d entr%s", mp, plural(mp, "y", "ies")))
	}
	props, _ := sch["properties"].(map[string]any)
	addl := sch["additionalProperties"]
	for _, e := range ps {
		if e.k.Kind != yaml.ScalarNode {
			continue
		}
		cp := p.key(e.k.Value)
		if sub, ok := props[e.k.Value].(map[string]any); ok {
			c.node(e.v, sub, cx, cp, sd+1)
			continue
		}
		switch a := addl.(type) {
		case bool:
			if !a && !merged {
				c.add(CodeUnknownProperty, LevelError, cp, posOf(e.k), "additionalProperties", cx.file,
					fmt.Sprintf("property %q is not allowed here", e.k.Value))
			}
		case map[string]any:
			c.node(e.v, a, cx, cp, sd+1)
		}
	}
	if allOf, ok := sch["allOf"].([]any); ok {
		for _, entry := range allOf {
			em, _ := entry.(map[string]any)
			c.conditional(n, em, cx, p, sd+1)
		}
	}
}

// conditional applies one allOf entry: a plain sub-schema, or if/then/else.
func (c *checker) conditional(n *yaml.Node, entry map[string]any, cx ctx, p path, sd int) {
	if sd > maxSchemaDepth {
		return
	}
	ifc, hasIf := entry["if"].(map[string]any)
	if !hasIf {
		if _, isRef := entry["$ref"]; isRef || entry["properties"] != nil || entry["required"] != nil {
			c.node(n, entry, cx, p, sd)
		}
		return
	}
	match, ok := c.matches(n, ifc)
	if !ok {
		return // the condition uses a keyword this checker does not model
	}
	branch := "then"
	if !match {
		branch = "else"
	}
	br, _ := entry[branch].(map[string]any)
	if br == nil {
		return
	}
	c.apply(n, br, cx, p, sd, ifc)
}

// apply evaluates the restricted branch forms.
func (c *checker) apply(n *yaml.Node, br map[string]any, cx ctx, p path, sd int, cond map[string]any) {
	if not, ok := br["not"].(map[string]any); ok {
		if len(not) == 0 && len(br) == 1 {
			pp, pos, name := p, posOf(n), ""
			for _, k := range condKeys(cond) {
				if e := mapGetPair(n, k); e != nil {
					pp, pos, name = p.key(k), posOf(e.k), k
					break
				}
			}
			msg := "this combination of properties is not supported by the azd schema"
			if name != "" {
				msg = fmt.Sprintf("%q in this configuration is not supported by the azd schema", name)
			}
			c.add(CodeUnsupportedShape, LevelError, pp, pos, "not", cx.file, msg)
		} else {
			for _, name := range notRequired(not) {
				if e := mapGetPair(n, name); e != nil {
					c.add(CodeForbiddenProperty, LevelError, p.key(name), posOf(e.k), "not", cx.file,
						fmt.Sprintf("property %q is not allowed with this configuration", name))
				}
			}
		}
	}
	props, _ := br["properties"].(map[string]any)
	for _, k := range sortedKeys(props) {
		e := mapGetPair(n, k)
		if e == nil {
			continue
		}
		switch sub := props[k].(type) {
		case bool:
			if !sub {
				c.add(CodeForbiddenProperty, LevelError, p.key(k), posOf(e.k), "properties", cx.file,
					fmt.Sprintf("property %q is not allowed with this configuration", k))
			}
		case map[string]any:
			c.node(e.v, sub, cx, p.key(k), sd+1)
		}
	}
	for _, r := range stringList(br["required"]) {
		e := mapGetPair(n, r)
		switch {
		case e == nil:
			if !hasMerge(n) {
				c.add(CodeMissingRequired, LevelError, p, posOf(n), "required", cx.file, fmt.Sprintf("required property %q is missing", r))
			}
		case isNull(e.v) || (e.v.Kind == yaml.ScalarNode && strings.TrimSpace(e.v.Value) == ""):
			c.add(CodeMissingRequired, LevelError, p.key(r), posOf(e.v), "required", cx.file, fmt.Sprintf("required property %q is empty", r))
		}
	}
	if allOf, ok := br["allOf"].([]any); ok {
		for _, entry := range allOf {
			em, _ := entry.(map[string]any)
			c.conditional(n, em, cx, p, sd+1)
		}
	}
}

// hasMerge reports whether mapping n has a YAML merge key (<<).
func hasMerge(n *yaml.Node) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Kind == yaml.ScalarNode && n.Content[i].ShortTag() == "!!merge" {
			return true
		}
	}
	return false
}

func mapGetPair(n *yaml.Node, key string) *pair {
	for _, e := range pairs(n) {
		if e.k.Kind == yaml.ScalarNode && e.k.Value == key {
			return &e
		}
	}
	return nil
}

// notRequired extracts property names from {required:[..]} or {anyOf:[{required:[..]},..]}.
func notRequired(not map[string]any) []string {
	out := stringList(not["required"])
	if alts, ok := not["anyOf"].([]any); ok {
		for _, a := range alts {
			if am, ok := a.(map[string]any); ok {
				out = append(out, stringList(am["required"])...)
			}
		}
	}
	return out
}

// condKeys lists property names an if-condition mentions, for locating the offending key.
func condKeys(cond map[string]any) []string {
	out := stringList(cond["required"])
	props, _ := cond["properties"].(map[string]any)
	return append(out, sortedKeys(props)...)
}

// matches evaluates an if-condition. ok is false when it uses a keyword outside the modelled set. A property that is
// absent does not match (the schema's vacuous truth would apply every host's rules to a service with no host).
func (c *checker) matches(n *yaml.Node, cond map[string]any) (match, ok bool) {
	for k := range cond {
		switch k {
		case "properties", "required", "const", "enum", "not", "$comment", "comment":
		default:
			return false, false
		}
	}
	if cv, has := cond["const"]; has {
		if n.Kind != yaml.ScalarNode || fmt.Sprint(cv) != n.Value {
			return false, true
		}
	}
	if en, has := cond["enum"].([]any); has {
		found := false
		for _, e := range en {
			if n.Kind == yaml.ScalarNode && fmt.Sprint(e) == n.Value {
				found = true
			}
		}
		if !found {
			return false, true
		}
	}
	if req := stringList(cond["required"]); len(req) > 0 {
		if n.Kind != yaml.MappingNode {
			return false, true
		}
		for _, r := range req {
			if mapGetPair(n, r) == nil {
				return false, true
			}
		}
	}
	if props, has := cond["properties"].(map[string]any); has {
		if n.Kind != yaml.MappingNode {
			return false, true
		}
		for _, k := range sortedKeys(props) {
			sub, _ := props[k].(map[string]any)
			e := mapGetPair(n, k)
			if e == nil {
				return false, true
			}
			m, sok := c.matches(e.v, sub)
			if !sok {
				return false, false
			}
			if !m {
				return false, true
			}
		}
	}
	if not, has := cond["not"].(map[string]any); has {
		m, sok := c.matches(n, not)
		if !sok {
			return false, false
		}
		if m {
			return false, true
		}
	}
	return true, true
}

func typesOf(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		return stringList(t)
	}
	return nil
}

func numberOf(v any) (int, bool) {
	f, ok := v.(float64)
	return int(f), ok
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func kindName(n *yaml.Node) string {
	switch n.Kind {
	case yaml.MappingNode:
		return "a mapping"
	case yaml.SequenceNode:
		return "a list"
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!bool":
			return "a boolean"
		case "!!int":
			return "an integer"
		case "!!float":
			return "a number"
		case "!!null":
			return "null"
		}
		return "a string"
	}
	return "an unknown node"
}

// yaml.v3 decodes these plain spellings into a bool field, so azd accepts them although their tag is !!str.
var boolSpellings = []string{"y", "Y", "yes", "Yes", "YES", "n", "N", "no", "No", "NO", "on", "On", "ON", "off", "Off", "OFF"}

// typeOK reports whether n is acceptable for one of the JSON-schema types. It follows what azd's YAML decoder
// accepts, not the strictest reading: any scalar is a string, null fits every type, and a value with a ${...}
// reference fits scalar types because it is known only after expansion.
func typeOK(n *yaml.Node, types []string) bool {
	if isNull(n) {
		return true
	}
	for _, t := range types {
		switch t {
		case "object":
			if n.Kind == yaml.MappingNode {
				return true
			}
		case "array":
			if n.Kind == yaml.SequenceNode {
				return true
			}
		case "string":
			if n.Kind == yaml.ScalarNode {
				return true
			}
		case "boolean":
			if n.Kind == yaml.ScalarNode && (interpolated(n) || n.ShortTag() == "!!bool" || (n.ShortTag() == "!!str" && n.Style == 0 && slices.Contains(boolSpellings, n.Value))) {
				return true
			}
		case "integer":
			if n.Kind == yaml.ScalarNode && (interpolated(n) || n.ShortTag() == "!!int") {
				return true
			}
		case "number":
			if n.Kind == yaml.ScalarNode && (interpolated(n) || n.ShortTag() == "!!int" || n.ShortTag() == "!!float") {
				return true
			}
		case "null":
			// null already accepted above
		}
	}
	return false
}
