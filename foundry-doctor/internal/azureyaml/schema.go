package azureyaml

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/ruairispain/copilot-web/foundry-doctor/schemas"
)

// rootFile is the vendored root schema (azd tag azure-dev-cli_1.35.0).
const rootFile = "azure.yaml.json"

// schemaSet is the parsed vendored schemas plus the facts derived from them once. It is read-only after load.
type schemaSet struct {
	root       map[string]any
	files      map[string]map[string]any
	knownHosts map[string]bool // hosts the root schema lists as examples or gives per-host rules
}

var loadSchemas = sync.OnceValues(func() (*schemaSet, error) {
	s := &schemaSet{files: map[string]map[string]any{}, knownHosts: map[string]bool{}}
	err := fs.WalkDir(schemas.AzdFS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return err
		}
		raw, rerr := fs.ReadFile(schemas.AzdFS(), p)
		if rerr != nil {
			return rerr
		}
		var doc map[string]any
		if jerr := json.Unmarshal(raw, &doc); jerr != nil {
			return fmt.Errorf("parse %s: %w", p, jerr)
		}
		s.files[p] = doc
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load vendored azd schemas: %w", err)
	}
	s.root = s.files[rootFile]
	if s.root == nil {
		return nil, fmt.Errorf("load vendored azd schemas: %s missing", rootFile)
	}
	svc, _ := dig(s.root, "properties", "services", "additionalProperties").(map[string]any)
	allOf, _ := svc["allOf"].([]any)
	for _, e := range allOf {
		if host, _ := dig(e, "if", "properties", "host", "const").(string); host != "" {
			s.knownHosts[host] = true // hosts with per-host rules
		}
	}
	for _, h := range stringList(dig(svc, "properties", "host", "examples")) {
		s.knownHosts[h] = true
	}
	return s, nil
})

// dig follows map keys (and decimal indexes into arrays) through decoded JSON.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		switch t := v.(type) {
		case map[string]any:
			v = t[k]
		case []any:
			var i int
			if _, err := fmt.Sscanf(k, "%d", &i); err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

func stringList(v any) []string {
	l, _ := v.([]any)
	var out []string
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
