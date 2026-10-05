package azureyaml

import (
	"strings"
	"testing"
)

func TestYAMLLevelIssuesAreCapped(t *testing.T) {
	src := "a: &x 1\nb: [" + strings.Repeat("*x,", 500) + "]\n"
	r, err := Parse([]byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var aliases, summaries int
	for _, i := range r.Issues {
		if i.Code != CodeAlias {
			continue
		}
		if strings.Contains(i.Message, "more yaml-alias issues") {
			summaries++
			if !strings.HasPrefix(i.Message, "400 more") {
				t.Errorf("summary = %q", i.Message)
			}
			continue
		}
		aliases++
	}
	if aliases != maxYAMLIssuesPerCode || summaries != 1 {
		t.Errorf("aliases = %d, summaries = %d", aliases, summaries)
	}
}
