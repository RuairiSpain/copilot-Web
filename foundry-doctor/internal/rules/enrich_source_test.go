package rules

import (
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func TestEnrichSourceSelection(t *testing.T) {
	c := cr("FND-X-001", nil, "")
	c.Sources = []catalog.Source{
		{URL: "https://example.test/first", LastVerified: "2026-10-01"},
		{URL: "https://example.test/second", LastVerified: "2026-10-16"},
	}
	cases := []struct {
		name, docs, wantURL, wantVerified string
	}{
		{"default is the first source", "", "https://example.test/first", "2026-10-01"},
		{"a catalogue source named by the rule", "https://example.test/second", "https://example.test/second", "2026-10-16"},
		{"an unknown URL is ignored", "https://evil.test/x", "https://example.test/first", "2026-10-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := finding()
			f.DocsURL = tc.docs
			out := Enrich(f, c, "foundry-prod", sdk.SeverityError)
			if out.DocsURL != tc.wantURL || out.LastVerified != tc.wantVerified {
				t.Fatalf("got %s %s, want %s %s", out.DocsURL, out.LastVerified, tc.wantURL, tc.wantVerified)
			}
		})
	}
}
