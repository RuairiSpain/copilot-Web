package html

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/assess"
)

func TestRenderAccessibleEscaped(t *testing.T) {
	a := assess.Assessment{
		Profile:  "foundry-prod",
		Decision: "Proceed with conditions.",
		Controls: []assess.Control{
			{Title: `<img src=x onerror=1>`, Pillar: "security", State: assess.StateFail, Recommendation: `Escape <script>`},
		},
	}
	var b bytes.Buffer
	if err := Render(&b, a); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{`<html lang="en">`, "<h1>", "<h2"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, bad := range []string{"<script>", "<img src=x onerror=1>"} {
		if strings.Contains(s, bad) {
			t.Fatalf("unsafe html: %s", s)
		}
	}
}
