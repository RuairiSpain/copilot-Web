package ops

import (
	"errors"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// A deliberately small ARM expression evaluator. It understands string literals, concat, toLower,
// toUpper, union and createObject, and parameters('environmentName'), which azd templates map to
// the AZURE_ENV_NAME environment value. Anything else is unresolved. It never guesses.

const (
	reasonUnique      = "uniquestring-length-unknown"
	reasonUnsupported = "unsupported-expression"
	reasonEnvName     = "azd-environment-name-unavailable"
)

type unresolvedError struct{ reason string }

func (e unresolvedError) Error() string { return "unresolved: " + e.reason }

var errUnsupported = unresolvedError{reasonUnsupported}

type evaluator struct {
	src string
	pos int
	env *model.Environment
	// fromEnv is set when the result depends on an azd environment value.
	fromEnv bool
}

// evalARM evaluates an ARM expression ("[...]"). The returned error is an unresolvedError.
func evalARM(expr string, env *model.Environment) (val any, fromEnv bool, err error) {
	if !model.IsARMExpression(expr) {
		return nil, false, errUnsupported
	}
	ev := &evaluator{src: expr[1 : len(expr)-1], env: env}
	v, err := ev.expr()
	if err != nil {
		return nil, false, err
	}
	ev.skipSpace()
	if ev.pos != len(ev.src) {
		return nil, false, errUnsupported
	}
	return v, ev.fromEnv, nil
}

func (e *evaluator) skipSpace() {
	for e.pos < len(e.src) && (e.src[e.pos] == ' ' || e.src[e.pos] == '\t' || e.src[e.pos] == '\n') {
		e.pos++
	}
}

func (e *evaluator) expr() (any, error) {
	e.skipSpace()
	if e.pos >= len(e.src) {
		return nil, errUnsupported
	}
	if e.src[e.pos] == '\'' {
		return e.str()
	}
	start := e.pos
	for e.pos < len(e.src) && isIdent(e.src[e.pos]) {
		e.pos++
	}
	name := strings.ToLower(e.src[start:e.pos])
	e.skipSpace()
	if name == "" || e.pos >= len(e.src) || e.src[e.pos] != '(' {
		return nil, errUnsupported
	}
	e.pos++
	if name == "uniquestring" {
		// The arguments do not matter: the length of the token azd builds from it is not documented.
		return nil, unresolvedError{reasonUnique}
	}
	var args []any
	for {
		e.skipSpace()
		if e.pos < len(e.src) && e.src[e.pos] == ')' {
			e.pos++
			break
		}
		if len(args) > 0 {
			if e.pos >= len(e.src) || e.src[e.pos] != ',' {
				return nil, errUnsupported
			}
			e.pos++
		}
		a, err := e.expr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
	}
	return e.call(name, args)
}

func isIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func (e *evaluator) str() (any, error) {
	e.pos++ // opening quote
	var b strings.Builder
	for e.pos < len(e.src) {
		c := e.src[e.pos]
		if c == '\'' {
			if e.pos+1 < len(e.src) && e.src[e.pos+1] == '\'' {
				b.WriteByte('\'')
				e.pos += 2
				continue
			}
			e.pos++
			return b.String(), nil
		}
		b.WriteByte(c)
		e.pos++
	}
	return nil, errUnsupported
}

func stringArgs(args []any) ([]string, bool) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		s, ok := a.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func (e *evaluator) call(name string, args []any) (any, error) {
	switch name {
	case "uniquestring":
		return nil, unresolvedError{reasonUnique}
	case "concat":
		s, ok := stringArgs(args)
		if !ok {
			return nil, errUnsupported
		}
		return strings.Join(s, ""), nil
	case "tolower", "toupper":
		s, ok := stringArgs(args)
		if !ok || len(s) != 1 {
			return nil, errUnsupported
		}
		if name == "tolower" {
			return strings.ToLower(s[0]), nil
		}
		return strings.ToUpper(s[0]), nil
	case "parameters":
		s, ok := stringArgs(args)
		if !ok || len(s) != 1 || !strings.EqualFold(s[0], "environmentName") || e.env == nil {
			return nil, errUnsupported
		}
		v, ok := e.env.Get("AZURE_ENV_NAME")
		if !ok {
			return nil, unresolvedError{reasonEnvName}
		}
		e.fromEnv = true
		return v.Reveal(), nil
	case "createobject":
		if len(args)%2 != 0 {
			return nil, errUnsupported
		}
		m := map[string]any{}
		for i := 0; i < len(args); i += 2 {
			k, ok := args[i].(string)
			if !ok {
				return nil, errUnsupported
			}
			m[k] = args[i+1]
		}
		return m, nil
	case "union":
		m := map[string]any{}
		for _, a := range args {
			o, ok := a.(map[string]any)
			if !ok {
				return nil, errUnsupported
			}
			for k, v := range o {
				m[k] = v
			}
		}
		return m, nil
	}
	return nil, errUnsupported
}

// reasonOf extracts the unresolved reason from an evaluator error.
func reasonOf(err error) string {
	var u unresolvedError
	if errors.As(err, &u) {
		return u.reason
	}
	return reasonUnsupported
}
