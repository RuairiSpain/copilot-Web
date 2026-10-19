// Package lines splits source text into lines the way the YAML and Bicep
// scanners count them, and keeps each line's original separator so an
// annotated copy keeps the source's line endings.
package lines

import "strings"

// Split cuts text into lines and the separator that ended each one (empty for
// a final line without a break). CR, LF and CRLF always break a line. With
// unicode set, NEL (U+0085), LS (U+2028) and PS (U+2029) break one too, as the
// YAML scanner counts them; the Bicep lexer does not.
//
// A line number from a finding is only reliable if it is counted the same way
// the tool that produced it counted, so each caller picks the matching rule.
func Split(text string, unicode bool) (lines, seps []string) {
	start := 0
	for i := 0; i < len(text); {
		switch {
		case text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n':
			lines, seps = append(lines, text[start:i]), append(seps, "\r\n")
			i += 2
			start = i
		case text[i] == '\r', text[i] == '\n':
			lines, seps = append(lines, text[start:i]), append(seps, text[i:i+1])
			i++
			start = i
		case unicode && strings.HasPrefix(text[i:], "\u0085"):
			lines, seps = append(lines, text[start:i]), append(seps, "\u0085")
			i += len("\u0085")
			start = i
		case unicode && (strings.HasPrefix(text[i:], "\u2028") || strings.HasPrefix(text[i:], "\u2029")):
			lines, seps = append(lines, text[start:i]), append(seps, text[i:i+3])
			i += 3
			start = i
		default:
			i++
		}
	}
	if start < len(text) {
		lines, seps = append(lines, text[start:]), append(seps, "")
	}
	return lines, seps
}

// EOL returns the line ending to use for inserted lines: the source's first
// line break (CRLF, CR or LF), so a copy never mixes endings. LF when the
// source has no line break.
func EOL(text string) string {
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		if strings.HasPrefix(text[i:], "\r\n") {
			return "\r\n"
		}
		return text[i : i+1]
	}
	return "\n"
}
