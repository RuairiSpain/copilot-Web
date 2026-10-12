package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/llm"
)

var prohibitedClaim = regexp.MustCompile(`(?i)\b(no findings|zero findings|all controls pass|all rules pass|exit code|suppressed|baselined|severity|priority|priorit(?:y|ize|ized|isation|ization)|reprioriti[sz]e|urgent|criticality|downgrad(?:e|ed)|upgrad(?:e|ed)|escalat(?:e|ed|ion))\b`)

// Options scopes response validation to the deterministic data shared with the
// provider.
type Options struct {
	AllowedRuleIDs map[string]struct{}
	AllowedActions map[string]struct{}
}

// Narrative parses and validates a provider response against the deterministic
// contract. Only the returned llm.Narrative may be rendered.
func Narrative(raw string, opt Options) (llm.Narrative, error) {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	var out llm.Narrative
	if err := dec.Decode(&out); err != nil {
		return llm.Narrative{}, fmt.Errorf("%w: response is not valid structured JSON", llm.ErrRejected)
	}
	if out.Summary = sanitize(out.Summary); out.Summary == "" {
		return llm.Narrative{}, fmt.Errorf("%w: response summary is empty", llm.ErrRejected)
	}
	if prohibitedClaim.MatchString(out.Summary) {
		return llm.Narrative{}, fmt.Errorf("%w: summary made a prohibited claim", llm.ErrRejected)
	}
	if len(out.Sections) == 0 || len(out.Sections) > 6 {
		return llm.Narrative{}, fmt.Errorf("%w: response must contain 1-6 sections", llm.ErrRejected)
	}
	for i := range out.Sections {
		out.Sections[i].Heading = sanitize(out.Sections[i].Heading)
		out.Sections[i].Summary = sanitize(out.Sections[i].Summary)
		if out.Sections[i].Heading == "" || out.Sections[i].Summary == "" {
			return llm.Narrative{}, fmt.Errorf("%w: section headings and summaries are required", llm.ErrRejected)
		}
		if prohibitedClaim.MatchString(out.Sections[i].Heading) {
			return llm.Narrative{}, fmt.Errorf("%w: section heading made a prohibited claim", llm.ErrRejected)
		}
		if prohibitedClaim.MatchString(out.Sections[i].Summary) {
			return llm.Narrative{}, fmt.Errorf("%w: section summary made a prohibited claim", llm.ErrRejected)
		}
		if len(out.Sections[i].Actions) > 5 || len(out.Sections[i].RuleIDs) > 5 {
			return llm.Narrative{}, fmt.Errorf("%w: sections may contain at most five actions and five rule ids", llm.ErrRejected)
		}
		out.Sections[i].Actions = cleanList(out.Sections[i].Actions)
		if err := validateActions(out.Sections[i].Actions, opt.AllowedActions); err != nil {
			return llm.Narrative{}, err
		}
		out.Sections[i].RuleIDs = cleanRules(out.Sections[i].RuleIDs, opt.AllowedRuleIDs)
		if len(out.Sections[i].RuleIDs) == 0 {
			return llm.Narrative{}, fmt.Errorf("%w: every section must reference at least one known rule id", llm.ErrRejected)
		}
	}
	if len(opt.AllowedRuleIDs) != 0 {
		covered := map[string]bool{}
		for _, section := range out.Sections {
			for _, id := range section.RuleIDs {
				covered[id] = true
			}
		}
		for id := range opt.AllowedRuleIDs {
			if !covered[id] {
				return llm.Narrative{}, fmt.Errorf("%w: response omitted rule %s", llm.ErrRejected, id)
			}
		}
	}
	return out, nil
}

func sanitize(s string) string {
	s = cleanText(s)
	if len(s) > 600 {
		s = strings.TrimSpace(s[:597]) + "..."
	}
	return s
}

func cleanText(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsControl(r) || (r >= '\u2028' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069') || r == utf8.RuneError {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = sanitize(item)
		if item == "" {
			continue
		}
		if prohibitedClaim.MatchString(item) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cleanRules(in []string, allowed map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if _, ok := allowed[id]; !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func validateActions(actions []string, allowed map[string]struct{}) error {
	for _, action := range actions {
		if len(allowed) == 0 {
			return fmt.Errorf("%w: response included unsupported action text", llm.ErrRejected)
		}
		if _, ok := allowed[action]; !ok {
			return fmt.Errorf("%w: response action was not present in deterministic recommendations", llm.ErrRejected)
		}
	}
	return nil
}

// MarshalCanonical is used by prompt snapshot tests.
func MarshalCanonical(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), nil
}
