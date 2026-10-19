package bicep

import (
	"fmt"
	"slices"
	"strings"

	srclines "github.com/ruairispain/copilot-web/foundry-doctor/internal/annotate/lines"
)

const (
	Banner = "// Foundry Doctor review copy (.review). Do not deploy this file."
	prefix = "// Foundry Doctor:"
)

type Comment struct {
	Line int
	Text string
}

const bom = "\uFEFF"

// Apply writes the review banner and inserts deterministic Bicep comments
// ahead of the target lines. The banner is written even when comments is
// empty. A comment for a line that follows #disable-next-line directives is
// placed above the directives, so they still apply to the original line. A
// leading UTF-8 byte order mark stays first.
func Apply(src []byte, comments []Comment) ([]byte, error) {
	text := string(src)
	hasBOM := strings.HasPrefix(text, bom)
	text = strings.TrimPrefix(text, bom)
	lines, seps := srclines.Split(text, false)
	eol := srclines.EOL(text)
	for _, c := range comments {
		if c.Line <= 0 || c.Line > len(lines) {
			return nil, fmt.Errorf("bicep comment line %d out of range", c.Line)
		}
	}
	slices.SortFunc(comments, func(a, b Comment) int {
		switch {
		case a.Line < b.Line:
			return -1
		case a.Line > b.Line:
			return 1
		case a.Text < b.Text:
			return -1
		case a.Text > b.Text:
			return 1
		default:
			return 0
		}
	})
	grouped := map[int][]string{}
	for _, c := range comments {
		at := c.Line
		for at > 1 && isDisableNextLine(lines[at-2]) {
			at--
		}
		grouped[at] = append(grouped[at], prefix+" "+strings.TrimSpace(c.Text))
	}
	var b strings.Builder
	if hasBOM {
		b.WriteString(bom)
	}
	b.WriteString(Banner)
	b.WriteString(eol)
	for i, line := range lines {
		lineNo := i + 1
		if cs := grouped[lineNo]; len(cs) > 0 {
			indent := indentation(line)
			for _, c := range cs {
				b.WriteString(indent)
				b.WriteString(c)
				b.WriteString(eol)
			}
		}
		b.WriteString(line)
		b.WriteString(seps[i])
	}
	return []byte(b.String()), nil
}

func isDisableNextLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#disable-next-line")
}

func indentation(line string) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n]
}
