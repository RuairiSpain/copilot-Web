package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// StateVersion is the version of the state file format.
const StateVersion = 1

// StateItem records what was deployed for one item. It holds no secrets: a content hash and the
// version the service returned.
type StateItem struct {
	Project string `json:"project"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Hash    string `json:"hash"`
	Version string `json:"version"`
}

// Key identifies the item.
func (s StateItem) Key() string { return Key(s.Project, s.Kind, s.Name) }

// State is the record of the last deployment of one environment. Items that are in the state
// are the ones x-foundry owns, so it only ever deletes those.
type State struct {
	SchemaVersion int         `json:"schemaVersion"`
	Environment   string      `json:"environment"`
	Items         []StateItem `json:"items"`
}

// NewState returns an empty state.
func NewState(environment string) *State {
	return &State{SchemaVersion: StateVersion, Environment: environment}
}

// LoadState reads a state file; a missing file is an empty state. A file for another environment
// or a newer format is an error so two environments are never mixed.
func LoadState(path, environment string) (*State, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the user's own state file
	if errors.Is(err, fs.ErrNotExist) {
		return NewState(environment), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("state file %s is not valid: %w", path, err)
	}
	if s.SchemaVersion > StateVersion {
		return nil, fmt.Errorf("state file %s has format %d; this release reads up to %d", path, s.SchemaVersion, StateVersion)
	}
	if s.Environment != environment {
		return nil, fmt.Errorf("state file %s belongs to environment %q, not %q", path, s.Environment, environment)
	}
	return &s, nil
}

// Save writes the state atomically.
func (s *State) Save(path string) error {
	sort.Slice(s.Items, func(i, j int) bool { return s.Items[i].Key() < s.Items[j].Key() })
	s.SchemaVersion = StateVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// Find returns the recorded item.
func (s *State) Find(key string) (StateItem, bool) {
	for _, it := range s.Items {
		if it.Key() == key {
			return it, true
		}
	}
	return StateItem{}, false
}

// Set records an item, replacing an earlier record of the same key.
func (s *State) Set(item StateItem) {
	for i, it := range s.Items {
		if it.Key() == item.Key() {
			s.Items[i] = item
			return
		}
	}
	s.Items = append(s.Items, item)
}

// Remove forgets an item.
func (s *State) Remove(key string) {
	out := s.Items[:0]
	for _, it := range s.Items {
		if it.Key() != key {
			out = append(out, it)
		}
	}
	s.Items = out
}
