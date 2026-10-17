package armexpr

import "strings"

// DeploymentsType is the resource type of a module deployment.
const DeploymentsType = "Microsoft.Resources/deployments"

// RewriteDeploymentOutputs replaces each read of a module deployment output in
// the ARM expression s:
//
//	reference(resourceId('Microsoft.Resources/deployments', 'name'), '2025-04-01').outputs.id.value
//
// (or reference('name', ...) for a deployment addressed by symbolic name). f
// receives the deployment name and the output name and returns replacement
// expression text (without brackets) and true, or false to keep the read as it
// is. A string that is not an expression is returned unchanged. When the
// result is a single string literal the plain literal is returned.
func RewriteDeploymentOutputs(s string, f func(deployment, output string) (string, bool)) string {
	if !IsExpression(s) || !strings.HasSuffix(s, "]") || !strings.Contains(strings.ToLower(s), "outputs") {
		return s
	}
	inner := s[1 : len(s)-1]
	var b strings.Builder
	changed := false
	inQuote := false
	for i := 0; i < len(inner); {
		c := inner[i]
		if c == '\'' {
			inQuote = !inQuote
			b.WriteByte(c)
			i++
			continue
		}
		if !inQuote && isIdentStart(c) && (i == 0 || !isIdentOrDot(inner[i-1])) {
			if dep, out, n, ok := matchOutputRead(inner[i:]); ok {
				if repl, ok := f(dep, out); ok {
					rest := inner[i+n:]
					if !(strings.HasPrefix(repl, "'") && (strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, "["))) {
						b.WriteString(repl)
						i += n
						changed = true
						continue
					}
				}
			}
		}
		b.WriteByte(c)
		i++
	}
	if !changed {
		return s
	}
	out := b.String()
	if v, ok := Unquote(out); ok {
		if strings.HasPrefix(v, "[") {
			return "[" + v
		}
		return v
	}
	return "[" + out + "]"
}

// matchOutputRead matches reference(<deployment>[, api]).outputs.<name>.value
// at the start of s and returns its length.
func matchOutputRead(s string) (deployment, output string, n int, ok bool) {
	const fn = "reference"
	if len(s) < len(fn) || !strings.EqualFold(s[:len(fn)], fn) {
		return
	}
	open := skipSpace(s, len(fn))
	if open >= len(s) || s[open] != '(' {
		return
	}
	end := matchParen(s, open)
	if end < 0 {
		return
	}
	args, aok := SplitArgs(s[open+1 : end])
	if !aok || len(args) == 0 || len(args) > 3 {
		return
	}
	if name, lit := Unquote(args[0]); lit {
		deployment = name
	} else {
		c, cok := ParseCall(args[0])
		if !cok || c.Fn != "resourceid" || c.Suffix != "" || len(c.Args) != 2 {
			return
		}
		typ, tok := Unquote(c.Args[0])
		name, nok := Unquote(c.Args[1])
		if !tok || !nok || !strings.EqualFold(typ, DeploymentsType) {
			return
		}
		deployment = name
	}
	rest := s[end+1:]
	const outs = ".outputs."
	if len(rest) < len(outs) || !strings.EqualFold(rest[:len(outs)], outs) {
		return "", "", 0, false
	}
	id := identRe.FindString(rest[len(outs):])
	if id == "" {
		return "", "", 0, false
	}
	tail := rest[len(outs)+len(id):]
	const val = ".value"
	if len(tail) < len(val) || !strings.EqualFold(tail[:len(val)], val) {
		return "", "", 0, false
	}
	if next := tail[len(val):]; next != "" && isIdentOrDot(next[0]) && next[0] != '.' {
		return "", "", 0, false
	}
	return deployment, id, end + 1 + len(outs) + len(id) + len(val), true
}
