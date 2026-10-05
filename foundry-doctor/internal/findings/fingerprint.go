// Package findings holds the pure helpers the engine applies to rule output: fingerprints
// (ADR-008), redaction, severity stamping and deterministic ordering.
package findings

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// FingerprintPrefix versions the fingerprint scheme (ADR-008). A scheme change needs a new prefix.
const FingerprintPrefix = "fp1:"

// fingerprintHexLen is the number of hex characters kept after the prefix (ADR-008).
const fingerprintHexLen = 32

// Fingerprint returns the stable identity of f: "fp1:" plus 32 hex characters of a SHA-256 over
// rule ID, rule version, logical resource identity, Key and the normalised evidence key.
// Line, column, profile, severity, confidence, evidence prose and adapter are excluded, so the
// result is stable across finding order, line moves, profile changes and path separators.
func Fingerprint(f sdk.Finding) string {
	name := f.Resource.Name
	if f.Resource.Kind == "file" {
		name = slash(name)
	}
	if strings.TrimSpace(name) == "" {
		// No logical name: the file path is the only identity left (ADR-008, "when the resource has a logical name").
		name = slash(f.Location.File)
	}
	key := strings.TrimSpace(f.Key)
	evidenceKey := ""
	if key == "" {
		evidenceKey = strings.TrimSpace(f.Resource.Pointer)
	}
	parts := []string{
		f.RuleID,
		strconv.Itoa(f.RuleVersion),
		f.Resource.Kind,
		strings.ToLower(f.Resource.Type),
		strings.TrimSpace(name),
		key,
		evidenceKey,
	}
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		// A NUL inside a part would make the join ambiguous.
		h.Write([]byte(strings.ReplaceAll(p, "\x00", "�")))
	}
	return FingerprintPrefix + hex.EncodeToString(h.Sum(nil))[:fingerprintHexLen]
}

// slash converts platform path separators to forward slashes and drops a leading "./".
func slash(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	return strings.TrimPrefix(p, "./")
}
