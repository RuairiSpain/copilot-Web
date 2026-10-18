package wara

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	ruledata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

var guid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func loadRules(t *testing.T) map[string]catalog.Rule {
	t.Helper()
	rules, err := catalog.Load(context.Background(), ruledata.FS, ruledata.Root)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]catalog.Rule, len(rules))
	for _, r := range rules {
		out[r.ID] = r
	}
	return out
}

func TestEveryWaraRuleHasMapping(t *testing.T) {
	for _, r := range loadRules(t) {
		if !slices.Contains(r.Basis, "wara") {
			continue
		}
		if got := Lookup(r.ID); len(got) == 0 {
			t.Errorf("missing wara mapping for %s", r.ID)
		}
	}
}

// Every mapped rule must be a reliability rule with basis wara, every ID must be
// a full GUID with a pgVerified value, and every ID must appear in one of the
// rule's own APRL source notes with the same source URL.
func TestReferencesAreWellFormedAndSourced(t *testing.T) {
	rules := loadRules(t)
	for id, refs := range Map() {
		r, ok := rules[id]
		if !ok {
			t.Errorf("%s not in catalogue", id)
			continue
		}
		if r.Pillar != "reliability" || !slices.Contains(r.Basis, "wara") {
			t.Errorf("%s: WARA references need pillar reliability and basis wara (pillar=%s basis=%v)", id, r.Pillar, r.Basis)
		}
		for _, ref := range refs {
			if !guid.MatchString(ref.ID) {
				t.Errorf("%s: %q is not a full APRL GUID", id, ref.ID)
			}
			switch ref.PGVerified {
			case "true", "false", "unrecorded":
			default:
				t.Errorf("%s: %s pgVerified=%q", id, ref.ID, ref.PGVerified)
			}
			found := slices.ContainsFunc(r.Sources, func(s catalog.Source) bool {
				return strings.TrimSpace(s.URL) == ref.SourceURL && strings.Contains(s.Note, ref.ID)
			})
			if !found {
				t.Errorf("%s: %s not found in an APRL source note at %s", id, ref.ID, ref.SourceURL)
			}
		}
	}
}
