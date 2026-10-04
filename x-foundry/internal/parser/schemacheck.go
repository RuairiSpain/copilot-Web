package parser

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/schemas"
)

// Root is the path prefix of every diagnostic.
const Root = "x-foundry"

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.DefaultDraft(jsonschema.Draft2020)
		c.AssertFormat()
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemas.XFoundry))
		if err != nil {
			compileErr = err
			return
		}
		const url = "urn:x-foundry:schema"
		if compileErr = c.AddResource(url, doc); compileErr != nil {
			return
		}
		compiled, compileErr = c.Compile(url)
	})
	return compiled, compileErr
}

// FormatPath turns a JSON pointer ("/projects/0/name") into "x-foundry.projects[0].name".
func FormatPath(pointer string) string {
	out := Root
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if _, err := strconv.Atoi(part); err == nil {
			out += "[" + part + "]"
		} else {
			out += "." + part
		}
	}
	return out
}

var sessionPool = regexp.MustCompile(`(?i)session[-_ ]?pool|agent[-_ ]?pool|pooling`)

// walk visits every mapping key with the path of its parent.
func walk(node any, path []string, visit func(parent []string, key string)) {
	switch t := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			visit(path, k)
			walk(t[k], append(append([]string{}, path...), k), visit)
		}
	case []any:
		for i, x := range t {
			walk(x, append(append([]string{}, path...), strconv.Itoa(i)), visit)
		}
	}
}

func pointer(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	return "/" + strings.Join(parts, "/")
}

// FindUnsupportedKeys implements rule 18: session pools are out of scope.
func FindUnsupportedKeys(raw any) []diag.Diagnostic {
	var found []diag.Diagnostic
	walk(raw, nil, func(parent []string, key string) {
		path := FormatPath(pointer(append(append([]string{}, parent...), key)...))
		switch {
		case sessionPool.MatchString(key):
			found = append(found, diag.Err("XF018", path,
				"'%s' is not supported: session and agent pooling are separate Foundry capabilities and are out of scope for x-foundry", key))
		}
	})
	return found
}

var versionPattern = regexp.MustCompile(`^\d+\.\d+$`)

// CheckVersion rejects a schemaVersion with an unsupported major version (XF103).
func CheckVersion(raw map[string]any) []diag.Diagnostic {
	version, ok := raw["schemaVersion"].(string)
	if !ok || !versionPattern.MatchString(version) {
		return nil // absent means default; malformed is reported by the schema pass
	}
	supported := strings.SplitN(schemas.SchemaVersion, ".", 2)[0]
	if strings.SplitN(version, ".", 2)[0] != supported {
		return []diag.Diagnostic{diag.Err("XF103", Root+".schemaVersion",
			"schemaVersion %s is not supported; this release supports %s.x", version, supported)}
	}
	return nil
}

var quoted = regexp.MustCompile(`'([^']+)'`)

// ValidateSchema validates raw against the JSON Schema, one diagnostic per violation.
func ValidateSchema(raw any) ([]diag.Diagnostic, error) {
	sch, err := schema()
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	unsupported := FindUnsupportedKeys(raw)
	flagged := make(map[string]bool, len(unsupported))
	for _, d := range unsupported {
		flagged[d.Path] = true
	}
	verr, ok := sch.Validate(raw).(*jsonschema.ValidationError)
	if !ok {
		return unsupported, nil
	}
	var found []diag.Diagnostic
	var collect func(u jsonschema.OutputUnit)
	collect = func(u jsonschema.OutputUnit) {
		path := FormatPath(u.InstanceLocation)
		if len(u.Errors) > 0 {
			if strings.HasSuffix(u.KeywordLocation, "/oneOf") || strings.HasSuffix(u.KeywordLocation, "/anyOf") {
				found = append(found, diag.Err("XF102", path, "does not match any allowed form (%s)", firstLeaf(u)))
				return
			}
			for _, e := range u.Errors {
				collect(e)
			}
			return
		}
		if u.Error == nil {
			return
		}
		msg := u.Error.String()
		if strings.HasSuffix(u.KeywordLocation, "/additionalProperties") {
			for _, m := range quoted.FindAllStringSubmatch(msg, -1) {
				if flagged[path+"."+m[1]] {
					return // reported with a more specific message
				}
			}
		}
		found = append(found, diag.Err("XF102", path, "%s", msg))
	}
	collect(*verr.DetailedOutput())
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Path != found[j].Path {
			return found[i].Path < found[j].Path
		}
		return found[i].Message < found[j].Message
	})
	return append(unsupported, found...), nil
}

func firstLeaf(u jsonschema.OutputUnit) string {
	if len(u.Errors) == 0 {
		if u.Error != nil {
			return u.Error.String()
		}
		return ""
	}
	return firstLeaf(u.Errors[len(u.Errors)-1])
}
