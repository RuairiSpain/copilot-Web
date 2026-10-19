package prices

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A cache written by SaveCache must match schemas/price-cache.schema.json:
// the required fields are present and every emitted field is documented.
func TestSavedCacheMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "price-cache.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	path := filepath.Join(t.TempDir(), "prices.json")
	q := Query{ServiceName: "S", ProductName: "P", Region: "eastus", CurrencyCode: "USD", MeterName: "M", RequireHourly: true}
	rec := Record{Query: q, CurrencyCode: "USD", Region: "eastus", ProductName: "P", SKUName: "K", ArmSKUName: "A", MeterName: "M", MeterID: "id",
		UnitOfMeasure: "1 Hour", UnitPrice: 1.5, Type: "Consumption", PrimaryMeterRegion: true, EffectiveStartDate: "2025-10-01T00:00:00Z", RetrievedAt: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
	if err := SaveCache(path, Cache{Records: []Record{rec}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if problems := checkAgainst(doc, schema, schema, "$"); len(problems) != 0 {
		t.Fatalf("cache does not match the schema:\n%s", strings.Join(problems, "\n"))
	}
	// The schema must also reject what the code rejects: a negative price.
	doc["records"].([]any)[0].(map[string]any)["unitPrice"] = -1.0
	if problems := checkAgainst(doc, schema, schema, "$"); len(problems) == 0 {
		t.Fatal("the schema accepts a negative unitPrice")
	}
}

// checkAgainst checks the subset of JSON Schema that price-cache.schema.json
// uses: $ref, type, const, required, properties, additionalProperties false,
// items, minimum and minLength. Fields the schema does not document are reported.
func checkAgainst(v, node, root any, path string) []string {
	n := node.(map[string]any)
	if ref, ok := n["$ref"].(string); ok {
		cur := root
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			cur = cur.(map[string]any)[part]
		}
		return checkAgainst(v, cur, root, path)
	}
	var out []string
	if c, ok := n["const"]; ok && fmt.Sprint(c) != fmt.Sprint(v) {
		out = append(out, fmt.Sprintf("%s: %v != const %v", path, v, c))
	}
	switch t, _ := n["type"].(string); t {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return append(out, path+": not an object")
		}
		props, _ := n["properties"].(map[string]any)
		if req, ok := n["required"].([]any); ok {
			for _, r := range req {
				if _, ok := m[r.(string)]; !ok {
					out = append(out, fmt.Sprintf("%s: missing %q", path, r))
				}
			}
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			sub, ok := props[k]
			if !ok {
				if n["additionalProperties"] == false {
					out = append(out, fmt.Sprintf("%s: property %q is not documented in the schema", path, k))
				}
				continue
			}
			out = append(out, checkAgainst(m[k], sub, root, path+"."+k)...)
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return append(out, path+": not an array")
		}
		for i, e := range a {
			out = append(out, checkAgainst(e, n["items"], root, fmt.Sprintf("%s[%d]", path, i))...)
		}
	case "number":
		f, ok := v.(float64)
		if !ok {
			return append(out, path+": not a number")
		}
		if min, ok := n["minimum"].(float64); ok && f < min {
			out = append(out, fmt.Sprintf("%s: %v < minimum %v", path, f, min))
		}
	case "string":
		s, ok := v.(string)
		if !ok {
			return append(out, path+": not a string")
		}
		if ml, ok := n["minLength"].(float64); ok && float64(len(s)) < ml {
			out = append(out, path+": shorter than minLength")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			out = append(out, path+": not a boolean")
		}
	}
	return out
}
