package plan_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/parser"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
	"github.com/RuairiSpain/copilot-Web/x-foundry/schemas"
)

func exampleFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(files) < 6 {
		t.Fatalf("examples: %v %v", files, err)
	}
	return files
}

func TestExamplesAreValidAndOrdered(t *testing.T) {
	for _, f := range exampleFiles(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			a, err := plan.AnalyseFile(f)
			if err != nil {
				t.Fatal(err)
			}
			p := MustOK(t, a)
			position := map[string]int{}
			for i, n := range p.Nodes {
				position[n.ID] = i
			}
			for _, n := range p.Nodes {
				for _, d := range n.DependsOn {
					if position[d] >= position[n.ID] {
						t.Fatalf("%s depends on %s, which comes later", n.ID, d)
					}
				}
			}
			total := 0
			for _, layer := range p.Layers {
				total += len(layer)
			}
			if total != len(p.Nodes) {
				t.Fatalf("layers cover %d of %d nodes", total, len(p.Nodes))
			}
			for _, d := range a.Diagnostics {
				if d.Severity == "error" {
					t.Fatal(d)
				}
			}
		})
	}
}

func TestExamplePlansAreDeterministic(t *testing.T) {
	for _, f := range exampleFiles(t) {
		first, _ := plan.BuildFile(f)
		second, _ := plan.BuildFile(f)
		a, _ := first.JSON("")
		b, _ := second.JSON("")
		if !bytes.Equal(a, b) {
			t.Fatalf("%s: plans differ between runs", f)
		}
	}
}

func TestSchemaIsValidAndHasNoSessionPoolSettings(t *testing.T) {
	c := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemas.XFoundry))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("urn:x", doc); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Compile("urn:x"); err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(schemas.XFoundry))
	if strings.Contains(lower, "sessionpool") || strings.Contains(lower, "session_pool") {
		t.Fatal("the schema must not contain session-pool settings")
	}
	if schemas.SchemaVersion != "1.0" || !strings.Contains(string(schemas.XFoundry), "/1.0.0/") {
		t.Fatal("schema version is not pinned to 1.0")
	}
}

// Every property the schema declares for the root has a field in the typed model, and the
// other way round, so the two cannot drift apart.
func TestSchemaAndModelDeclareTheSameRootKeys(t *testing.T) {
	var s struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemas.XFoundry, &s); err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseMapping(Doc(t), "<test>")
	if err != nil {
		t.Fatal(err)
	}
	fields := jsonKeys(parsed.Config)
	for key := range s.Defs["xFoundry"].Properties {
		if !fields[key] {
			t.Fatalf("schema key %q has no field in the typed model", key)
		}
		delete(fields, key)
	}
	for key := range fields {
		t.Fatalf("model field %q is not declared by the schema", key)
	}
}

func TestPublishedSchemasAreCurrentAndValidateExamples(t *testing.T) {
	wrapper, err := os.ReadFile("../../schemas/azure-yaml-x-foundry.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(wrapper))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddResource("urn:wrapper", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile("urn:wrapper")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range exampleFiles(t) {
		text, _ := os.ReadFile(f)
		v, err := parser.LoadYAML(string(text))
		if err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(v); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	// The wrapper requires x-foundry but allows every native azd key.
	app, _ := parser.LoadYAML("name: app\nservices: {}\n")
	if sch.Validate(app) == nil {
		t.Fatal("x-foundry is required")
	}
	text, _ := os.ReadFile("../../examples/standalone-minimal.yaml")
	v, _ := parser.LoadYAML(string(text))
	v.(map[string]any)["services"] = map[string]any{"api": map[string]any{"project": "./api", "language": "py", "host": "containerapp"}}
	if err := sch.Validate(v); err != nil {
		t.Fatalf("native azd keys must be allowed: %v", err)
	}
}
