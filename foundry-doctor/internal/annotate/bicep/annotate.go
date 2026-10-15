package bicep

import (
	"fmt"
	"slices"
	"strings"
)

const (
	Banner = "// Foundry Doctor review copy (.review). Do not deploy this file."
	prefix = "// Foundry Doctor:"
)

type Comment struct {
	Line int
	Text string
}

// Apply inserts deterministic Bicep comments ahead of the target lines.
func Apply(src []byte, comments []Comment) ([]byte, error) {
	if len(comments) == 0 {
		return src, nil
	}
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
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
		grouped[c.Line] = append(grouped[c.Line], prefix+" "+strings.TrimSpace(c.Text))
	}
	var b strings.Builder
	b.WriteString(Banner)
	b.WriteByte('\n')
	for i, line := range lines {
		lineNo := i + 1
		if cs := grouped[lineNo]; len(cs) > 0 {
			indent := indentation(line)
			for _, c := range cs {
				b.WriteString(indent)
				b.WriteString(c)
				b.WriteByte('\n')
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

func indentation(line string) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n]
}
