// Package norm holds the deterministic helpers shared by the four reporters: stable ordering,
// text sanitising, path normalisation, policy flattening with redaction, and the owner summary.
// Nothing here reads the clock, the environment or the terminal.
package norm

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// NoFile is the group label for findings that carry no file.
const NoFile = "(no file)"

// ProfileKeyMissingPrefix starts the skip reason of a rule whose profile key is absent (ADR-007).
const ProfileKeyMissingPrefix = "profile-key-missing:"

// FilePath returns a project-relative, forward-slash path, or "" when the input is empty.
// An absolute path (which reports must never carry) is reduced to its base name.
func FilePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') {
		b := path.Base(p[strings.LastIndex(p, ":")+1:])
		if b == "/" || b == "." || b == ":" {
			return ""
		}
		return b
	}
	c := path.Clean(p)
	if c == "." {
		return ""
	}
	return c
}

// Text removes terminal escape sequences and control characters, and replaces invalid UTF-8.
// Newlines and tabs become single spaces when oneLine is true; otherwise newlines are kept
// (CRLF and CR are folded to LF) and tabs become spaces. Unicode format characters
// (for example bidirectional overrides) are dropped.
func Text(s string, oneLine bool) string {
	s = stripEscapes(strings.ToValidUTF8(s, "�"))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\r':
			continue
		case r == '\n':
			if oneLine {
				b.WriteByte(' ')
			} else {
				b.WriteByte('\n')
			}
		case r == '\t', r == ' ', r == ' ':
			b.WriteByte(' ')
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// dropped
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if oneLine {
		out = strings.Join(strings.Fields(out), " ")
	}
	return strings.TrimSpace(out)
}

// stripEscapes removes CSI, OSC and other ESC-introduced sequences, and the 8-bit C1 CSI/OSC forms.
func stripEscapes(s string) string {
	if !strings.ContainsAny(s, "\x1b\u009b\u009d") {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case 0x1b:
			if i+1 >= len(rs) {
				continue
			}
			switch rs[i+1] {
			case '[':
				i = skipCSI(rs, i+2)
			case ']', 'P', '_', '^', 'X':
				i = skipString(rs, i+2)
			default:
				i++ // two-character escape
			}
		case 0x9b:
			i = skipCSI(rs, i+1)
		case 0x9d:
			i = skipString(rs, i+1)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipCSI returns the index of the final byte of a CSI sequence whose parameters start at i.
func skipCSI(rs []rune, i int) int {
	for i < len(rs) {
		if rs[i] >= 0x40 && rs[i] <= 0x7e {
			return i
		}
		i++
	}
	return i
}

// skipString returns the index of the terminator (BEL or ST) of an OSC-style string.
func skipString(rs []rune, i int) int {
	for i < len(rs) {
		if rs[i] == 0x07 || rs[i] == 0x9c {
			return i
		}
		if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '\\' {
			return i + 1
		}
		i++
	}
	return i
}

// TruncateUTF16 shortens s to at most n UTF-16 code units without splitting a character.
func TruncateUTF16(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// TruncateRunes shortens s to at most n runes and appends "..." when it cut.
func TruncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}

// Active reports whether a finding counts: neither suppressed nor baselined.
func Active(f sdk.Finding) bool { return f.Suppressed == nil && !f.Baselined }

// Normalised returns a copy of r with non-nil slices and maps, a schema version, redacted policy,
// and findings, skipped checks and tools in canonical order. The input is never modified.
func Normalised(r *sdk.Report) sdk.Report {
	var out sdk.Report
	if r != nil {
		out = *r
	}
	if out.SchemaVersion == "" {
		out.SchemaVersion = sdk.ReportSchemaVersion
	}
	out.Findings = SortedFindings(out.Findings)
	out.Skipped = SortedSkipped(out.Skipped)
	out.Tools = SortedTools(out.Tools)
	out.EffectivePolicy = RedactPolicy(out.EffectivePolicy)
	by := make(map[string]int, len(out.Summary.SkippedByReason))
	for k, v := range out.Summary.SkippedByReason {
		by[k] = v
	}
	out.Summary.SkippedByReason = by
	for i := range out.Findings {
		f := &out.Findings[i]
		f.Location.File = FilePath(f.Location.File)
		f.Basis = slices.Clone(f.Basis)
	}
	return out
}

// SortedFindings returns a sorted copy in the canonical order.
func SortedFindings(in []sdk.Finding) []sdk.Finding {
	out := make([]sdk.Finding, len(in))
	copy(out, in)
	slices.SortStableFunc(out, CompareFindings)
	return out
}

// CompareFindings is the canonical finding order: file (empty last), severity (most severe
// first), line, column, rule, key, resource, fingerprint, then every remaining field, so that
// the order is total for any distinguishable input.
func CompareFindings(a, b sdk.Finding) int {
	fa, fb := FilePath(a.Location.File), FilePath(b.Location.File)
	if (fa == "") != (fb == "") {
		if fa == "" {
			return 1
		}
		return -1
	}
	return cmp.Or(
		cmp.Compare(fa, fb),
		cmp.Compare(b.Severity.Rank(), a.Severity.Rank()),
		cmp.Compare(a.Location.Line, b.Location.Line),
		cmp.Compare(a.Location.Column, b.Location.Column),
		cmp.Compare(a.RuleID, b.RuleID),
		cmp.Compare(a.Key, b.Key),
		cmp.Compare(a.Resource.Kind, b.Resource.Kind),
		cmp.Compare(a.Resource.Type, b.Resource.Type),
		cmp.Compare(a.Resource.Name, b.Resource.Name),
		cmp.Compare(a.Resource.Pointer, b.Resource.Pointer),
		cmp.Compare(a.Fingerprint, b.Fingerprint),
		cmp.Compare(a.Profile, b.Profile),
		cmp.Compare(a.Evidence, b.Evidence),
		cmp.Compare(a.Recommendation, b.Recommendation),
		cmp.Compare(a.Location.EndLine, b.Location.EndLine),
		cmp.Compare(a.Location.EndColumn, b.Location.EndColumn),
		cmp.Compare(a.Location.Pointer, b.Location.Pointer),
		cmp.Compare(a.Resource.ID, b.Resource.ID),
		cmp.Compare(a.Adapter, b.Adapter),
		cmp.Compare(boolInt(a.Baselined), boolInt(b.Baselined)),
		cmp.Compare(suppressionKey(a), suppressionKey(b)),
		cmp.Compare(string(a.Confidence), string(b.Confidence)),
		cmp.Compare(a.Fix, b.Fix),
		cmp.Compare(strings.Join(a.Basis, ","), strings.Join(b.Basis, ",")),
	)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func suppressionKey(f sdk.Finding) string {
	if f.Suppressed == nil {
		return ""
	}
	return f.Suppressed.Reason + "\x00" + f.Suppressed.Expires + "\x00" + f.Suppressed.Owner + "\x00" + f.Suppressed.Source
}

// SortedSkipped returns a sorted copy of the skipped checks.
func SortedSkipped(in []sdk.SkippedCheck) []sdk.SkippedCheck {
	out := make([]sdk.SkippedCheck, len(in))
	copy(out, in)
	slices.SortStableFunc(out, func(a, b sdk.SkippedCheck) int {
		return cmp.Or(
			cmp.Compare(a.RuleID, b.RuleID),
			cmp.Compare(a.Reason, b.Reason),
			cmp.Compare(a.Resource.Kind, b.Resource.Kind),
			cmp.Compare(a.Resource.Type, b.Resource.Type),
			cmp.Compare(a.Resource.Name, b.Resource.Name),
			cmp.Compare(a.MissingCapability, b.MissingCapability),
			cmp.Compare(a.Detail, b.Detail),
			cmp.Compare(a.RuleVersion, b.RuleVersion),
			cmp.Compare(a.Resource.ID, b.Resource.ID),
			cmp.Compare(a.Resource.Pointer, b.Resource.Pointer),
		)
	})
	return out
}

// SortedTools returns a sorted, non-nil copy of the tool statuses.
func SortedTools(in []sdk.ToolStatus) []sdk.ToolStatus {
	out := make([]sdk.ToolStatus, len(in))
	copy(out, in)
	slices.SortStableFunc(out, func(a, b sdk.ToolStatus) int {
		return cmp.Or(
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Version, b.Version),
			cmp.Compare(string(a.State), string(b.State)),
			cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Detail, b.Detail),
			cmp.Compare(boolInt(a.Required), boolInt(b.Required)),
		)
	})
	return out
}

// Redacted replaces the value of a policy key whose name suggests a credential.
const Redacted = "[redacted]"

var sensitiveParts = []string{
	"secret", "token", "password", "passwd", "apikey", "api-key", "api_key",
	"connectionstring", "connection-string", "connection_string", "credential",
	"privatekey", "private-key", "private_key", "accountkey", "authorization",
}

// SensitiveKey reports whether a policy key name suggests a credential.
func SensitiveKey(k string) bool {
	l := strings.ToLower(k)
	for _, p := range sensitiveParts {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// RedactPolicy returns a non-nil deep copy of m in which the values under credential-like keys
// are replaced by Redacted. Reporters print policy values; this keeps a mis-configured secret
// out of every format.
func RedactPolicy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if SensitiveKey(k) {
			out[k] = Redacted
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return RedactPolicy(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = redactValue(t[i])
		}
		return out
	case []string:
		return slices.Clone(t)
	case map[string]string:
		m := make(map[string]any, len(t))
		for k, s := range t {
			m[k] = s
		}
		return RedactPolicy(m)
	}
	return v
}

// KV is one flattened policy entry.
type KV struct{ Key, Value string }

// PolicyLines flattens the effective policy into sorted dotted-path entries with sanitised,
// redacted, single-line values.
func PolicyLines(m map[string]any) []KV {
	var out []KV
	flatten("", RedactPolicy(m), &out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func flatten(prefix string, m map[string]any, out *[]KV) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := Text(k, true)
		if prefix != "" {
			p = prefix + "." + p
		}
		if t, ok := m[k].(map[string]any); ok && len(t) > 0 {
			flatten(p, t, out)
			continue
		}
		*out = append(*out, KV{p, scalar(m[k])})
	}
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return Text(t, true)
	case []any:
		parts := make([]string, len(t))
		for i := range t {
			parts[i] = scalar(t[i])
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		parts := make([]string, len(t))
		for i := range t {
			parts[i] = Text(t[i], true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		if len(t) == 0 {
			return "{}"
		}
		var kv []KV
		flatten("", t, &kv)
		parts := make([]string, len(kv))
		for i, e := range kv {
			parts[i] = e.Key + "=" + e.Value
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return Text(fmt.Sprint(v), true)
}

// RuleCount is a rule with its number of active findings.
type RuleCount struct {
	RuleID   string
	Count    int
	Severity sdk.Severity // highest severity seen
}

// TopRules returns up to n rules by active finding count (ties: severity, then rule ID).
func TopRules(fs []sdk.Finding, n int) []RuleCount {
	idx := map[string]*RuleCount{}
	for _, f := range fs {
		if !Active(f) {
			continue
		}
		rc := idx[f.RuleID]
		if rc == nil {
			rc = &RuleCount{RuleID: f.RuleID}
			idx[f.RuleID] = rc
		}
		rc.Count++
		if f.Severity.Rank() > rc.Severity.Rank() {
			rc.Severity = f.Severity
		}
	}
	out := make([]RuleCount, 0, len(idx))
	for _, rc := range idx {
		out = append(out, *rc)
	}
	slices.SortFunc(out, func(a, b RuleCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(b.Severity.Rank(), a.Severity.Rank()), cmp.Compare(a.RuleID, b.RuleID))
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// MissingKey is a profile key that made one or more rules skip.
type MissingKey struct {
	Key   string
	Rules []string
}

// MissingProfileKeys groups skipped checks with reason profile-key-missing:<key> by key.
func MissingProfileKeys(sk []sdk.SkippedCheck) []MissingKey {
	idx := map[string][]string{}
	for _, s := range sk {
		if k, ok := strings.CutPrefix(s.Reason, ProfileKeyMissingPrefix); ok {
			k = Text(k, true)
			if !slices.Contains(idx[k], s.RuleID) {
				idx[k] = append(idx[k], s.RuleID)
			}
		}
	}
	keys := make([]string, 0, len(idx))
	for k := range idx {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]MissingKey, 0, len(keys))
	for _, k := range keys {
		r := idx[k]
		sort.Strings(r)
		out = append(out, MissingKey{Key: k, Rules: r})
	}
	return out
}

// SkippedStatement is the sentence the human-readable formats print whenever anything was skipped.
const SkippedStatement = "Skipped is not passed: a skipped check was not evaluated and says nothing about the project."

// SkippedCount is the number of skipped checks to display: the larger of the summary figure and the list.
func SkippedCount(r *sdk.Report) int { return max(r.Summary.Skipped, len(r.Skipped)) }

// Position formats "line:col", "line", or the JSON pointer when the line is unknown.
func Position(l sdk.Location) string {
	if l.Line <= 0 {
		return Text(l.Pointer, true)
	}
	if l.Column > 0 {
		return fmt.Sprintf("%d:%d", l.Line, l.Column)
	}
	return fmt.Sprint(l.Line)
}

// ResourceLabel returns "type name pointer" style text for a resource, or "".
func ResourceLabel(r sdk.ResourceRef) string {
	var parts []string
	for _, p := range []string{r.Type, r.Name, r.Pointer} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return Text(strings.Join(parts, " "), true)
}

// Group is the findings of one file.
type Group struct {
	File     string
	Findings []sdk.Finding
}

// GroupByFile groups findings that are already in canonical order. Findings without a file are
// labelled NoFile and come last.
func GroupByFile(fs []sdk.Finding) []Group {
	var out []Group
	for _, f := range fs {
		name := FilePath(f.Location.File)
		if name == "" {
			name = NoFile
		}
		if n := len(out); n > 0 && out[n-1].File == name {
			out[n-1].Findings = append(out[n-1].Findings, f)
			continue
		}
		out = append(out, Group{File: name, Findings: []sdk.Finding{f}})
	}
	return out
}
