package azureyaml

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

// scan records the ${...} references in one scalar. Syntax (phase-1-fact-check.md, fact 3; ADR-004):
//
//	${VAR}, ${VAR:-x}   azd expands these (drone/envsubst); other shell operators exist
//	$$                  escapes to a single $, so $${VAR} is a literal
//	${{ ... }}          a Foundry expression, passed through; $${{ ... }} is the escaped spelling
//	$VAR                not expanded
//
// A run of n dollar signs before { escapes floor(n/2) of them; the reference is live when n is odd.
func (b *builder) scan(n *yaml.Node, p path) {
	v := n.Value
	if !strings.Contains(v, "$") {
		return
	}
	pos := posOf(n)
	add := func(i Interpolation) {
		i.Path, i.Pos = p.Dotted(), pos
		b.interps = append(b.interps, i)
	}
	for i := 0; i < len(v); {
		if v[i] != '$' {
			i++
			continue
		}
		start := i
		for i < len(v) && v[i] == '$' {
			i++
		}
		run := i - start
		if i >= len(v) || v[i] != '{' {
			continue
		}
		live := run%2 == 1
		exprStart := start + run - 1 // the $ that opens a live reference
		if strings.HasPrefix(v[i:], "{{") {
			if !live {
				add(Interpolation{Form: FormEscapedFoundry, Offset: start})
				continue
			}
			end := strings.Index(v[i+2:], "}}")
			if end < 0 {
				add(Interpolation{Form: FormMalformed, Offset: exprStart})
				return
			}
			add(Interpolation{Form: FormFoundry, Offset: exprStart})
			i += 2 + end + 2
			continue
		}
		name, after := envName(v, i+1)
		if !live {
			if name != "" {
				add(Interpolation{Form: FormEscapedEnv, Name: name, Offset: start})
			}
			continue
		}
		if name == "" {
			add(Interpolation{Form: FormMalformed, Offset: exprStart})
			i++
			continue
		}
		if after < len(v) && v[after] == '}' {
			add(Interpolation{Form: FormEnv, Name: name, Offset: exprStart})
			i = after + 1
			continue
		}
		op := operatorAt(v, after)
		end, nested := closingBrace(v, after)
		if op == "" || end < 0 {
			add(Interpolation{Form: FormMalformed, Name: name, Offset: exprStart})
			i = after
			continue
		}
		form := FormEnvOperator
		if op == ":-" || op == "-" {
			form = FormEnvDefault
		}
		add(Interpolation{Form: form, Name: name, Operator: op, Nested: nested, Offset: exprStart})
		i = end + 1
	}
}

// envName reads [A-Za-z_][A-Za-z0-9_]* at v[i:]. It returns the name and the index after it.
func envName(v string, i int) (string, int) {
	j := i
	for j < len(v) {
		c := v[j]
		isAlpha := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !isAlpha && (j == i || c < '0' || c > '9') {
			break
		}
		j++
	}
	return v[i:j], j
}

// operatorAt returns the shell parameter operator at v[i:], or "" if there is none.
func operatorAt(v string, i int) string {
	if i >= len(v) {
		return ""
	}
	c := v[i]
	next := byte(0)
	if i+1 < len(v) {
		next = v[i+1]
	}
	switch c {
	case ':':
		if next == '-' || next == '=' || next == '+' || next == '?' {
			return v[i : i+2]
		}
		return ""
	case '#', '%', '^', ',', '/':
		if next == c {
			return v[i : i+2]
		}
		return string(c)
	case '-', '=', '+', '?':
		return string(c)
	}
	return ""
}

// closingBrace finds the } that closes the reference whose operator part starts at v[i:], counting nested braces.
// nested reports whether another ${ appears inside.
func closingBrace(v string, i int) (end int, nested bool) {
	depth := 1
	for j := i; j < len(v); j++ {
		switch v[j] {
		case '{':
			depth++
			if j > 0 && v[j-1] == '$' {
				nested = true
			}
		case '}':
			depth--
			if depth == 0 {
				return j, nested
			}
		}
	}
	return -1, nested
}
