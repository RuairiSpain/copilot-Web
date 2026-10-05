package project

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// ParseError is a .env syntax error. It carries a line number and a fixed reason, never any text
// from the file, because the file may hold secrets.
type ParseError struct {
	Line   int    // 1-based line of the statement
	Reason string // fixed vocabulary, see the Reason* constants
}

// Reasons used in ParseError.
const (
	ReasonBinary       = "binary content (NUL byte)"
	ReasonBadKeyChar   = "unexpected character in variable name"
	ReasonNoSeparator  = "missing '=' or ':' after variable name"
	ReasonEmptyKey     = "empty variable name"
	ReasonUnterminated = "unterminated quoted value"
	ReasonCancelled    = "cancelled"
)

// Error implements error.
func (e *ParseError) Error() string { return fmt.Sprintf(".env line %d: %s", e.Line, e.Reason) }

// EnvResult is a parsed .env file.
type EnvResult struct {
	Env           model.Environment
	DuplicateKeys []string // keys assigned more than once (the last assignment wins), sorted
}

var (
	escapeRegex        = regexp.MustCompile(`\\.`)
	expandVarRegex     = regexp.MustCompile(`(\\)?(\$)(\()?\{?([A-Z0-9_]+)?\}?`)
	unescapeCharsRegex = regexp.MustCompile(`\\([^$])`)
)

// ParseEnv parses a .env file with the grammar of github.com/joho/godotenv v1.5.1 (parser.go),
// the reader azd uses for .azure/<env>/.env (docs/development/phase-1-fact-check.md, fact 2a):
//
//   - CRLF is normalised to LF; a UTF-8 BOM is not skipped and is a syntax error, as in godotenv.
//   - A statement is KEY=VALUE or KEY: VALUE, with an optional leading "export" followed by blank.
//   - Key characters are letters, digits, '_' and '.'; blanks inside are tolerated and trimmed at the end.
//   - A line whose first non-space character is '#' is a comment.
//   - Unquoted values run to end of line, are trimmed, and end at a '#' preceded by blank. They
//     expand $VAR and ${VAR} like double-quoted values (godotenv v1.5.1 does this).
//   - Single-quoted values are literal.
//   - Double-quoted values may span lines; \n and \r are expanded, other backslash pairs are
//     unescaped (except \$), and $VAR / ${VAR} (uppercase names) expand from earlier keys of the
//     same file only, never from the process environment. An unknown name expands to "".
//   - An unterminated quote is an error. A later duplicate key overwrites the earlier one.
//
// Deliberate deviations (godotenv accepts these and produces a junk entry): a statement with no
// separator and an empty key are errors here (ReasonNoSeparator, ReasonEmptyKey).
//
// Data containing a NUL byte is rejected as binary. ctx is checked between statements.
func ParseEnv(ctx context.Context, name string, data []byte) (EnvResult, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return EnvResult{}, &ParseError{Line: 1 + bytes.Count(data[:bytes.IndexByte(data, 0)], []byte{'\n'}), Reason: ReasonBinary}
	}
	src := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	total := len(src)
	lineAt := func(rest []byte) int { return 1 + bytes.Count(src[:total-len(rest)], []byte{'\n'}) }

	out := map[string]string{}
	seen := map[string]int{}
	cut := src
	for n := 0; ; n++ {
		if n%256 == 0 {
			if err := ctx.Err(); err != nil {
				return EnvResult{}, fmt.Errorf("parse environment %q: %w", name, err)
			}
		}
		cut = statementStart(cut)
		if cut == nil {
			break
		}
		stmt := cut
		key, left, reason := locateKey(cut)
		if reason != "" {
			return EnvResult{}, &ParseError{Line: lineAt(stmt), Reason: reason}
		}
		value, left, reason := extractValue(left, out)
		if reason != "" {
			return EnvResult{}, &ParseError{Line: lineAt(stmt), Reason: reason}
		}
		out[key] = value
		seen[key]++
		cut = left
	}
	var dups []string
	for k, c := range seen {
		if c > 1 {
			dups = append(dups, k)
		}
	}
	slices.Sort(dups)
	return EnvResult{Env: model.NewEnvironment(name, out), DuplicateKeys: dups}, nil
}

// IsParseError reports whether err is a *ParseError and returns it.
func IsParseError(err error) (*ParseError, bool) {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}

func statementStart(src []byte) []byte {
	for {
		pos := bytes.IndexFunc(src, func(r rune) bool { return !unicode.IsSpace(r) })
		if pos == -1 {
			return nil
		}
		src = src[pos:]
		if src[0] != '#' {
			return src
		}
		pos = bytes.IndexByte(src, '\n')
		if pos == -1 {
			return nil
		}
		src = src[pos:]
	}
}

// isBlank is godotenv's isSpace: blanks but not line breaks (\n), including \r.
func isBlank(r rune) bool {
	switch r {
	case '\t', '\v', '\f', '\r', ' ', 0x85, 0xA0:
		return true
	}
	return false
}

func locateKey(src []byte) (key string, rest []byte, reason string) {
	src = bytes.TrimLeftFunc(src, isBlank)
	if bytes.HasPrefix(src, []byte("export")) {
		trimmed := bytes.TrimPrefix(src, []byte("export"))
		if bytes.IndexFunc(trimmed, isBlank) == 0 {
			src = bytes.TrimLeftFunc(trimmed, isBlank)
		}
	}
	offset := -1
loop:
	for i, char := range src {
		rchar := rune(char) // godotenv ranges over bytes, so bytes above 0x7f are tested as Latin-1
		if isBlank(rchar) {
			continue
		}
		switch char {
		case '=', ':':
			key = string(src[:i])
			offset = i + 1
			break loop
		case '_':
		default:
			if unicode.IsLetter(rchar) || unicode.IsNumber(rchar) || rchar == '.' {
				continue
			}
			return "", nil, ReasonBadKeyChar
		}
	}
	if offset < 0 {
		return "", nil, ReasonNoSeparator
	}
	key = strings.TrimRightFunc(key, unicode.IsSpace)
	if key == "" {
		return "", nil, ReasonEmptyKey
	}
	return key, bytes.TrimLeftFunc(src[offset:], isBlank), ""
}

func extractValue(src []byte, vars map[string]string) (value string, rest []byte, reason string) {
	var quote byte
	if len(src) > 0 && (src[0] == '"' || src[0] == '\'') {
		quote = src[0]
	}
	if quote == 0 {
		end := bytes.IndexFunc(src, func(r rune) bool { return r == '\n' || r == '\r' })
		if end == -1 {
			end = len(src)
			if end == 0 {
				return "", nil, ""
			}
		}
		line := []rune(string(src[:end]))
		endOfVar := len(line)
		if endOfVar == 0 {
			return "", src[end:], ""
		}
		for i := endOfVar - 1; i >= 0; i-- {
			if line[i] == '#' && i > 0 && isBlank(line[i-1]) {
				endOfVar = i
				break
			}
		}
		trimmed := strings.TrimFunc(string(line[:endOfVar]), isBlank)
		return expandVariables(trimmed, vars), src[end:], ""
	}
	for i := 1; i < len(src); i++ {
		if src[i] != quote || src[i-1] == '\\' {
			continue
		}
		trim := func(r rune) bool { return r == rune(quote) }
		value = string(bytes.TrimLeftFunc(bytes.TrimRightFunc(src[:i], trim), trim))
		if quote == '"' {
			value = expandVariables(expandEscapes(value), vars)
		}
		return value, src[i+1:], ""
	}
	return "", nil, ReasonUnterminated
}

func expandEscapes(s string) string {
	out := escapeRegex.ReplaceAllStringFunc(s, func(m string) string {
		switch strings.TrimPrefix(m, `\`) {
		case "n":
			return "\n"
		case "r":
			return "\r"
		default:
			return m
		}
	})
	return unescapeCharsRegex.ReplaceAllString(out, "$1")
}

func expandVariables(v string, m map[string]string) string {
	return expandVarRegex.ReplaceAllStringFunc(v, func(s string) string {
		sub := expandVarRegex.FindStringSubmatch(s)
		if sub == nil {
			return s
		}
		if sub[1] == `\` || sub[2] == "(" {
			return sub[0][1:]
		} else if sub[4] != "" {
			return m[sub[4]]
		}
		return s
	})
}
