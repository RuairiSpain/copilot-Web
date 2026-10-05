// Package sarif renders a report as SARIF 2.1.0 for GitHub code scanning. The structs below are
// hand-written and cover only the fields emitted (ADR-011 decision 4). The properties GitHub
// requires are listed in docs/development/phase-1-tooling-facts.md.
package sarif

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/norm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

const (
	// SchemaURI is the SARIF 2.1.0 JSON schema location.
	SchemaURI = "https://json.schemastore.org/sarif-2.1.0.json"
	// Version is the only SARIF version GitHub accepts.
	Version = "2.1.0"
	// DefaultMaxResults is GitHub's limit of results per run.
	DefaultMaxResults = 25000
	// DefaultInformationURI is the tool's home; the module path of this repository.
	DefaultInformationURI = "https://github.com/ruairispain/copilot-web/tree/main/foundry-doctor"
	// MaxDescription is GitHub's limit for short and full descriptions (characters).
	MaxDescription = 1024
	// MaxName is GitHub's limit for a rule name.
	MaxName = 255
	// MaxTags is the number of rule tags GitHub keeps.
	MaxTags = 10
	// SrcRoot is the uriBaseId used for every artifact location.
	SrcRoot = "%SRCROOT%"
	// FingerprintKey is the partial fingerprint that carries the Foundry Doctor fingerprint (ADR-008).
	FingerprintKey = "foundryDoctor/v1"
	// LineHashKey is the only partial fingerprint GitHub uses.
	LineHashKey = "primaryLocationLineHash"
)

// RuleInfo is optional catalogue text for one rule. Without it the rule text is derived from the
// first finding of the rule.
type RuleInfo struct {
	Title       string
	Description string
	Help        string
	HelpURI     string
}

// Options configure the SARIF reporter.
type Options struct {
	// MaxResults caps results per run; zero means DefaultMaxResults.
	MaxResults int
	// InformationURI overrides DefaultInformationURI.
	InformationURI string
	// Rules supplies catalogue text keyed by rule ID.
	Rules map[string]RuleInfo
}

// Reporter is the SARIF sdk.Reporter.
type Reporter struct{ opts Options }

var _ sdk.Reporter = Reporter{}

// New returns a SARIF reporter.
func New(opts Options) Reporter { return Reporter{opts: opts} }

// Format returns "sarif".
func (Reporter) Format() string { return "sarif" }

// Log is the SARIF root object.
type Log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is one tool execution.
type Run struct {
	Tool               Tool                        `json:"tool"`
	OriginalUriBaseIDs map[string]ArtifactLocation `json:"originalUriBaseIds"`
	Invocations        []Invocation                `json:"invocations"`
	Results            []Result                    `json:"results"`
	Properties         map[string]any              `json:"properties,omitempty"`
}

// Tool wraps the driver.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver describes Foundry Doctor and its rules.
type Driver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri"`
	Rules          []Rule `json:"rules"`
}

// Text is a SARIF message string.
type Text struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}

// Rule is a reportingDescriptor.
type Rule struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	ShortDescription     Text           `json:"shortDescription"`
	FullDescription      Text           `json:"fullDescription"`
	Help                 Text           `json:"help"`
	HelpURI              string         `json:"helpUri,omitempty"`
	DefaultConfiguration RuleConfig     `json:"defaultConfiguration"`
	Properties           RuleProperties `json:"properties"`
}

// RuleConfig holds the default level.
type RuleConfig struct {
	Level string `json:"level"`
}

// RuleProperties holds the tags.
type RuleProperties struct {
	Tags []string `json:"tags"`
}

// Invocation records how the run ended and what it skipped.
type Invocation struct {
	ExecutionSuccessful        bool           `json:"executionSuccessful"`
	ExitCode                   int            `json:"exitCode"`
	ToolExecutionNotifications []Notification `json:"toolExecutionNotifications"`
}

// Notification is a toolExecutionNotification.
type Notification struct {
	Level          string         `json:"level"`
	Message        Text           `json:"message"`
	Descriptor     *Reference     `json:"descriptor,omitempty"`
	AssociatedRule *Reference     `json:"associatedRule,omitempty"`
	Properties     map[string]any `json:"properties,omitempty"`
}

// Reference points at a rule by ID.
type Reference struct {
	ID string `json:"id"`
}

// Result is one finding.
type Result struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             Text              `json:"message"`
	Locations           []Location        `json:"locations"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	Suppressions        []Suppression     `json:"suppressions,omitempty"`
	Properties          map[string]any    `json:"properties"`
}

// Location is a result location.
type Location struct {
	PhysicalLocation PhysicalLocation  `json:"physicalLocation"`
	LogicalLocations []LogicalLocation `json:"logicalLocations,omitempty"`
}

// PhysicalLocation is a file and region.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           Region           `json:"region"`
}

// ArtifactLocation is a relative URI under a base ID.
type ArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

// Region is a full region; GitHub requires all four positions.
type Region struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

// LogicalLocation names the resource a result is about.
type LogicalLocation struct {
	Name               string `json:"name,omitempty"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
	Kind               string `json:"kind,omitempty"`
}

// Suppression marks a suppressed or baselined result.
type Suppression struct {
	Kind          string         `json:"kind"`
	Status        string         `json:"status"`
	Justification string         `json:"justification,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
}

// Level maps a severity to a SARIF level.
func Level(s sdk.Severity) string {
	switch s {
	case sdk.SeverityError:
		return "error"
	case sdk.SeverityInfo:
		return "note"
	}
	return "warning"
}

// Build converts a report into a SARIF log.
func (c Reporter) Build(r *sdk.Report) Log {
	rep := norm.Normalised(r)
	max := c.opts.MaxResults
	if max <= 0 {
		max = DefaultMaxResults
	}
	info := c.opts.InformationURI
	if info == "" {
		info = DefaultInformationURI
	}
	name := norm.Text(rep.Tool.Name, true)
	if name == "" {
		name = "foundry-doctor"
	}

	findings, omitted := capFindings(rep.Findings, max)
	rules, index := c.buildRules(findings)

	counts := map[string]int{}
	results := make([]Result, 0, len(findings))
	for _, f := range findings {
		res := buildResult(f, index[f.RuleID])
		h := res.PartialFingerprints[LineHashKey]
		counts[h]++
		res.PartialFingerprints[LineHashKey] = fmt.Sprintf("%s:%d", h, counts[h])
		results = append(results, res)
	}

	notes := []Notification{}
	for _, s := range rep.Skipped {
		text := "Skipped " + norm.Text(s.RuleID, true) + ": " + norm.Text(s.Reason, true)
		if s.MissingCapability != "" {
			text += " (missing capability: " + norm.Text(s.MissingCapability, true) + ")"
		}
		text += ". Skipped is not passed."
		n := Notification{
			Level:   "note",
			Message: Text{Text: text},
			Properties: map[string]any{
				"reason": norm.Text(s.Reason, true), "skipped": true,
			},
		}
		if s.RuleID != "" {
			n.Descriptor = &Reference{ID: norm.Text(s.RuleID, true)}
			n.AssociatedRule = &Reference{ID: norm.Text(s.RuleID, true)}
		}
		if s.MissingCapability != "" {
			n.Properties["missingCapability"] = norm.Text(s.MissingCapability, true)
		}
		notes = append(notes, n)
	}
	if omitted > 0 {
		notes = append(notes, Notification{
			Level: "note",
			Message: Text{Text: fmt.Sprintf("Results truncated: %d of %d results omitted to stay within the limit of %d per run; the most severe active results were kept.",
				omitted, len(rep.Findings), max)},
			Properties: map[string]any{"truncated": true, "omitted": omitted},
		})
	}

	run := Run{
		Tool: Tool{Driver: Driver{Name: name, Version: norm.Text(rep.Tool.Version, true), InformationURI: info, Rules: rules}},
		OriginalUriBaseIDs: map[string]ArtifactLocation{
			SrcRoot: {URI: "./"},
		},
		Invocations: []Invocation{{
			ExecutionSuccessful:        rep.ExitCode != sdk.ExitCannotRun && rep.ExitCode != sdk.ExitInternal,
			ExitCode:                   rep.ExitCode,
			ToolExecutionNotifications: notes,
		}},
		Results:    results,
		Properties: map[string]any{"profile": norm.Text(rep.Profile, true), "hidden": rep.Summary.Hidden},
	}
	return Log{Schema: SchemaURI, Version: Version, Runs: []Run{run}}
}

// Write renders r to w as indented SARIF.
func (c Reporter) Write(w io.Writer, r *sdk.Report) error {
	var buf bytes.Buffer
	enc := stdjson.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c.Build(r)); err != nil {
		return fmt.Errorf("encode sarif: %w", err)
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

// capFindings keeps at most max findings, preferring active over suppressed or baselined and
// higher over lower severity, and returns them in canonical order with the number omitted.
func capFindings(in []sdk.Finding, max int) ([]sdk.Finding, int) {
	if len(in) <= max {
		return in, 0
	}
	idx := make([]int, len(in))
	for i := range idx {
		idx[i] = i
	}
	// in is already in canonical order, so the index is a total tie-break.
	sort.SliceStable(idx, func(a, b int) bool {
		fa, fb := in[idx[a]], in[idx[b]]
		if norm.Active(fa) != norm.Active(fb) {
			return norm.Active(fa)
		}
		return fa.Severity.Rank() > fb.Severity.Rank()
	})
	keep := idx[:max]
	slices.Sort(keep)
	out := make([]sdk.Finding, 0, max)
	for _, i := range keep {
		out = append(out, in[i])
	}
	return out, len(in) - max
}

func (c Reporter) buildRules(fs []sdk.Finding) ([]Rule, map[string]int) {
	byID := map[string][]sdk.Finding{}
	for _, f := range fs {
		byID[f.RuleID] = append(byID[f.RuleID], f)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := make([]Rule, 0, len(ids))
	index := make(map[string]int, len(ids))
	for i, id := range ids {
		index[id] = i
		rules = append(rules, c.buildRule(id, byID[id]))
	}
	return rules, index
}

func (c Reporter) buildRule(id string, fs []sdk.Finding) Rule {
	cleanID := norm.Text(id, true)
	level := sdk.Severity("")
	tagSet := map[string]bool{"foundry-doctor": true}
	var rec, fix, docs string
	for _, f := range fs {
		if f.Severity.Rank() > level.Rank() {
			level = f.Severity
		}
		if rec == "" {
			rec = norm.Text(f.Recommendation, true)
		}
		if fix == "" {
			fix = norm.Text(f.Fix, true)
		}
		if docs == "" {
			docs = httpsURL(f.DocsURL)
		}
		for _, t := range append([]string{f.Pillar, string(f.Category)}, f.Basis...) {
			if t = norm.Text(t, true); t != "" {
				tagSet[t] = true
			}
		}
	}
	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	if len(tags) > MaxTags {
		tags = tags[:MaxTags]
	}

	meta := c.opts.Rules[id]
	short := norm.Text(meta.Title, true)
	if short == "" {
		short = cleanID
	}
	full := norm.Text(meta.Description, true)
	if full == "" {
		full = cleanID
		if rec != "" {
			full += ": " + rec
		}
	}
	helpText := norm.Text(meta.Help, true)
	if helpText == "" {
		helpText = rec
		if helpText == "" {
			helpText = full
		}
		if fix != "" {
			helpText += " Fix: " + fix
		}
	}
	if u := httpsURL(meta.HelpURI); u != "" {
		docs = u
	}
	helpMD := escapeMD(helpText)
	if docs != "" {
		helpText += " See " + docs
		helpMD += "\n\n[Documentation](" + docs + ")"
	}
	return Rule{
		ID:                   cleanID,
		Name:                 norm.TruncateUTF16(cleanID, MaxName),
		ShortDescription:     Text{Text: norm.TruncateUTF16(short, MaxDescription)},
		FullDescription:      Text{Text: norm.TruncateUTF16(full, MaxDescription)},
		Help:                 Text{Text: helpText, Markdown: helpMD},
		HelpURI:              docs,
		DefaultConfiguration: RuleConfig{Level: Level(level)},
		Properties:           RuleProperties{Tags: tags},
	}
}

var mdEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "@", "&#64;", "\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "|", "\\|", "#", "\\#")

func escapeMD(s string) string { return mdEscaper.Replace(s) }

// httpsURL returns raw when it is an https URL without characters that could break a link, else "".
func httpsURL(raw string) string {
	raw = norm.Text(raw, true)
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Scheme != "https" || u.Host == "" || strings.ContainsAny(raw, " ()<>`[]\\|") {
		return ""
	}
	return u.String()
}

func buildResult(f sdk.Finding, ruleIndex int) Result {
	msg := norm.Text(f.Evidence, false)
	if msg == "" {
		msg = norm.Text(f.Recommendation, false)
	}
	if msg == "" {
		msg = norm.Text(f.RuleID, true)
	}
	if msg == "" {
		msg = "Finding"
	}
	basis := append([]string{}, f.Basis...)
	for i := range basis {
		basis[i] = norm.Text(basis[i], true)
	}
	props := map[string]any{
		"confidence":  norm.Text(string(f.Confidence), true),
		"basis":       basis,
		"profile":     norm.Text(f.Profile, true),
		"severity":    norm.Text(string(f.Severity), true),
		"ruleVersion": f.RuleVersion,
	}
	if f.Adapter != "" {
		props["adapter"] = norm.Text(f.Adapter, true)
	}
	loc := Location{PhysicalLocation: PhysicalLocation{
		ArtifactLocation: ArtifactLocation{URI: artifactURI(f.Location.File), URIBaseID: SrcRoot},
		Region:           region(f.Location),
	}}
	if f.Resource.Name != "" || f.Resource.Pointer != "" {
		fq := strings.Trim(norm.Text(f.Resource.Type+"/"+f.Resource.Name, true), "/")
		if f.Resource.Pointer != "" {
			fq += norm.Text(f.Resource.Pointer, true)
		}
		kind := norm.Text(f.Resource.Kind, true)
		loc.LogicalLocations = []LogicalLocation{{Name: norm.Text(f.Resource.Name, true), FullyQualifiedName: fq, Kind: kind}}
	}
	res := Result{
		RuleID:    norm.Text(f.RuleID, true),
		RuleIndex: ruleIndex,
		Level:     Level(f.Severity),
		Message:   Text{Text: msg},
		Locations: []Location{loc},
		PartialFingerprints: map[string]string{
			LineHashKey:    lineHash(f),
			FingerprintKey: fingerprint(f),
		},
		Properties: props,
	}
	if f.Suppressed != nil {
		res.Suppressions = append(res.Suppressions, Suppression{
			Kind: "external", Status: "accepted", Justification: norm.Text(f.Suppressed.Reason, true),
			Properties: map[string]any{
				"source": "suppression", "expires": norm.Text(f.Suppressed.Expires, true),
				"owner": norm.Text(f.Suppressed.Owner, true), "file": norm.FilePath(f.Suppressed.Source),
			},
		})
	}
	if f.Baselined {
		res.Suppressions = append(res.Suppressions, Suppression{
			Kind: "external", Status: "accepted", Justification: "Matches an entry in the baseline.",
			Properties: map[string]any{"source": "baseline"},
		})
	}
	return res
}

func artifactURI(file string) string {
	p := norm.FilePath(file)
	if p == "" {
		return "."
	}
	u := url.URL{Path: p}
	return u.EscapedPath()
}

func region(l sdk.Location) Region {
	sl, sc := max(l.Line, 1), max(l.Column, 1)
	el := l.EndLine
	if el < sl {
		el = sl
	}
	ec := l.EndColumn
	if ec < 1 {
		ec = 1
		if l.Line > 0 && el == sl {
			ec = sc + 1
		}
	}
	if el == sl && ec < sc {
		ec = sc
	}
	return Region{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
}

// fingerprint returns the Foundry Doctor fingerprint, or a derived stand-in when the engine left it empty.
func fingerprint(f sdk.Finding) string {
	if f.Fingerprint != "" {
		return norm.Text(f.Fingerprint, true)
	}
	return "derived:" + digest(f.RuleID, f.Resource.Type, f.Resource.Name, f.Key, f.Resource.Pointer, f.Location.Pointer, norm.FilePath(f.Location.File), f.Evidence)
}

// lineHash derives the 16-hex-digit hash half of primaryLocationLineHash from the fingerprint.
// GitHub requires the "hash:occurrence" shape; the caller appends the occurrence.
func lineHash(f sdk.Finding) string {
	return digest(fingerprint(f))[:16]
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
