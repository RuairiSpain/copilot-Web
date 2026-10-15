// Package findings provides fingerprints, ordering and secret redaction for
// findings.
package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Fingerprint returns a stable identity for a finding. It deliberately omits
// line, column and evidence so unrelated edits do not invalidate baselines,
// and never includes evidence text (which may be sensitive).
func Fingerprint(f sdk.Finding) string {
	h := sha256.New()
	fmt.Fprintf(h, "v1\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s",
		f.RuleID, f.RuleVersion, f.Resource.Type, f.Resource.Name, f.Resource.ID, strings.ToLower(f.Location.File))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Sort orders findings deterministically: file, line, column, rule, resource.
func Sort(fs []sdk.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		switch {
		case a.Location.File != b.Location.File:
			return a.Location.File < b.Location.File
		case a.Location.Line != b.Location.Line:
			return a.Location.Line < b.Location.Line
		case a.Location.Column != b.Location.Column:
			return a.Location.Column < b.Location.Column
		case a.RuleID != b.RuleID:
			return a.RuleID < b.RuleID
		case a.Resource.Name != b.Resource.Name:
			return a.Resource.Name < b.Resource.Name
		}
		return a.Fingerprint < b.Fingerprint
	})
}

// Placeholder replaces redacted values.
const Placeholder = "[REDACTED]"

var (
	rePEM      = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	reKeyValue = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:password|passwd|pwd|secret|token|apikey|api[_-]key|accountkey|sharedaccesskey|sharedaccesssignature|connectionstring|access[_-]?key|authorization)[a-z0-9_.-]*["']?\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;"']+)`)
	reBearer   = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[a-z0-9._~+/=-]{8,}`)
	reSig      = regexp.MustCompile(`(?i)([?&]sig=)[^&\s"']+`)
	reUserInfo = regexp.MustCompile(`(://[^/\s:@]+:)[^/\s@]+(@)`)
	reJWT      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}\b`)
	reGitHub   = regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{16,}\b`)
	reStorage  = regexp.MustCompile(`[A-Za-z0-9+/]{86}==`)
)

// Redact removes secret-shaped content from s. It is idempotent and never
// panics for arbitrary input.
func Redact(s string) string {
	s = rePEM.ReplaceAllString(s, Placeholder)
	s = reBearer.ReplaceAllString(s, "${1} "+Placeholder)
	s = reKeyValue.ReplaceAllStringFunc(s, func(m string) string {
		sub := reKeyValue.FindStringSubmatch(m)
		if strings.HasPrefix(sub[2], Placeholder) {
			return m
		}
		return sub[1] + Placeholder
	})
	s = reSig.ReplaceAllString(s, "${1}"+Placeholder)
	s = reUserInfo.ReplaceAllString(s, "${1}"+Placeholder+"${2}")
	s = reJWT.ReplaceAllString(s, Placeholder)
	s = reGitHub.ReplaceAllString(s, Placeholder)
	s = reStorage.ReplaceAllString(s, Placeholder)
	return s
}

// RedactFinding returns f with all free-text fields redacted.
func RedactFinding(f sdk.Finding) sdk.Finding {
	f.Evidence = Redact(f.Evidence)
	f.Recommendation = Redact(f.Recommendation)
	f.Fix = Redact(f.Fix)
	f.Resource.Name = Redact(f.Resource.Name)
	f.Resource.ID = Redact(f.Resource.ID)
	return f
}
