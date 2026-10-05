package findings

import (
	"regexp"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// mask is the longest replacement. A span shorter than mask is replaced by that many '*', so
// redaction never expands its input.
const mask = "***"

// rule redacts capture group `group` (0 is the whole match) of every match of re.
type rule struct {
	re    *regexp.Regexp
	group int
}

// Redactor removes secret-shaped text. It is immutable after construction, so it is safe for
// concurrent use. Go's regexp is RE2 (linear time), so adversarial input cannot cause backtracking.
type Redactor struct {
	rules []rule
}

// secretValue is a value after a key-like name: a quoted string or a run without separators.
const secretValue = `(?:"[^"\n]*"|'[^'\n]*'|[^;\s,]+)`

// defaultRedactor is the shared policy instance behind Redact and Sanitize. It is immutable.
var defaultRedactor = NewRedactor()

// NewRedactor builds a Redactor with the policy pattern set: PEM private keys, JWTs, bearer and
// Authorization values, GitHub/OpenAI/AWS-style keys, Azure AD client secrets, storage-key-shaped
// base64, connection-string secrets (AccountKey, SharedAccessKey, SharedAccessSignature,
// Password), URL userinfo, SAS sig= values, and long base64 or hex values after key-like names.
func NewRedactor() *Redactor {
	defs := []struct {
		pattern string
		group   int
	}{
		{`-----BEGIN [A-Z ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----|[\s\S]*)`, 0},
		{`eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`, 0},
		{`(?i)\b(bearer\s+)([A-Za-z0-9._~+/=-]{8,})`, 2},
		{`(?i)(authorization["']?\s*[:=]\s*["']?(?:basic\s+|bearer\s+)?)([^\s,;"']{8,})`, 2},
		{`sk-[A-Za-z0-9_-]{16,}`, 0},
		{`gh[pousr]_[A-Za-z0-9]{20,}`, 0},
		{`github_pat_[A-Za-z0-9_]{20,}`, 0},
		{`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`, 0},
		{`[A-Za-z0-9_~.-]{3}8Q~[A-Za-z0-9_~.-]{20,}`, 0},
		{`[A-Za-z0-9+/]{86}==`, 0},
		{`(?i)(\b[A-Za-z0-9_.-]*(?:key|secret|token|passw(?:or)?d|pwd|credential)[A-Za-z0-9_.-]*["']?\s*[:=]\s*["']?)([A-Za-z0-9+/=_-]{16,})`, 2},
		{`(://)([^\s/@"']+)(@)`, 2},
		{`(?i)(\bsig=)([^&\s"';]+)`, 2},
		// Last: its value may span a line break, so it must not hide a named secret from the rules above.
		{`(?i)\b((?:AccountKey|SharedAccessKey|SharedAccessSignature|Password|Pwd|ClientSecret|SecretAccessKey|PrimaryKey|SecondaryKey)\s*=\s*)(` + secretValue + `)`, 2},
	}
	r := &Redactor{}
	for _, d := range defs {
		r.rules = append(r.rules, rule{re: regexp.MustCompile(d.pattern), group: d.group})
	}
	return r
}

// Redact returns s with secret-shaped spans masked. It is deterministic, idempotent and never
// longer than s. Because a replacement can expose a new match, rules run until the text is stable;
// each pass that changes the text lowers its non-mask byte count, so the loop terminates.
func (r *Redactor) Redact(s string) string {
	for {
		next := s
		for _, rl := range r.rules {
			next = rl.apply(next)
		}
		if next == s {
			return s
		}
		s = next
	}
}

func (rl rule) apply(s string) string {
	locs := rl.re.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range locs {
		start, end := loc[2*rl.group], loc[2*rl.group+1]
		if start < 0 || start < last {
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(maskFor(end - start))
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

// maskFor returns "***", or fewer asterisks for a shorter span, never more than n bytes.
func maskFor(n int) string {
	if n >= len(mask) {
		return mask
	}
	return mask[:n]
}

// Redact applies the default policy pattern set to s.
func Redact(s string) string { return defaultRedactor.Redact(s) }

// Sanitize returns a copy of f with every free-text field redacted: Evidence, Recommendation,
// Fix, Key, resource and location text and the suppression reason. Call it before Fingerprint
// so the fingerprint is computed over the same Key that is reported. f is not modified.
func Sanitize(f sdk.Finding) sdk.Finding { return defaultRedactor.Sanitize(f) }

// Sanitize is the package-level Sanitize using this Redactor.
func (r *Redactor) Sanitize(f sdk.Finding) sdk.Finding {
	f.Evidence = r.Redact(f.Evidence)
	f.Recommendation = r.Redact(f.Recommendation)
	f.Fix = r.Redact(f.Fix)
	f.Key = r.Redact(f.Key)
	f.Resource.Name = r.Redact(f.Resource.Name)
	f.Resource.ID = r.Redact(f.Resource.ID)
	f.Resource.Pointer = r.Redact(f.Resource.Pointer)
	f.Location.File = r.Redact(f.Location.File)
	f.Location.Pointer = r.Redact(f.Location.Pointer)
	if f.Suppressed != nil {
		s := *f.Suppressed
		s.Reason = r.Redact(s.Reason)
		f.Suppressed = &s
	}
	return f
}
