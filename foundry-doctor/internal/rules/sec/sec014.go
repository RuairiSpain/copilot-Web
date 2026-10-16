package sec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armexpr"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/armmodel"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FND-SEC-014 check kinds. Finding.Key is "<output name>:<kind>".
const (
	sec014SecureParam = "secure-param"
	sec014ListFunc    = "list-function"
	sec014Name        = "name"
)

// outputSource is the optional model surface FND-SEC-014 reads. *armmodel.Model
// implements it; a model that only implements sdk.ARMOutputs is judged by
// output name alone.
type outputSource interface {
	OutputDetails() []armmodel.OutputDetail
	SecureParams() []string
	VariableText(name string) (string, bool)
}

// evalSEC014 flags root outputs that carry secret material. This follows the
// documented behaviour of the Bicep linter rule outputs-should-not-contain-secrets
// (secure parameters, list*() functions, an output name containing "password";
// @secure() outputs are exempt) and works on the compiled ARM JSON, not on
// Bicep source. Name-only hits other than password are uncertain and are
// reported as per-output skips, never as findings.
func evalSEC014(in *sdk.Input) sdk.Result {
	if src, ok := in.ARM.(outputSource); ok {
		return sec014Detailed(src)
	}
	outs, ok := in.ARM.(sdk.ARMOutputs)
	if !ok {
		return skip(sdk.SkipInputUnavailable + ": compiled ARM outputs are not available")
	}
	// Names only: the value expressions are unknown, so only the name check
	// can run and "no hit" is not a pass.
	var r sdk.Result
	for _, o := range outs.Outputs() {
		sec014NameOnly(&r, o.Name, o.Type)
	}
	if len(r.Findings) == 0 {
		r.Skips = append(r.Skips, sdk.Skip{Reason: sdk.SkipInputUnavailable + ": output value expressions are not available, only output names were checked"})
	}
	return r
}

func sec014Detailed(src outputSource) sdk.Result {
	secure := map[string]bool{}
	for _, p := range src.SecureParams() {
		secure[strings.ToLower(p)] = true
	}
	var r sdk.Result
	for _, o := range src.OutputDetails() {
		if isSecureType(o.Type) {
			continue // @secure() output: the linter skips it too
		}
		sc := sec014Scan{src: src, secure: secure, seenVars: map[string]bool{}}
		sc.scanText(o.Value, 0)
		hit := false
		if names := sc.secureHits(); len(names) > 0 {
			hit = true
			r.Findings = append(r.Findings, sec014Finding(o.Name, sec014SecureParam,
				"output value is derived from secure parameter "+strings.Join(names, ", ")))
		}
		if fns := sc.listHits(); len(fns) > 0 {
			hit = true
			r.Findings = append(r.Findings, sec014Finding(o.Name, sec014ListFunc,
				"output value calls "+strings.Join(fns, ", ")+"()"))
		}
		if !hit {
			sec014NameOnly(&r, o.Name, o.Type)
		}
	}
	return r
}

// sec014NameOnly applies the name check to an output whose value showed no
// secret. A name containing the token password, passwd or pwd is what the
// Bicep linter documents as a failure, so it is a finding. Other secret-shaped
// names (secret, token, key forms, connection string) are uncertain.
func sec014NameOnly(r *sdk.Result, name, typ string) {
	if isSecureType(typ) {
		return
	}
	tok, strong := secretNameToken(name)
	switch {
	case tok == "":
	case strong:
		r.Findings = append(r.Findings, sec014Finding(name, sec014Name,
			"output name contains the word \""+tok+"\" (name-only check)"))
	default:
		r.Skips = append(r.Skips, sdk.Skip{Reason: fmt.Sprintf(
			"uncertain: output %s is named like a secret (%q) but its value is not derived from a secure parameter or a list*() function", name, tok)})
	}
}

func sec014Finding(name, kind, evidence string) sdk.Finding {
	return sdk.Finding{
		Resource: sdk.ResourceRef{Type: "ARM/output", Name: name},
		Key:      name + ":" + kind,
		Evidence: evidence,
	}
}

func isSecureType(t string) bool {
	t = strings.ToLower(t)
	return t == "securestring" || t == "secureobject"
}

var (
	passwordTokens = map[string]bool{"password": true, "passwd": true, "pwd": true}
	secretTokens   = map[string]bool{"secret": true, "secrets": true, "token": true, "tokens": true}
	keyPrefixes    = map[string]bool{"api": true, "access": true, "primary": true, "secondary": true, "shared": true}
)

// secretNameToken splits an output name into tokens and returns the matched
// word and whether it is the password family. Whole tokens only, so
// keyVaultUri, monkey and tokenizerName do not match.
func secretNameToken(name string) (tok string, password bool) {
	toks := nameTokens(name)
	for _, t := range toks {
		if passwordTokens[t] {
			return t, true
		}
	}
	for _, t := range toks {
		if secretTokens[t] {
			return t, false
		}
	}
	for i, t := range toks {
		if (t == "key" || t == "keys") && (i == len(toks)-1 || (i > 0 && keyPrefixes[toks[i-1]])) {
			return t, false
		}
		if t == "connection" && i+1 < len(toks) && toks[i+1] == "string" {
			return "connectionString", false
		}
	}
	return "", false
}

// nameTokens splits on '_', '-', '.', spaces, lower-to-upper case changes,
// acronym ends (APIKey -> API, Key) and letter/digit changes; tokens are
// lower-cased.
func nameTokens(name string) []string {
	rs := []rune(strings.TrimSpace(name))
	var toks []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			toks = append(toks, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 {
			p := rs[i-1]
			switch {
			case unicode.IsLower(p) && unicode.IsUpper(r):
				flush()
			case unicode.IsUpper(p) && unicode.IsUpper(r) && i+1 < len(rs) && unicode.IsLower(rs[i+1]):
				flush()
			case unicode.IsDigit(p) != unicode.IsDigit(r):
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return toks
}

// sec014Scan collects what an output's value text references.
type sec014Scan struct {
	src      outputSource
	secure   map[string]bool
	seenVars map[string]bool
	params   map[string]bool
	lists    map[string]bool
}

const sec014MaxVarDepth = 8

func (s *sec014Scan) secureHits() []string { return sortedKeys(s.params) }
func (s *sec014Scan) listHits() []string   { return sortedKeys(s.lists) }

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// scanText scans one value: a literal string (ignored unless it is an ARM
// expression) or JSON text (objects and arrays are walked for expression
// strings).
func (s *sec014Scan) scanText(text string, depth int) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if text[0] == '{' || text[0] == '[' || text[0] == '"' {
		var v any
		if json.Unmarshal([]byte(text), &v) == nil {
			s.scanValue(v, depth)
			return
		}
	}
	s.scanString(text, depth)
}

func (s *sec014Scan) scanValue(v any, depth int) {
	switch x := v.(type) {
	case string:
		s.scanString(x, depth)
	case []any:
		for _, e := range x {
			s.scanValue(e, depth)
		}
	case map[string]any:
		for _, e := range x {
			s.scanValue(e, depth)
		}
	}
}

func (s *sec014Scan) scanString(str string, depth int) {
	str = strings.TrimSpace(str)
	if !armexpr.IsExpression(str) || !strings.HasSuffix(str, "]") {
		return
	}
	for _, c := range exprCalls(str[1 : len(str)-1]) {
		switch name := strings.ToLower(c.name); {
		case name == "parameters":
			if c.arg != "" && s.secure[strings.ToLower(c.arg)] {
				if s.params == nil {
					s.params = map[string]bool{}
				}
				s.params[c.arg] = true
			}
		case name == "variables":
			key := strings.ToLower(c.arg)
			if c.arg == "" || s.seenVars[key] || depth >= sec014MaxVarDepth {
				continue
			}
			s.seenVars[key] = true
			if text, ok := s.src.VariableText(c.arg); ok {
				s.scanText(text, depth+1)
			}
		case strings.HasPrefix(name, "list") && len(name) > 4:
			if s.lists == nil {
				s.lists = map[string]bool{}
			}
			s.lists[c.name] = true
		}
	}
}

// exprCall is a function call in an ARM expression; arg is the first argument
// when it is a string literal.
type exprCall struct {
	name string
	arg  string
}

// exprCalls lists the function calls of an ARM expression body, skipping
// single-quoted string literals (” is an escaped quote) and member accesses
// such as ns.func( .
func exprCalls(body string) []exprCall {
	var out []exprCall
	rs := []rune(body)
	n := len(rs)
	isIdent := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }
	for i := 0; i < n; {
		r := rs[i]
		if r == '\'' {
			i = skipQuoted(rs, i)
			continue
		}
		if !(unicode.IsLetter(r) || r == '_') {
			i++
			continue
		}
		j := i
		for j < n && isIdent(rs[j]) {
			j++
		}
		name := string(rs[i:j])
		member := i > 0 && rs[i-1] == '.'
		k := j
		for k < n && unicode.IsSpace(rs[k]) {
			k++
		}
		if !member && k < n && rs[k] == '(' {
			c := exprCall{name: name}
			a := k + 1
			for a < n && unicode.IsSpace(rs[a]) {
				a++
			}
			if a < n && rs[a] == '\'' {
				end := skipQuoted(rs, a)
				c.arg = strings.ReplaceAll(string(rs[a+1:max(a+1, end-1)]), "''", "'")
			}
			out = append(out, c)
		}
		i = j
	}
	return out
}

// skipQuoted returns the index just after the single-quoted literal that
// starts at rs[i]; an unterminated literal runs to the end.
func skipQuoted(rs []rune, i int) int {
	for i++; i < len(rs); i++ {
		if rs[i] == '\'' {
			if i+1 < len(rs) && rs[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(rs)
}
