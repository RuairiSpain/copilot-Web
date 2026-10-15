package project

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ErrDotEnv wraps every ParseDotEnv failure. The error text never contains a
// value; it names only the byte offset class of the problem.
var ErrDotEnv = errors.New("invalid .env content")

// ParseDotEnv parses .env content with the grammar of github.com/joho/godotenv
// v1.5.1 (parser.go), which azd uses to read .azure/<env>/.env. It is an
// in-repository port of that grammar, not the module: the dependency is absent
// from go.mod and docs/licence-inventory.md, and the grammar is small. Behaviour
// ported, including its quirks:
//
//   - KEY=VALUE or KEY: VALUE, optional leading "export " ; key characters are
//     letters, digits, '_' and '.', anything else is an error;
//   - a statement starting with '#' is a comment line;
//   - an unquoted value runs to end of line, is trimmed and ends at a '#' that
//     follows whitespace; $VAR and ${VAR} (upper-case names) expand from earlier
//     keys of the same content, never from the process environment;
//   - a single-quoted value is literal; a double-quoted value may span lines,
//     expands \n and \r, unescapes other backslash pairs and expands variables;
//   - an unterminated quote is an error; a later duplicate key overwrites an
//     earlier one; CRLF is normalised.
//
// Differences from godotenv: none intended. The key-name check uses the same
// byte-wise unicode.IsLetter/IsNumber test as the original.
func ParseDotEnv(src []byte) (map[string]string, error) {
	out := map[string]string{}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	cut := src
	for {
		cut = dotenvStatementStart(cut)
		if cut == nil {
			return out, nil
		}
		key, left, err := dotenvKey(cut)
		if err != nil {
			return nil, err
		}
		val, left, err := dotenvValue(left, out)
		if err != nil {
			return nil, err
		}
		out[key] = val
		cut = left
	}
}

var (
	dotenvEscape   = regexp.MustCompile(`\\.`)
	dotenvExpand   = regexp.MustCompile(`(\\)?(\$)(\()?\{?([A-Z0-9_]+)?\}?`)
	dotenvUnescape = regexp.MustCompile(`\\([^$])`)
)

// dotenvSpace is godotenv's isSpace: white space that is not a line feed.
func dotenvSpace(r rune) bool {
	switch r {
	case '\t', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return false
}

func dotenvStatementStart(src []byte) []byte {
	for {
		i := bytes.IndexFunc(src, func(r rune) bool { return !unicode.IsSpace(r) })
		if i == -1 {
			return nil
		}
		src = src[i:]
		if src[0] != '#' {
			return src
		}
		i = bytes.IndexByte(src, '\n')
		if i == -1 {
			return nil
		}
		src = src[i:]
	}
}

func dotenvKey(src []byte) (string, []byte, error) {
	const export = "export"
	src = bytes.TrimLeftFunc(src, dotenvSpace)
	if bytes.HasPrefix(src, []byte(export)) {
		trimmed := bytes.TrimPrefix(src, []byte(export))
		if i := bytes.IndexFunc(trimmed, dotenvSpace); i == 0 {
			src = bytes.TrimLeftFunc(trimmed, dotenvSpace)
		}
	}
	offset := 0
	key := ""
loop:
	for i, c := range src {
		r := rune(c)
		if dotenvSpace(r) {
			continue
		}
		switch c {
		case '=', ':':
			key = string(src[:i])
			offset = i + 1
			break loop
		case '_':
		default:
			if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' {
				continue
			}
			return "", nil, fmt.Errorf("%w: unexpected character in variable name", ErrDotEnv)
		}
	}
	if len(src) == 0 {
		return "", nil, fmt.Errorf("%w: zero length statement", ErrDotEnv)
	}
	key = strings.TrimRightFunc(key, unicode.IsSpace)
	return key, bytes.TrimLeftFunc(src[offset:], dotenvSpace), nil
}

func dotenvValue(src []byte, vars map[string]string) (string, []byte, error) {
	var quote byte
	if len(src) > 0 && (src[0] == '"' || src[0] == '\'') {
		quote = src[0]
	}
	if quote == 0 {
		end := bytes.IndexFunc(src, func(r rune) bool { return r == '\n' || r == '\r' })
		if end == -1 {
			end = len(src)
			if end == 0 {
				return "", nil, nil
			}
		}
		line := []rune(string(src[:end]))
		stop := len(line)
		if stop == 0 {
			return "", src[end:], nil
		}
		for i := stop - 1; i >= 0; i-- {
			if line[i] == '#' && i > 0 && dotenvSpace(line[i-1]) {
				stop = i
				break
			}
		}
		return dotenvExpandVars(strings.TrimFunc(string(line[:stop]), dotenvSpace), vars), src[end:], nil
	}
	for i := 1; i < len(src); i++ {
		if src[i] != quote || src[i-1] == '\\' {
			continue
		}
		trim := func(r rune) bool { return r == rune(quote) }
		v := string(bytes.TrimLeftFunc(bytes.TrimRightFunc(src[:i], trim), trim))
		if quote == '"' {
			v = dotenvExpandVars(dotenvExpandEscapes(v), vars)
		}
		return v, src[i+1:], nil
	}
	return "", nil, fmt.Errorf("%w: unterminated quoted value", ErrDotEnv)
}

func dotenvExpandEscapes(s string) string {
	out := dotenvEscape.ReplaceAllStringFunc(s, func(m string) string {
		switch strings.TrimPrefix(m, `\`) {
		case "n":
			return "\n"
		case "r":
			return "\r"
		}
		return m
	})
	return dotenvUnescape.ReplaceAllString(out, "$1")
}

func dotenvExpandVars(v string, m map[string]string) string {
	return dotenvExpand.ReplaceAllStringFunc(v, func(s string) string {
		sub := dotenvExpand.FindStringSubmatch(s)
		if sub == nil {
			return s
		}
		// godotenv v1.5.1 tests submatch[2] == "(", which never holds; the
		// port keeps that behaviour.
		if sub[1] == "\\" || sub[2] == "(" {
			return sub[0][1:]
		}
		if sub[4] != "" {
			return m[sub[4]]
		}
		return s
	})
}
