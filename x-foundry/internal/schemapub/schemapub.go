// Package schemapub derives the published schema files from schemas/x-foundry.schema.json.
package schemapub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PublishedExamples are copied to schemas/examples.
var PublishedExamples = []string{
	"standalone-minimal", "standalone-private", "hub-spoke", "foundry-iq", "apim-ai-gateway",
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Render returns {path relative to schemas/: content} for every generated file. root is
// the x-foundry module directory. The canonical schema is copied byte for byte.
func Render(root string) (map[string][]byte, error) {
	src, err := os.ReadFile(filepath.Join(root, "schemas", "x-foundry.schema.json")) //nolint:gosec // path is built from the module root
	if err != nil {
		return nil, err
	}
	var subtree struct {
		Schema string          `json:"$schema"`
		ID     string          `json:"$id"`
		Defs   json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(src, &subtree); err != nil {
		return nil, err
	}
	// .../azd/x-foundry/1.0.0/x-foundry.schema.json -> version "1.0"
	parts := strings.Split(subtree.ID, "/x-foundry/")
	if len(parts) != 2 || !strings.Contains(parts[1], "/") {
		return nil, fmt.Errorf("unexpected $id %q", subtree.ID)
	}
	version := parts[1][:strings.LastIndex(parts[1], "/")]
	version = version[:strings.LastIndex(version, ".")]

	type ref struct {
		Ref string `json:"$ref"`
	}
	wrapper := struct {
		Schema               string          `json:"$schema"`
		ID                   string          `json:"$id"`
		Title                string          `json:"title"`
		Description          string          `json:"description"`
		Type                 string          `json:"type"`
		Properties           map[string]ref  `json:"properties"`
		Required             []string        `json:"required"`
		AdditionalProperties bool            `json:"additionalProperties"`
		Defs                 json.RawMessage `json:"$defs"`
	}{
		Schema: subtree.Schema,
		ID:     strings.Replace(subtree.ID, "x-foundry.schema.json", "azure-yaml-x-foundry.schema.json", 1),
		Title:  "azure.yaml with the x-foundry extension",
		Description: "Validates the x-foundry key of an azure.yaml while allowing every native azd property " +
			"alongside it. Combine with the native azure.yaml schema in editors (allOf). x-foundry is " +
			"implemented by a custom extension and is not a native azure.yaml capability.",
		Type:                 "object",
		Properties:           map[string]ref{"x-foundry": {"#/$defs/xFoundry"}},
		Required:             []string{"x-foundry"},
		AdditionalProperties: true,
		Defs:                 subtree.Defs,
	}
	files := map[string][]byte{
		"x-foundry.schema.json":                          src,
		"versions/" + version + "/x-foundry.schema.json": src,
	}
	if files["azure-yaml-x-foundry.schema.json"], err = marshal(wrapper); err != nil {
		return nil, err
	}
	for _, name := range PublishedExamples {
		b, err := os.ReadFile(filepath.Join(root, "examples", name+".yaml")) //nolint:gosec // path is built from the module root
		if err != nil {
			return nil, err
		}
		files["examples/"+name+".yaml"] = b
	}
	return files, nil
}

// Stale lists generated files that differ from what is on disk under schemas/.
func Stale(root string, files map[string][]byte) []string {
	var stale []string
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(root, "schemas", rel)) //nolint:gosec // path is built from the module root
		if err != nil || !bytes.Equal(got, want) {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	return stale
}

// Write writes the generated files under schemas/.
func Write(root string, files map[string][]byte) error {
	for rel, content := range files {
		target := filepath.Join(root, "schemas", rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o600); err != nil {
			return err
		}
	}
	return nil
}
