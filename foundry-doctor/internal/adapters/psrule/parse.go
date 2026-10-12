package psrule

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/adapters"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// UnmappedPrefix is the rule namespace for PSRule rules with no overlap.psrule mapping.
const UnmappedPrefix = "psrule/"

const (
	maxResults    = 10000
	maxMessage    = 500
	maxJoined     = 5
	maxNameRunes  = 200
	sarifVersion  = "2.1.0"
	resourceKind  = "arm-resource"
	recommendFmt  = "Review PSRule for Azure rule %s for this resource."
	unmappedRecom = "Review PSRule for Azure rule %s; it has no Foundry Doctor catalogue mapping."
)

// ruleNameRE is the strict shape of a PSRule rule name (Azure.Storage.SoftDelete, or a policy GUID).
// Anything else is hostile or from an unknown format and fails the whole parse.
var ruleNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var workFileRE = regexp.MustCompile(`(?:^|/)input/(\d{4})(?:\.parameters)?\.json$`)

// SARIF 2.1.0 (OASIS) subset. Only standard fields are read. PSRule-specific properties are not used,
// because their shape is not verified (see docs/development/adapters.md).
type sarifLog struct {
	Version string      `json:"version"`
	Runs    *[]sarifRun `json:"runs"`
}

type sarifRun struct {
	Results []sarifResult `json:"results"`
}

type sarifResult struct {
	RuleID    *string         `json:"ruleId"`
	Level     string          `json:"level"`
	Kind      string          `json:"kind"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	Physical *struct {
		Artifact *struct {
			URI string `json:"uri"`
		} `json:"artifactLocation"`
	} `json:"physicalLocation"`
	Logical []struct {
		Name     string `json:"name"`
		FullName string `json:"fullyQualifiedName"`
	} `json:"logicalLocations"`
}

// Parse maps SARIF produced by PSRule over inputs to findings with Adapter "psrule". inputs must be the
// templates in the order they were written (input i is NNNN = i+1). m may be nil.
//
// A malformed document, an unexpected SARIF version, an invalid rule name or too many results is an
// error wrapping adapters.ErrProtocol (exit 4). The output is sorted and deterministic.
func Parse(data []byte, inputs []Input, m Mapper) ([]sdk.Finding, error) {
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("psrule: %w: not valid SARIF JSON", adapters.ErrProtocol)
	}
	if log.Version != sarifVersion {
		return nil, fmt.Errorf("psrule: %w: SARIF version %q, want %s", adapters.ErrProtocol, clean(log.Version), sarifVersion)
	}
	if log.Runs == nil {
		return nil, fmt.Errorf("psrule: %w: SARIF has no runs array", adapters.ErrProtocol)
	}
	idx := make([][]resEntry, len(inputs))
	total := 0
	for _, run := range *log.Runs {
		for _, r := range run.Results {
			total++
			if total > maxResults {
				return nil, fmt.Errorf("psrule: %w: more than %d results", adapters.ErrProtocol, maxResults)
			}
			if r.RuleID == nil || !ruleNameRE.MatchString(*r.RuleID) {
				return nil, fmt.Errorf("psrule: %w: result %d has a missing or invalid rule name", adapters.ErrProtocol, total)
			}
			switch r.Level {
			case "", "error", "warning", "note", "none":
			default:
				return nil, fmt.Errorf("psrule: %w: result %d has unknown level %q", adapters.ErrProtocol, total, clean(r.Level))
			}
			switch r.Kind {
			case "pass", "notApplicable":
				continue
			case "", "fail", "review", "open", "informational":
			default:
				return nil, fmt.Errorf("psrule: %w: result %d has unknown kind %q", adapters.ErrProtocol, total, clean(r.Kind))
			}
		}
	}
	return buildFindings(*log.Runs, inputs, idx, m)
}

// group is one finding under construction: the same rule on the same place, with distinct messages joined.
type group struct {
	rule     string
	level    string
	res      located
	messages map[string]bool
}

type located struct {
	file     string
	resource sdk.ResourceRef
	pointer  string
	key      string
}

func buildFindings(runs []sarifRun, inputs []Input, idx [][]resEntry, m Mapper) ([]sdk.Finding, error) {
	groups := map[string]*group{}
	var order []string
	for _, run := range runs {
		for _, r := range run.Results {
			if r.Kind == "pass" || r.Kind == "notApplicable" || r.Level == "none" {
				continue
			}
			res := locate(r, inputs, idx)
			k := res.key + "\x00" + *r.RuleID
			g := groups[k]
			if g == nil {
				g = &group{rule: *r.RuleID, level: r.Level, res: res, messages: map[string]bool{}}
				groups[k] = g
				order = append(order, k)
			}
			if rank(r.Level) > rank(g.level) {
				g.level = r.Level
			}
			if msg := cleanRedact(r.Message.Text); msg != "" {
				g.messages[msg] = true
			}
		}
	}
	var out []sdk.Finding
	for _, k := range order {
		g := groups[k]
		msgs := make([]string, 0, len(g.messages))
		for s := range g.messages {
			msgs = append(msgs, s)
		}
		sort.Strings(msgs)
		if len(msgs) > maxJoined {
			msgs = append(msgs[:maxJoined], "...")
		}
		detail := strings.Join(msgs, " | ")
		ids := mappedIDs(m, g.rule)
		for _, id := range ids {
			out = append(out, g.finding(id, detail, true))
		}
		if len(ids) == 0 {
			out = append(out, g.finding(UnmappedPrefix+g.rule, detail, false))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Resource.Name != b.Resource.Name {
			return a.Resource.Name < b.Resource.Name
		}
		if a.Location.Pointer != b.Location.Pointer {
			return a.Location.Pointer < b.Location.Pointer
		}
		if a.Location.File != b.Location.File {
			return a.Location.File < b.Location.File
		}
		return a.Key < b.Key
	})
	return out, nil
}

func (g *group) finding(ruleID, detail string, mapped bool) sdk.Finding {
	ev := "PSRule " + g.rule
	if detail != "" {
		ev += ": " + detail
	}
	f := sdk.Finding{
		RuleID:     ruleID,
		Adapter:    Name,
		Key:        g.rule,
		Resource:   g.res.resource,
		Location:   sdk.Location{File: g.res.file, Pointer: g.res.pointer},
		Evidence:   ev,
		Confidence: sdk.ConfidenceLikely,
	}
	if mapped {
		f.Recommendation = fmt.Sprintf(recommendFmt, g.rule)
		return f // the engine stamps severity, category and the rest from the catalogue rule
	}
	f.RuleVersion = 1
	f.Recommendation = fmt.Sprintf(unmappedRecom, g.rule)
	f.Category = sdk.CategoryNiceToHave
	switch g.level {
	case "error":
		f.Severity = sdk.SeverityError
	case "note":
		f.Severity = sdk.SeverityInfo
	default:
		f.Severity = sdk.SeverityWarning
	}
	return f
}

func rank(level string) int {
	switch level {
	case "error":
		return 3
	case "warning", "":
		return 2
	case "note":
		return 1
	}
	return 0
}

// mappedIDs returns the sorted, de-duplicated catalogue IDs for a PSRule rule, ignoring any that are not
// catalogue-shaped so a bad mapper cannot inject arbitrary rule IDs.
func mappedIDs(m Mapper, rule string) []string {
	if m == nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, id := range m.Lookup(Name, rule) {
		if strings.HasPrefix(id, "FND-") && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

type resEntry struct {
	typ, name, pointer string
}

// locate finds the input file and, when the template names exactly one matching resource, its type and
// ARM pointer. Unresolved results keep the logical name PSRule reported.
func locate(r sarifResult, inputs []Input, idx [][]resEntry) located {
	n := -1
	var names []string
	for _, l := range r.Locations {
		if l.Physical != nil && l.Physical.Artifact != nil && n < 0 {
			uri := strings.ReplaceAll(l.Physical.Artifact.URI, `\`, "/")
			if sm := workFileRE.FindStringSubmatch(uri); sm != nil {
				if v, err := strconv.Atoi(sm[1]); err == nil && v >= 1 && v <= len(inputs) {
					n = v - 1
				}
			}
		}
		for _, ll := range l.Logical {
			for _, s := range []string{ll.Name, ll.FullName} {
				if s = findings.Redact(clean(s)); s != "" {
					names = append(names, s)
				}
			}
		}
	}
	if n < 0 && len(inputs) == 1 {
		n = 0
	}
	out := located{}
	if n >= 0 {
		out.file = inputs[n].Name
		if idx[n] == nil {
			idx[n] = indexResources(inputs[n].Template)
		}
		for _, nm := range names {
			var hit []resEntry
			for _, e := range idx[n] {
				if e.name == nm {
					hit = append(hit, e)
				}
			}
			if len(hit) == 1 {
				out.resource = sdk.ResourceRef{Kind: resourceKind, Type: hit[0].typ, Name: hit[0].name, Pointer: hit[0].pointer}
				out.pointer = hit[0].pointer
				break
			}
		}
	}
	if out.resource.Name == "" && len(names) > 0 {
		out.resource = sdk.ResourceRef{Kind: resourceKind, Name: truncate(names[0], maxNameRunes)}
	}
	out.key = out.file + "\x00" + out.resource.Type + "\x00" + out.resource.Name + "\x00" + out.pointer
	return out
}

// indexResources lists the top-level resources of a compiled ARM template, in template order for the
// array form and in key order for the languageVersion 2.0 object form. It uses encoding/json only.
func indexResources(tpl []byte) []resEntry {
	var doc struct {
		Resources json.RawMessage `json:"resources"`
	}
	if json.Unmarshal(tpl, &doc) != nil || len(doc.Resources) == 0 {
		return []resEntry{}
	}
	type res struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	out := []resEntry{}
	var arr []res
	if json.Unmarshal(doc.Resources, &arr) == nil {
		for i, r := range arr {
			out = append(out, resEntry{r.Type, clean(r.Name), "/resources/" + strconv.Itoa(i)})
		}
		return out
	}
	var obj map[string]res
	if json.Unmarshal(doc.Resources, &obj) == nil {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			esc := strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
			out = append(out, resEntry{obj[k].Type, clean(obj[k].Name), "/resources/" + esc})
		}
	}
	return out
}

// clean makes untrusted text safe to place in a report: control and format characters (including bidi
// overrides) become spaces, angle brackets and backticks are replaced so the text cannot form markup,
// and runs of whitespace collapse.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '<':
			r = '‹'
		case r == '>':
			r = '›'
		case r == '`':
			r = '\''
		case r == unicode.ReplacementChar || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r):
			r = ' '
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// cleanRedact cleans, redacts secret-shaped text, then truncates, so a cut can never expose part of a secret.
func cleanRedact(s string) string {
	return truncate(findings.Redact(clean(s)), maxMessage)
}

func truncate(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}
