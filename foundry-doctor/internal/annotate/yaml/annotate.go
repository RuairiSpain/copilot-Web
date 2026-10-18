package yaml

import (
	"fmt"
	"slices"
	"strings"
)

const (
	Banner = "# Foundry Doctor review copy (.review). Do not deploy this file."
	prefix = "# Foundry Doctor:"
)

type Comment struct {
	Line int
	Text string
}

const bom = "\uFEFF"

// Apply writes the review banner and inserts deterministic YAML comment lines
// ahead of the target lines. The banner is written even when comments is
// empty. A leading UTF-8 byte order mark stays first.
func Apply(src []byte, comments []Comment) ([]byte, error) {
	text := string(src)
	hasBOM := strings.HasPrefix(text, bom)
	text = strings.TrimPrefix(text, bom)
	lines, ends := split([]byte(text))
	for _, c := range comments {
		if c.Line <= 0 || c.Line > len(lines) {
			return nil, fmt.Errorf("yaml comment line %d out of range", c.Line)
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
	if hasBOM {
		b.WriteString(bom)
	}
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
		if ends[i] {
			b.WriteByte('\n')
		}
	}
	return []byte(b.String()), nil
}

func split(src []byte) ([]string, []bool) {
	raw := strings.ReplaceAll(string(src), "\r\n", "\n")
	trailing := strings.HasSuffix(raw, "\n")
	raw = strings.TrimSuffix(raw, "\n")
	if raw == "" {
		if trailing {
			return []string{""}, []bool{true}
		}
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	ends := make([]bool, len(lines))
	for i := range ends {
		ends[i] = i < len(lines)-1 || trailing
	}
	return lines, ends
}

func indentation(line string) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n]
}
