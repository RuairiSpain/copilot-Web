package json_test

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	reportjson "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/json"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/reporttest"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func render(t *testing.T, r *sdk.Report) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := reportjson.New().Write(&b, r); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestGolden(t *testing.T) {
	reporttest.Golden(t, "report.golden.json", render(t, reporttest.FullReport()))
}

func TestFormat(t *testing.T) {
	if reportjson.New().Format() != "json" {
		t.Fatal("format")
	}
}

func TestEmptyReportHasNoNulls(t *testing.T) {
	for _, r := range []*sdk.Report{nil, {}} {
		out := render(t, r)
		if bytes.Contains(out, []byte("null")) {
			t.Errorf("null in output:\n%s", out)
		}
		var m map[string]any
		if err := stdjson.Unmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		if m["schemaVersion"] != sdk.ReportSchemaVersion {
			t.Errorf("schemaVersion = %v", m["schemaVersion"])
		}
		for _, k := range []string{"tools", "findings", "skipped"} {
			if a, ok := m[k].([]any); !ok || len(a) != 0 {
				t.Errorf("%s = %#v, want []", k, m[k])
			}
		}
		if p, ok := m["effectivePolicy"].(map[string]any); !ok || len(p) != 0 {
			t.Errorf("effectivePolicy = %#v", m["effectivePolicy"])
		}
	}
}

func TestInputNotModified(t *testing.T) {
	r := reporttest.FullReport()
	before, _ := stdjson.Marshal(r)
	render(t, r)
	after, _ := stdjson.Marshal(r)
	if !bytes.Equal(before, after) {
		t.Error("Write modified its input")
	}
}

func TestPolicyRedacted(t *testing.T) {
	if out := render(t, reporttest.FullReport()); bytes.Contains(out, []byte(reporttest.Canary)) {
		t.Errorf("canary leaked:\n%s", out)
	}
}

func TestRoundTrip(t *testing.T) {
	var got sdk.Report
	if err := stdjson.Unmarshal(render(t, reporttest.FullReport()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 5 || got.Findings[0].Location.File != "azure.yaml" || got.Findings[1].RuleID != "FND-SEC-002" {
		t.Errorf("findings = %+v", got.Findings)
	}
}

// ---- schema parity ----

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "report.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := stdjson.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func resolve(t *testing.T, root, s map[string]any) map[string]any {
	t.Helper()
	ref, ok := s["$ref"].(string)
	if !ok {
		return s
	}
	name, found := strings.CutPrefix(ref, "#/$defs/")
	if !found {
		t.Fatalf("unsupported $ref %q", ref)
	}
	d, ok := root["$defs"].(map[string]any)[name].(map[string]any)
	if !ok {
		t.Fatalf("unknown $ref %q", ref)
	}
	return d
}

func tagInfo(f reflect.StructField) (name string, required bool) {
	parts := strings.Split(f.Tag.Get("json"), ",")
	required = true
	for _, o := range parts[1:] {
		if o == "omitempty" || o == "omitzero" {
			required = false
		}
	}
	return parts[0], required
}

func checkType(t *testing.T, root map[string]any, typ reflect.Type, s map[string]any, path string) {
	t.Helper()
	s = resolve(t, root, s)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	want := map[reflect.Kind]string{
		reflect.String: "string", reflect.Int: "integer", reflect.Bool: "boolean",
		reflect.Slice: "array", reflect.Map: "object", reflect.Struct: "object",
	}[typ.Kind()]
	if typ.Kind() == reflect.String {
		_, isEnum := s["enum"]
		_, isConst := s["const"]
		if !isEnum && !isConst && s["type"] != "string" {
			t.Errorf("%s: string field needs type string, enum or const: %v", path, s)
		}
		return
	}
	if s["type"] != want {
		t.Errorf("%s: schema type %v, Go kind %v wants %s", path, s["type"], typ.Kind(), want)
	}
	switch typ.Kind() {
	case reflect.Slice:
		items, ok := s["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array without items", path)
		}
		checkType(t, root, typ.Elem(), items, path+"[]")
	case reflect.Map:
		if ap, ok := s["additionalProperties"].(map[string]any); ok {
			checkType(t, root, typ.Elem(), ap, path+"{}")
		} else if typ.Elem().Kind() != reflect.Interface {
			t.Errorf("%s: typed map needs additionalProperties", path)
		}
	case reflect.Struct:
		props, _ := s["properties"].(map[string]any)
		if s["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false", path)
		}
		req := map[string]bool{}
		reqList, _ := s["required"].([]any)
		for _, r := range reqList {
			req[r.(string)] = true
		}
		seen := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, required := tagInfo(f)
			seen[name] = true
			sub, ok := props[name].(map[string]any)
			if !ok {
				t.Errorf("%s: Go field %s (%q) missing from schema", path, f.Name, name)
				continue
			}
			if req[name] != required {
				t.Errorf("%s.%s: required in schema = %v, Go tag says %v", path, name, req[name], required)
			}
			checkType(t, root, f.Type, sub, path+"."+name)
		}
		for name := range props {
			if !seen[name] {
				t.Errorf("%s: schema property %q has no Go field", path, name)
			}
		}
	}
}

func TestSchemaParityWithGoStructs(t *testing.T) {
	root := loadSchema(t)
	if root["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("$schema = %v", root["$schema"])
	}
	checkType(t, root, reflect.TypeOf(sdk.Report{}), root, "report")
	if c := root["properties"].(map[string]any)["schemaVersion"].(map[string]any)["const"]; c != sdk.ReportSchemaVersion {
		t.Errorf("schemaVersion const %v != %s", c, sdk.ReportSchemaVersion)
	}
}

func TestSchemaEnumsMatchSDK(t *testing.T) {
	root := loadSchema(t)
	defs := root["$defs"].(map[string]any)
	enum := func(def, prop string) []string {
		var out []string
		for _, v := range defs[def].(map[string]any)["properties"].(map[string]any)[prop].(map[string]any)["enum"].([]any) {
			out = append(out, v.(string))
		}
		sort.Strings(out)
		return out
	}
	cases := []struct {
		def, prop string
		want      []string
	}{
		{"finding", "severity", []string{"error", "info", "warning"}},
		{"finding", "confidence", []string{string(sdk.ConfidenceCertain), string(sdk.ConfidenceLikely), string(sdk.ConfidenceUncertain)}},
		{"finding", "category", []string{"", string(sdk.CategoryMustHave), string(sdk.CategoryNiceToHave)}},
		{"toolStatus", "state", []string{string(sdk.ToolAvailable), string(sdk.ToolDisabled), string(sdk.ToolFailed), string(sdk.ToolMissing), string(sdk.ToolUnsupported)}},
	}
	for _, c := range cases {
		sort.Strings(c.want)
		if got := enum(c.def, c.prop); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s.%s enum = %v, want %v", c.def, c.prop, got, c.want)
		}
	}
}

// validate is a small validator for the subset of JSON Schema the report schema uses.
func validate(root, s map[string]any, v any, path string, errs *[]string) {
	if ref, ok := s["$ref"].(string); ok {
		s = root["$defs"].(map[string]any)[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
	}
	if c, ok := s["const"]; ok && c != v {
		*errs = append(*errs, fmt.Sprintf("%s: %v != const %v", path, v, c))
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			found = found || x == v
		}
		if !found {
			*errs = append(*errs, fmt.Sprintf("%s: %v not in enum", path, v))
		}
	}
	switch s["type"] {
	case "string":
		if _, ok := v.(string); !ok {
			*errs = append(*errs, path+": not a string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			*errs = append(*errs, path+": not a boolean")
		}
	case "integer":
		if f, ok := v.(float64); !ok || f != float64(int64(f)) {
			*errs = append(*errs, path+": not an integer")
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			*errs = append(*errs, path+": not an array")
			return
		}
		for i, e := range a {
			validate(root, s["items"].(map[string]any), e, fmt.Sprintf("%s[%d]", path, i), errs)
		}
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			*errs = append(*errs, path+": not an object")
			return
		}
		props, _ := s["properties"].(map[string]any)
		if r, ok := s["required"].([]any); ok {
			for _, k := range r {
				if _, has := m[k.(string)]; !has {
					*errs = append(*errs, path+": missing required "+k.(string))
				}
			}
		}
		for k, e := range m {
			if sub, ok := props[k].(map[string]any); ok {
				validate(root, sub, e, path+"."+k, errs)
			} else if ap, ok := s["additionalProperties"].(map[string]any); ok {
				validate(root, ap, e, path+"."+k, errs)
			} else if s["additionalProperties"] == false {
				*errs = append(*errs, path+": unexpected property "+k)
			}
		}
	}
}

func TestOutputValidatesAgainstSchema(t *testing.T) {
	root := loadSchema(t)
	for name, r := range map[string]*sdk.Report{"full": reporttest.FullReport(), "empty": {}, "huge": reporttest.Huge(50)} {
		var v any
		if err := stdjson.Unmarshal(render(t, r), &v); err != nil {
			t.Fatal(err)
		}
		var errs []string
		validate(root, root, v, "$", &errs)
		if len(errs) > 0 {
			t.Errorf("%s: %s", name, strings.Join(errs, "\n"))
		}
	}
}

func TestSchemaRejectsBadReport(t *testing.T) {
	root := loadSchema(t)
	var v any
	_ = stdjson.Unmarshal([]byte(`{"schemaVersion":"2","extra":1}`), &v)
	var errs []string
	validate(root, root, v, "$", &errs)
	if len(errs) < 3 {
		t.Errorf("validator too lax: %v", errs)
	}
}
