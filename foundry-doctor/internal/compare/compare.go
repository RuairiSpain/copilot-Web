// Package compare diffs two azd environments. It returns key names and value fingerprints only;
// a value never appears in the result, in an error, or in a log line (ADR-008, CLAUDE.md).
package compare

import (
	"context"
	"errors"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

// ErrNotImplemented is returned until the Phase 1 implementation lands.
var ErrNotImplemented = errors.New("compare: not implemented")

// DiffKind classifies one key.
type DiffKind string

// DiffKind values.
const (
	OnlyLeft  DiffKind = "only-left"
	OnlyRight DiffKind = "only-right"
	Different DiffKind = "different"
	Same      DiffKind = "same"
)

// KeyDiff is the comparison of one key. Fingerprints are the first 16 hex characters of
// SHA-256(key || 0x00 || value); empty on the side where the key is missing. They let a reader see
// that two values differ without learning either value.
type KeyDiff struct {
	Key              string   `json:"key"`
	Kind             DiffKind `json:"kind"`
	LeftFingerprint  string   `json:"leftFingerprint,omitempty"`
	RightFingerprint string   `json:"rightFingerprint,omitempty"`
}

// Diff is the deterministic result: Keys is sorted by key name.
type Diff struct {
	Left  string    `json:"left"`
	Right string    `json:"right"`
	Keys  []KeyDiff `json:"keys"`
}

// Environments compares two environments.
func Environments(ctx context.Context, left, right model.Environment) (Diff, error) {
	return Diff{}, ErrNotImplemented
}
