package diff

import (
	"bytes"
	"fmt"
	"strings"
)

const noNewline = "\\ No newline at end of file\n"

// Unified emits a deterministic full-file unified diff when before and after
// differ. The single hunk replaces the whole file: review copies are
// generated artefacts and do not need an edit-distance-optimised patch. CRLF
// line endings are shown as LF. The diff carries the full source of each
// annotated file, so handle it like source.
func Unified(path string, before, after []byte) []byte {
	if bytes.Equal(before, after) {
		return nil
	}
	var b strings.Builder
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	fmt.Fprintf(&b, "--- a/%s\n", path)
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -%s +%s @@\n", hunkRange(len(beforeLines)), hunkRange(len(afterLines)))
	writeLines(&b, '-', beforeLines, before)
	writeLines(&b, '+', afterLines, after)
	return []byte(b.String())
}

// hunkRange follows the unified format: an empty side is "0,0".
func hunkRange(n int) string {
	if n == 0 {
		return "0,0"
	}
	return fmt.Sprintf("1,%d", n)
}

func writeLines(b *strings.Builder, mark byte, lines []string, raw []byte) {
	for _, line := range lines {
		b.WriteByte(mark)
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if len(lines) > 0 && !bytes.HasSuffix(raw, []byte("\n")) {
		b.WriteString(noNewline)
	}
}

func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
