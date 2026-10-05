package model

import (
	"encoding/json"
	"slices"
	"strings"
)

// EnvValue is one azd environment value. It never prints itself: String, GoString and the JSON
// encoder all yield a redaction marker, so a stray %v or a marshalled struct cannot leak it.
type EnvValue struct{ v string }

// NewEnvValue wraps a raw value.
func NewEnvValue(v string) EnvValue { return EnvValue{v: v} }

// Reveal returns the raw value. Call it only to compare or to test shape, never to build output.
func (e EnvValue) Reveal() string { return e.v }

// String implements fmt.Stringer.
func (e EnvValue) String() string { return "<redacted>" }

// GoString implements fmt.GoStringer.
func (e EnvValue) GoString() string { return "<redacted>" }

// MarshalJSON implements json.Marshaler.
func (e EnvValue) MarshalJSON() ([]byte, error) { return []byte(`"<redacted>"`), nil }

// MarshalText implements encoding.TextMarshaler.
func (e EnvValue) MarshalText() ([]byte, error) { return []byte("<redacted>"), nil }

// Environment is one azd environment: its name and its key/value pairs.
type Environment struct {
	Name   string
	values map[string]EnvValue
}

// NewEnvironment builds an Environment from raw pairs. The map is copied.
func NewEnvironment(name string, raw map[string]string) Environment {
	m := make(map[string]EnvValue, len(raw))
	for k, v := range raw {
		m[k] = NewEnvValue(v)
	}
	return Environment{Name: name, values: m}
}

// Has reports whether the key is set (an empty value still counts as set).
func (e Environment) Has(key string) bool { _, ok := e.values[key]; return ok }

// Get returns the value for key.
func (e Environment) Get(key string) (EnvValue, bool) { v, ok := e.values[key]; return v, ok }

// Keys returns the key names, sorted. Key names are safe to print; values are not.
func (e Environment) Keys() []string {
	keys := make([]string, 0, len(e.values))
	for k := range e.values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// String implements fmt.Stringer. It shows the name and the key names only, never a value.
func (e Environment) String() string {
	return "Environment{" + e.Name + " keys=" + strings.Join(e.Keys(), ",") + "}"
}

// GoString implements fmt.GoStringer with the same redaction as String.
func (e Environment) GoString() string { return e.String() }

// MarshalJSON emits the name and sorted key names only.
func (e Environment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name string   `json:"name"`
		Keys []string `json:"keys"`
	}{e.Name, e.Keys()})
}
