package diff

import (
	"bytes"
	"fmt"
	"strings"
)

// Unified emits a deterministic full-file unified diff when before and after
// differ. The hunk is intentionally coarse: review copies are generated
// artefacts and do not need an edit-distance-optimised patch.
func Unified(path string, before, after []byte) []byte {
	if bytes.Equal(before, after) {
		return nil
	}
	var b strings.Builder
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	fmt.Fprintf(&b, "--- a/%s\n", path)
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(beforeLines), len(afterLines))
	for _, line := range beforeLines {
		b.WriteByte('-')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, line := range afterLines {
		b.WriteByte('+')
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func splitLines(data []byte) []string {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}
