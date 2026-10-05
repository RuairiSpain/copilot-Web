package azureyaml

import (
	"slices"
	"strings"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func TestFixturesStructure(t *testing.T) {
	tests := []struct {
		file string
		want []string // "code path" of error-level issues, sorted
	}{
		// negative: must produce no error
		{"minimal-hosted-agent-project.azure.yaml", nil},
		{"full-example-from-reference.azure.yaml", nil},
		{"unknown-extra-property-allowed.azure.yaml", nil},
		// positive: must produce findings
		{"agent-without-project.azure.yaml", []string{"missing-required services.triage"}},
		{"prompt-agent-missing-model-and-instructions.azure.yaml", []string{"missing-required services.helper", "missing-required services.helper"}},
		{"project-service-with-project-path.azure.yaml", []string{"forbidden-property services.ai-project.image", "forbidden-property services.ai-project.project"}},
		{"voice-modeltype-hosted-agent.azure.yaml", []string{
			"forbidden-property services.talker.targetAgent",
			"invalid-enum services.talker.modelType",
			"missing-required services.talker",
			"unsupported-shape services.talker.modelType",
			"unsupported-shape services.talker.targetAgent",
		}},
		{"unknown-host-missing-host-field.azure.yaml", []string{"missing-required services.nohost"}},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got := codePaths(errorsOf(fixture(t, tc.file)))
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("errors = %v\nwant     %v", got, want)
			}
		})
	}
}

func TestUnknownHostAndTopLevelAreInfo(t *testing.T) {
	r := fixture(t, "unknown-host-missing-host-field.azure.yaml")
	var found bool
	for _, i := range r.Issues {
		if i.Code == CodeUnknownHost {
			found = true
			if i.Level != LevelInfo || i.Path != "services.mystery.host" || i.Pointer != "/services/mystery/host" || i.Pos != (model.Pos{Line: 4, Column: 11}) {
				t.Errorf("unknown-host issue = %+v", i)
			}
		}
	}
	if !found {
		t.Error("no unknown-host issue")
	}
	r = fixture(t, "unknown-extra-property-allowed.azure.yaml")
	if len(r.UnknownTopLevel) != 1 || r.UnknownTopLevel[0].Name != "x-custom-top-level" || r.UnknownTopLevel[0].Pos != (model.Pos{Line: 2, Column: 1}) {
		t.Errorf("unknown top-level = %+v", r.UnknownTopLevel)
	}
	for _, i := range r.Issues {
		if i.Code == CodeUnknownTopLevelKey && i.Level != LevelInfo {
			t.Errorf("unknown top-level key must be info, got %s", i.Level)
		}
	}
}

func TestStructuralChecks(t *testing.T) {
	svc := func(body string) string {
		return "name: demo\nservices:\n  api:\n    host: containerapp\n    project: src/api\n" + body
	}
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"valid", svc(""), nil},
		{"name uppercase", "name: Demo\nservices:\n  a:\n    host: x\n", []string{"pattern-mismatch name"}},
		{"name too short", "name: a\nservices:\n  a:\n    host: x\n", []string{"invalid-length name"}},
		{"name underscore", "name: my_app\nservices:\n  a:\n    host: x\n", []string{"pattern-mismatch name"}},
		{"name is a number", "name: 123\nservices:\n  a:\n    host: x\n", nil},
		{"name is a list", "name: [a, b]\nservices:\n  a:\n    host: x\n", []string{"invalid-type name"}},
		{"name interpolated", "name: ${APP}\nservices:\n  a:\n    host: x\n", nil},
		{"resourceGroup too short", "name: demo\nresourceGroup: ab\nservices:\n  a:\n    host: x\n", []string{"invalid-length resourceGroup"}},
		{"resourceGroup too long", "name: demo\nresourceGroup: " + strings.Repeat("r", 65) + "\nservices:\n  a:\n    host: x\n", []string{"invalid-length resourceGroup"}},
		{"infra provider bad", "name: demo\ninfra:\n  provider: Bicep\nservices:\n  a:\n    host: x\n", []string{"pattern-mismatch infra.provider"}},
		{"infra provider foundry ok", "name: demo\ninfra:\n  provider: microsoft.foundry\nservices:\n  a:\n    host: x\n", nil},
		{"infra not a mapping", "name: demo\ninfra: bicep\nservices:\n  a:\n    host: x\n", []string{"invalid-type infra"}},
		{"pipeline provider bad", "name: demo\npipeline:\n  provider: jenkins\nservices:\n  a:\n    host: x\n", []string{"invalid-enum pipeline.provider"}},
		{"pipeline provider ok", "name: demo\npipeline:\n  provider: azdo\n  variables: [A]\n  secrets: [B]\nservices:\n  a:\n    host: x\n", nil},
		{"pipeline variables not list", "name: demo\npipeline:\n  variables: A\nservices:\n  a:\n    host: x\n", []string{"invalid-type pipeline.variables"}},
		{"unknown hook event", "name: demo\nhooks:\n  postlunch:\n    run: echo\nservices:\n  a:\n    host: x\n", []string{"unknown-property hooks.postlunch"}},
		{"hook shell enum", "name: demo\nhooks:\n  postprovision:\n    shell: zsh\n    run: echo\nservices:\n  a:\n    host: x\n", []string{"invalid-enum hooks.postprovision.shell"}},
		{"hook list ok", "name: demo\nhooks:\n  postprovision:\n    - run: echo\n    - run: echo2\nservices:\n  a:\n    host: x\n", nil},
		{"hook list bad item", "name: demo\nhooks:\n  postprovision:\n    - run: echo\n      shell: zsh\nservices:\n  a:\n    host: x\n", []string{"invalid-enum hooks.postprovision[0].shell"}},
		{"hook unknown key", "name: demo\nhooks:\n  postprovision:\n    run: echo\n    bogus: 1\nservices:\n  a:\n    host: x\n", []string{"unknown-property hooks.postprovision.bogus"}},
		{"hook is a scalar", "name: demo\nhooks:\n  postprovision: echo\nservices:\n  a:\n    host: x\n", []string{"invalid-type hooks.postprovision"}},
		{"hook continueOnError not bool", "name: demo\nhooks:\n  postprovision:\n    run: echo\n    continueOnError: maybe\nservices:\n  a:\n    host: x\n", []string{"invalid-type hooks.postprovision.continueOnError"}},
		{"hook continueOnError yes is accepted by azd", "name: demo\nhooks:\n  postprovision:\n    run: echo\n    continueOnError: yes\nservices:\n  a:\n    host: x\n", nil},
		{"hook continueOnError reference", "name: demo\nhooks:\n  postprovision:\n    run: echo\n    continueOnError: ${CONT}\nservices:\n  a:\n    host: x\n", nil},
		{"service hook unknown event", svc("    hooks:\n      postlunch:\n        run: echo\n"), []string{"unknown-property services.api.hooks.postlunch"}},
		{"requiredVersions unknown key", "name: demo\nrequiredVersions:\n  azd: \">= 1.0.0\"\n  tools: x\nservices:\n  a:\n    host: x\n", []string{"unknown-property requiredVersions.tools"}},
		{"requiredVersions extensions list", "name: demo\nrequiredVersions:\n  extensions: [a]\nservices:\n  a:\n    host: x\n", []string{"invalid-type requiredVersions.extensions"}},
		{"resource type enum", "name: demo\nresources:\n  db:\n    type: db.oracle\nservices:\n  a:\n    host: x\n", []string{"invalid-enum resources.db.type"}},
		{"resource type missing", "name: demo\nresources:\n  db:\n    existing: true\nservices:\n  a:\n    host: x\n", []string{"missing-required resources.db"}},
		{"service host empty", "name: demo\nservices:\n  a:\n    host: \"\"\n", []string{"missing-required services.a.host"}},
		{"service host null", "name: demo\nservices:\n  a:\n    host:\n", []string{"missing-required services.a.host"}},
		{"uses not a list", svc("    uses: db\n"), []string{"invalid-type services.api.uses"}},
		{"uses item mapping", svc("    uses:\n      - {a: 1}\n"), []string{"invalid-type services.api.uses[0]"}},
		{"env value mapping", svc("    env:\n      A:\n        b: 1\n"), []string{"invalid-type services.api.env.A"}},
		{"env value number is accepted", svc("    env:\n      A: 1\n"), nil},
		{"remoteBuild not bool", "name: demo\nservices:\n  f:\n    host: function\n    project: src/f\n    remoteBuild: later\n", []string{"invalid-type services.f.remoteBuild"}},
		{"k8s on containerapp", svc("    k8s: {}\n"), []string{"forbidden-property services.api.k8s"}},
		{"agent bad kind", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: robot\n", []string{"invalid-enum services.a.kind"}},
		{"agent kind reference", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: ${KIND}\n", nil},
		{"agent bad language", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    language: cobol\n", []string{"invalid-enum services.a.language"}},
		{"agent temperature type", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: prompt\n    model: gpt\n    instructions: hi\n    temperature: hot\n", []string{"invalid-type services.a.temperature"}},
		{"prompt agent blank instructions", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: prompt\n    model: gpt\n    instructions: \"   \"\n", []string{"missing-required services.a.instructions", "pattern-mismatch services.a.instructions"}},
		{"prompt agent ok", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: prompt\n    model: gpt\n    instructions: be nice\n", nil},
		{"connections on a hosted agent", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    kind: hosted\n    connections: [c]\n", []string{"forbidden-property services.a.connections"}},
		{"agent config deprecated shape stays valid", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    config:\n      kind: hosted\n", nil},
		{"agent config with bad kind", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    config:\n      kind: robot\n", []string{"invalid-enum services.a.config.kind"}},
		{"agent apiVersion forbidden", "name: demo\nservices:\n  a:\n    host: azure.ai.agent\n    project: src/a\n    apiVersion: v1\n", []string{"forbidden-property services.a.apiVersion"}},
		{"connection with project", "name: demo\nservices:\n  c:\n    host: azure.ai.connection\n    project: x\n", []string{"forbidden-property services.c.project"}},
		{"foundry legacy host forbids config", "name: demo\nservices:\n  f:\n    host: microsoft.foundry\n    config: {}\n", []string{"forbidden-property services.f.config"}},
		{"merge key hides absent host", "name: demo\nservices:\n  a:\n    <<: {host: x}\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := mustParse(t, tc.src)
			got := codePaths(errorsOf(r))
			want := slices.Clone(tc.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("errors = %v\nwant     %v", got, want)
			}
		})
	}
}

func TestIssueShapeAndOrder(t *testing.T) {
	r := mustParse(t, "name: Bad_Name\nservices:\n  a:\n    host: azure.ai.agent\n    kind: robot\n")
	if len(r.Issues) < 3 {
		t.Fatalf("issues = %+v", r.Issues)
	}
	for i := 1; i < len(r.Issues); i++ {
		a, b := r.Issues[i-1].Pos, r.Issues[i].Pos
		if a.Line > b.Line || (a.Line == b.Line && a.Column > b.Column) {
			t.Errorf("issues not sorted by position: %v then %v", a, b)
		}
	}
	for _, i := range r.Issues {
		if i.Level == LevelError && (i.Schema == "" || i.Keyword == "" || i.Message == "") {
			t.Errorf("error issue lacks schema/keyword/message: %+v", i)
		}
		if i.Pos.Line == 0 {
			t.Errorf("issue without position: %+v", i)
		}
		if i.Pointer != "" && !strings.HasPrefix(i.Pointer, "/") {
			t.Errorf("bad pointer %q", i.Pointer)
		}
	}
	var name, kind *Issue
	for k := range r.Issues {
		switch r.Issues[k].Path {
		case "name":
			name = &r.Issues[k]
		case "services.a.kind":
			kind = &r.Issues[k]
		}
	}
	if name == nil || name.Pointer != "/name" || name.Keyword != "pattern" || name.Schema != "azure.yaml.json" || name.Pos != (model.Pos{Line: 1, Column: 7}) {
		t.Errorf("name issue = %+v", name)
	}
	if kind == nil || kind.Schema != "azure.ai.agents/azure.ai.agent.json" || kind.Keyword != "enum" || kind.Pos != (model.Pos{Line: 5, Column: 11}) {
		t.Errorf("kind issue = %+v", kind)
	}
	if strings.Contains(name.Message+kind.Message, "robot") || strings.Contains(name.Message, "Bad_Name") {
		t.Errorf("messages must not echo document values: %q / %q", name.Message, kind.Message)
	}
}

func TestPathRendering(t *testing.T) {
	p := path{}.key("services").key("a.b").key("c/d").key("e~f").index(2).key("x y")
	if got, want := p.Dotted(), `services["a.b"]["c/d"]["e~f"][2]["x y"]`; got != want {
		t.Errorf("Dotted = %s, want %s", got, want)
	}
	if got, want := p.Pointer(), `/services/a.b/c~1d/e~0f/2/x y`; got != want {
		t.Errorf("Pointer = %s, want %s", got, want)
	}
	if (path{}).Dotted() != "" || (path{}).Pointer() != "" {
		t.Error("empty path should render empty")
	}
	if plainKey("") {
		t.Error("empty key is not plain")
	}
}

func TestMatchesAndHelpers(t *testing.T) {
	c := &checker{}
	if _, ok := c.matches(nil, map[string]any{"oneOf": []any{}}); ok {
		t.Error("unsupported keyword must report ok=false")
	}
	if len(typesOf(nil)) != 0 || len(typesOf([]any{"a", "b"})) != 2 {
		t.Error("typesOf")
	}
	if plural(1, "y", "ies") != "y" || plural(2, "y", "ies") != "ies" {
		t.Error("plural")
	}
	if dig(map[string]any{"a": []any{"x"}}, "a", "5") != nil || dig(1, "a") != nil || dig([]any{"x"}, "a") != nil {
		t.Error("dig out of range")
	}
	if dig(map[string]any{"a": []any{"x"}}, "a", "0") != "x" {
		t.Error("dig index")
	}
}

func TestResolveRefs(t *testing.T) {
	s, err := loadSchemas()
	if err != nil {
		t.Fatal(err)
	}
	c := &checker{s: s}
	root := ctx{doc: s.root, file: rootFile}
	got, _ := c.resolve(map[string]any{"$ref": "#/definitions/hook"}, root)
	if got["additionalProperties"] != false {
		t.Errorf("local ref did not resolve to the hook schema: %v", got)
	}
	agent := ctx{doc: s.files["azure.ai.agents/Agent.json"], file: "azure.ai.agents/Agent.json"}
	got, cx := c.resolve(map[string]any{"$ref": "FileRef.json"}, agent)
	if cx.file != "azure.ai.agents/FileRef.json" || got["required"] == nil {
		t.Errorf("sibling file ref resolved to %s", cx.file)
	}
	for _, bad := range []string{"#/definitions/nope", "missing/file.json", "https://example.invalid/x", "plain"} {
		got, _ := c.resolve(map[string]any{"$ref": bad}, root)
		if len(got) != 0 {
			t.Errorf("unresolvable ref %q should give an empty schema, got %v", bad, got)
		}
	}
}
