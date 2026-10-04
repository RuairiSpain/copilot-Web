// Package parser reads azure.yaml and produces the validated x-foundry configuration.
package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
)

// LoadYAML parses YAML text. Duplicate mapping keys are an error rather than silently
// keeping the last one. Numbers become json.Number so the result is JSON-compatible.
func LoadYAML(text string) (any, error) {
	var v any
	if err := yaml.Unmarshal([]byte(text), &v); err != nil {
		return nil, diag.Fail(diag.Err("XF100", "", "invalid YAML: %s", cleanYAMLError(err)))
	}
	return toJSONValue(v)
}

func cleanYAMLError(err error) string {
	return strings.TrimSpace(strings.TrimPrefix(err.Error(), "yaml:"))
}

// toJSONValue round-trips through JSON so every number is a json.Number and every map is
// map[string]any (YAML may produce map[any]any).
func toJSONValue(v any) (any, error) {
	v = stringKeys(v)
	b, err := json.Marshal(v)
	if err != nil {
		return nil, diag.Fail(diag.Err("XF100", "", "invalid YAML: %v", err))
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, diag.Fail(diag.Err("XF100", "", "invalid YAML: %v", err))
	}
	return out, nil
}

func stringKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = stringKeys(x)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[fmt.Sprint(k)] = stringKeys(x)
		}
		return out
	case []any:
		for i, x := range t {
			t[i] = stringKeys(x)
		}
		return t
	}
	return v
}

// ReadFile reads path, reporting failure as a diagnostic.
func ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the CLI reads the file the user names
	if err != nil {
		return "", diag.Fail(diag.Err("XF100", "", "cannot read %s: %v", path, err))
	}
	return string(b), nil
}
