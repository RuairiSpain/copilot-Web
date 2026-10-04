package plan_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	. "github.com/RuairiSpain/copilot-Web/x-foundry/internal/testutil"
)

const models = `models: {default: gpt-5, allowed: [gpt-5]}`

// kbDoc is a document with one knowledge base whose fields are overridden by body.
func kbDoc(body string) []string {
	return y(models, fmt.Sprintf(`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]%s}]}`, body))
}

func kbRule(t *testing.T, code, message, body string) {
	t.Helper()
	ExpectCase(t, code, "", message, kbDoc(body)...)
}

func TestRule6DefaultModel(t *testing.T) {
	ExpectCase(t, "XF006", "models.default", "", `models: {default: gpt-9}`)
	for _, ok := range []string{
		`models: {default: gpt-5, deployments: [{name: gpt-5, model: gpt-5}]}`,
		`models: {default: gpt-5, allowed: [gpt-5]}`,
		`models: {default: chat, deployments: [{name: chat, model: gpt-5}]}`,
	} {
		MustOK(t, Run(t, ok))
	}
	ExpectCase(t, "XF006", "", "denied", `models: {default: gpt-5, deployments: [{name: gpt-5, model: gpt-5}], denied: [gpt-5]}`)
	ExpectCase(t, "XF006", "", "denied", `models: {default: chat, deployments: [{name: chat, model: gpt-5}], denied: [gpt-5]}`)
	ExpectCase(t, "XF006", "", "not in models.allowed", `models: {default: mini, allowed: [gpt-5], deployments: [{name: mini, model: gpt-5-mini}]}`)
}

func TestRule5AgentReferences(t *testing.T) {
	a := Run(t, models, `projects: [{name: fin, agents: [{name: bot, model: gpt-5, toolboxes: [nope], mcps: [nope], knowledgeBases: [nope]}]}]`)
	var kinds []string
	for _, d := range a.Diagnostics {
		for _, k := range []string{"toolbox", "MCP", "knowledge base"} {
			if d.Code == "XF005" && strings.Contains(d.Message, "unknown "+k) {
				kinds = append(kinds, k)
			}
		}
	}
	if len(kinds) != 3 {
		t.Fatalf("kinds = %v\n%s", kinds, Lines(a.Diagnostics))
	}
	ExpectCase(t, "XF005", "", "unknown model", models, `agents: [{name: bot, model: gpt-9}]`)
	ExpectCase(t, "XF112", "", "no model", `agents: [{name: bot}]`)
	MustOK(t, Run(t, models, `agents: [{name: bot}]`))
	ExpectCase(t, "XF016", "", "agent 'bot' uses denied model", `models: {denied: [gpt-4], deployments: [{name: old, model: gpt-4}]}`, `agents: [{name: bot, model: old}]`)
}

func TestSharedAgentReferencesMustResolveForEveryProject(t *testing.T) {
	a := Run(t, models,
		`agents: [{name: bot, toolboxes: [tb]}]`,
		`projects: [{name: aa, toolboxes: [{name: tb, tools: [{name: t1, type: function, reference: f}]}]}, {name: bb}]`)
	var found []diag.Diagnostic
	for _, d := range a.Diagnostics {
		if d.Code == "XF005" {
			found = append(found, d)
		}
	}
	if len(found) != 1 || !strings.Contains(found[0].Message, "project 'bb'") {
		t.Fatalf("diagnostics:\n%s", Lines(a.Diagnostics))
	}
}

func TestToolConnectorAndRouteReferences(t *testing.T) {
	a := Run(t, `toolboxes: [{name: tb, tools: [{name: t1, type: mcp, reference: ghost}, {name: t2, type: knowledgeBase, reference: ghost}]}]`)
	n := 0
	for _, d := range a.Diagnostics {
		if d.Code == "XF005" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want 2 tool reference errors:\n%s", Lines(a.Diagnostics))
	}
	a = Run(t, models, `iq: {knowledgeBases: [{name: kb1, sources: [{name: sp, type: sharepoint, site: hr, connection: ghost}], routing: {routes: [{name: r1, when: {agent: ghost}, knowledgeBase: ghost}]}}]}`)
	for _, want := range []string{"connector 'ghost'", "unknown knowledge base 'ghost'", "unknown agent 'ghost'"} {
		Expect(t, a, "XF005", "", want)
	}
}

func TestRule7EmbeddingDimensions(t *testing.T) {
	deploy := func(model string) string {
		return fmt.Sprintf(`models: {deployments: [{name: emb, model: %s}]}`, model)
	}
	index := func(model string, dims int) string {
		return fmt.Sprintf(`iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], index: {vector: {model: %s, deployment: emb, dimensions: %d}}}]}`, model, dims)
	}
	ExpectCase(t, "XF007", "", "at most 1536", deploy("text-embedding-3-small"), index("text-embedding-3-small", 3072))
	ExpectCase(t, "XF007", "", "exactly 1536", deploy("text-embedding-ada-002"), index("text-embedding-ada-002", 1024))
	MustOK(t, Run(t, deploy("text-embedding-3-small"), index("text-embedding-3-small", 1024)))
	ExpectCase(t, "XF007", "", "serves 'text-embedding-3-small'", deploy("text-embedding-3-small"), `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], index: {vector: {deployment: emb}}}]}`)
	custom := Run(t, deploy("custom-embed"), index("custom-embed", 100))
	if !custom.OK() || !Codes(custom, diag.Warning)["XF007"] {
		t.Fatalf("unknown embedding model should warn:\n%s", Lines(custom.Diagnostics))
	}
	ExpectCase(t, "XF005", "", "embedding deployment 'ghost'", `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], index: {vector: {deployment: ghost}}}]}`)
	kbRule(t, "XF007", "1536 dimensions", `, index: {fields: [{name: contentVector, type: Collection(Edm.Single), dimensions: 1536, vectorProfile: default-vector-profile, searchable: true}]}`)
}

func TestRule8FilterFields(t *testing.T) {
	kbRule(t, "XF008", "undefined field", `, retrieval: {filterFields: [department]}`)
	kbRule(t, "XF008", "not filterable", `, index: {fields: [{name: department, type: Edm.String}]}, retrieval: {filterFields: [department]}`)
	for _, body := range kbDoc(`, index: {fields: [{name: department, type: Edm.String, filterable: true}]}, retrieval: {filterFields: [department]}`) {
		_ = body
	}
	MustOK(t, Run(t, kbDoc(`, index: {fields: [{name: department, type: Edm.String, filterable: true}]}, retrieval: {filterFields: [department]}`)...))
	kbRule(t, "XF008", "filterClaims", `, access: {filterClaims: {groups: groupIds}}`)
}

func TestRule9FieldDefinitions(t *testing.T) {
	kbRule(t, "XF009", "keyField 'docId'", `, index: {keyField: docId}`)
	kbRule(t, "XF009", "contentField 'body'", `, index: {contentField: body}`)
	kbRule(t, "XF009", "titleField 'name'", `, index: {titleField: name}`)
	kbRule(t, "XF009", "vectorField 'embedding'", `, index: {vectorField: embedding}`)
	kbRule(t, "XF009", "Edm.String with key: true", `, index: {fields: [{name: id, type: Edm.Int32, key: true}]}`)
	kbRule(t, "XF009", "several key fields", `, index: {fields: [{name: id, type: Edm.String, key: true}, {name: alt, type: Edm.String, key: true}]}`)
	kbRule(t, "XF009", "searchable Edm.String", `, index: {fields: [{name: content, type: Edm.Int32}]}`)
	kbRule(t, "XF009", "undefined field 'tags'", `, index: {semantic: {keywordFields: [tags]}}`)
	MustOK(t, Run(t, kbDoc(`, index: {keyField: docId, contentField: body, titleField: name, vectorField: embedding, fields: [
		{name: docId, type: Edm.String, key: true},
		{name: body, type: Edm.String, searchable: true},
		{name: name, type: Edm.String, searchable: true},
		{name: embedding, type: Collection(Edm.Single), dimensions: 3072, vectorProfile: default-vector-profile, searchable: true}]}`)...))
}

func TestRule10VectorFields(t *testing.T) {
	kbRule(t, "XF010", "must be Collection(Edm.Single)", `, index: {fields: [{name: contentVector, type: Edm.String}]}`)
	kbRule(t, "XF010", "uses profile 'other'", `, index: {fields: [{name: contentVector, type: Collection(Edm.Single), dimensions: 3072, vectorProfile: other, searchable: true}]}`)
	if !Codes(Run(t, kbDoc(`, index: {fields: [{name: v, type: Collection(Edm.Single)}]}`)...), "")["XF102"] {
		t.Fatal("vector fields need dimensions and a profile in the schema")
	}
	kbRule(t, "XF117", "requires index.vector.enabled", `, index: {vector: {enabled: false}}`)
	MustOK(t, Run(t, kbDoc(`, index: {vector: {enabled: false}}, retrieval: {mode: keyword}`)...))
	kbRule(t, "XF117", "fallback", `, index: {vector: {enabled: false}}, retrieval: {mode: keyword}, routing: {fallback: vector}`)
}

func TestRules11And12(t *testing.T) {
	kbRule(t, "XF011", "", `, index: {chunking: {size: 256, overlap: 256}}`)
	MustOK(t, Run(t, kbDoc(`, index: {chunking: {size: 256, overlap: 255}}`)...))
	kbRule(t, "XF012", "", `, retrieval: {vectorWeight: 0.8, keywordWeight: 0.3}`)
	MustOK(t, Run(t, kbDoc(`, retrieval: {vectorWeight: 0.6, keywordWeight: 0.4}`)...))
	MustOK(t, Run(t, kbDoc(`, retrieval: {vectorWeight: 0.1, keywordWeight: 0.2, mode: vector}`)...))
}

func TestSemanticRankingAndRouting(t *testing.T) {
	kbRule(t, "XF117", "index.semantic.enabled", `, index: {semantic: {enabled: false}}`)
	ExpectCase(t, "XF117", "", "Search service", models, `search: {semanticRanking: false}`,
		`iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)
	MustOK(t, Run(t, models, `search: {semanticRanking: false}`,
		`iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}], retrieval: {semanticRanking: false}}]}`))
	kbRule(t, "XF118", "at least one route", `, routing: {strategy: explicit}`)
}

func TestRule20IQNeedsSearch(t *testing.T) {
	web := `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`
	ExpectCase(t, "XF020", "", "", Public, models, `search: {enabled: false}`, web)
	ExpectCase(t, "XF020", "", "", Public, models, `projects: [{name: fin, search: {enabled: false}, iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}}]`)
	MustOK(t, Run(t, Public, `search: {enabled: false}`))
}

func TestStorageRequirements(t *testing.T) {
	web := `iq: {knowledgeBases: [{name: kb1, sources: [{name: files, type: blob, container: policies}]}]}`
	ExpectCase(t, "XF107", "", "storage is disabled", Public, models, `storage: {enabled: false}`, web)
	adls := `iq: {knowledgeBases: [{name: kb1, sources: [{name: lake, type: adls, container: lake}]}]}`
	p := MustPlan(t, models, adls)
	if !p.Config.Storage.HierarchicalNamespace {
		t.Fatal("ADLS sources enable hierarchical namespace")
	}
	groups := map[string]bool{}
	for _, pe := range p.Config.Network.PrivateEndpoints {
		groups[pe.Group] = true
	}
	if !groups["blob"] || !groups["dfs"] {
		t.Fatalf("private endpoints = %v", p.Config.Network.PrivateEndpoints)
	}
	ExpectCase(t, "XF107", "", "hierarchicalNamespace", models, `storage: {hierarchicalNamespace: false}`, adls)
}

func TestRule16DeploymentsRespectModelSets(t *testing.T) {
	ExpectCase(t, "XF016", "", "not in models.allowed", `models: {allowed: [gpt-5], deployments: [{name: mini, model: gpt-5-mini}]}`)
	ExpectCase(t, "XF016", "", "denied model", `models: {denied: [gpt-4], deployments: [{name: old, model: gpt-4}]}`)
	MustOK(t, Run(t, `models: {allowed: [gpt-5]}`, `iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)) // embeddings are exempt
	a := RunHub(t, `hub: {name: shared, models: {denied: [gpt-4]}}`, `projects: [{name: fin, models: {deployments: [{name: old, model: gpt-4}]}}]`)
	Expect(t, a, "XF016", "projects[fin]", "denied model")
}

func TestRule113ModelPolicy(t *testing.T) {
	policy := `governance: {modelPolicy: {allowedModels: [gpt-5], deniedModels: [gpt-4], allowedSkus: [GlobalStandard]}}`
	ExpectCase(t, "XF113", "", "allowedModels", policy, `models: {deployments: [{name: mm, model: gpt-5-mini}]}`)
	ExpectCase(t, "XF113", "", "denies", `governance: {modelPolicy: {deniedModels: [gpt-4]}}`, `models: {deployments: [{name: mm, model: gpt-4}]}`)
	ExpectCase(t, "XF113", "", "allowedSkus", policy, `models: {deployments: [{name: mm, model: gpt-5, sku: Standard}]}`)
	ExpectCase(t, "XF113", "", "gateway allows", `governance: {modelPolicy: {allowedModels: [gpt-5]}}`, `models: {allowed: [gpt-5]}`,
		`gateway: {enabled: true, models: {allowed: [gpt-5, gpt-5-mini]}}`, `observability: {}`)
	MustOK(t, Run(t, policy, `models: {deployments: [{name: mm, model: gpt-5}]}`))
}

func TestRule115SpokesMayOnlyNarrow(t *testing.T) {
	hub := `hub: {name: shared, models: {allowed: [gpt-5, gpt-5-mini]}}`
	ExpectCase := func(doc ...string) {
		t.Helper()
		Expect(t, RunHub(t, doc...), "XF115", "projects[fin].models.allowed", "gpt-4")
	}
	ExpectCase(hub, `projects: [{name: fin, models: {allowed: [gpt-5, gpt-4]}}]`)
	MustOK(t, RunHub(t, hub, `projects: [{name: fin, models: {allowed: [gpt-5]}}]`))
	Expect(t, Run(t, `models: {allowed: [gpt-5]}`, `projects: [{name: fin, models: {allowed: [gpt-4]}}]`), "XF115", "", "")
}

func TestRule104RequiredTags(t *testing.T) {
	ExpectCase(t, "XF104", "projects[finance].tags", "cost-center", `governance: {requiredTags: [environment, project, managed-by, cost-center]}`)
	MustOK(t, Run(t, `defaults: {tags: {cost-center: "1"}}`, `governance: {requiredTags: [cost-center]}`))
	MustOK(t, Run(t, `governance: {enabled: false, requiredTags: [cost-center]}`))
}

func TestRule126DataResidency(t *testing.T) {
	residency := `governance: {dataResidency: [westeurope]}`
	ExpectCase(t, "XF126", "deployments[chat].sku", "", `defaults: {location: westeurope}`, residency,
		`models: {deployments: [{name: chat, model: gpt-5, sku: GlobalStandard}]}`)
	MustOK(t, Run(t, `defaults: {location: westeurope}`, residency, `models: {deployments: [{name: chat, model: gpt-5, sku: DataZoneStandard}]}`))
	p := MustPlan(t, `defaults: {location: westeurope}`, residency, `models: {allowed: [gpt-5]}`,
		`iq: {knowledgeBases: [{name: kb1, sources: [{name: s1, type: web, url: "https://x.example"}]}]}`)
	for _, d := range p.Config.Scope("root").Models.Deployments {
		if d.SKU != "DataZoneStandard" {
			t.Fatalf("%s uses %s", d.Name, d.SKU)
		}
	}
	plain := MustPlan(t, `models: {allowed: [gpt-5]}`)
	if got := plain.Config.Scope("root").Models.Deployments[0].SKU; got != "GlobalStandard" {
		t.Fatalf("sku = %s", got)
	}
}

func TestModelDeploymentsPinTheirVersionByDefault(t *testing.T) {
	p := MustPlan(t, `models: {allowed: [gpt-5, gpt-5-mini], deployments: [{name: chat, model: gpt-5}, {name: auto, model: gpt-5-mini, versionUpgradeOption: OnceNewDefaultVersionAvailable}]}`)
	got := map[string]string{}
	for _, d := range p.Config.Scope("root").Models.Deployments {
		got[d.Name] = d.VersionUpgradeOption
	}
	if got["chat"] != "NoAutoUpgrade" || got["auto"] != "OnceNewDefaultVersionAvailable" {
		t.Fatalf("upgrade options = %v", got)
	}
	implicit := MustPlan(t, `models: {allowed: [gpt-5]}`)
	if got := implicit.Config.Scope("root").Models.Deployments[0].VersionUpgradeOption; got != "NoAutoUpgrade" {
		t.Fatalf("implicit deployment upgrade option = %s", got)
	}
}

// Gateway ----------------------------------------------------------------------------------

func gwDoc(gateway string, extra ...string) []string {
	return append(y(`models: {default: gpt-5, allowed: [gpt-5, gpt-5-mini]}`,
		`projects: [{name: fin, agents: [{name: bot, instructions: x}]}]`,
		`observability: {}`,
		fmt.Sprintf(`gateway: {enabled: true, authentication: {audiences: ["api://gw"]}%s}`, gateway)), extra...)
}

const ep = `{name: ep1, path: /a, target: bot, targetType: agent}`

func TestValidGateway(t *testing.T) {
	MustOK(t, Run(t, gwDoc(`, endpoints: [`+ep+`]`)...))
}

func TestRule13GatewayPaths(t *testing.T) {
	ExpectCase(t, "XF013", "", "used more than once", gwDoc(fmt.Sprintf(`, endpoints: [%s, {name: ep2, path: /a, target: bot, targetType: agent}]`, ep))...)
	ExpectCase(t, "XF013", "gateway.path", "already used", models, `observability: {}`,
		`gateway: {enabled: true, endpoints: [{name: ep1, path: /fin, target: gpt-5, targetType: model}]}`, `projects: [{name: fin, gateway: {path: /fin}}]`)
	ExpectCase(t, "XF013", "gateway.path", "", `observability: {}`, `gateway: {enabled: true}`,
		`projects: [{name: aa, gateway: {path: /x}}, {name: bb, gateway: {path: /x}}]`)
}

func TestRule14GatewayTargets(t *testing.T) {
	for _, c := range []struct{ endpoint, message string }{
		{`{name: ep1, path: /a, target: ghost, targetType: agent}`, "agent 'ghost' does not exist"},
		{`{name: ep1, path: /a, target: ghost, targetType: agent, project: fin}`, "in project 'fin'"},
		{`{name: ep1, path: /a, target: ghost, targetType: runtime}`, "runtime 'ghost'"},
		{`{name: ep1, path: /a, target: ghost, targetType: model}`, "model 'ghost'"},
		{`{name: ep1, path: /a, target: ghost, targetType: search}`, "search 'ghost'"},
		{`{name: ep1, path: /a, target: ghost, targetType: knowledgeBase}`, "knowledge base 'ghost'"},
	} {
		ExpectCase(t, "XF014", "", c.message, gwDoc(`, endpoints: [`+c.endpoint+`]`)...)
	}
	MustOK(t, Run(t,
		`models: {default: gpt-5, allowed: [gpt-5]}`, `observability: {}`, `search: {name: srch-main}`,
		`runtime: {enabled: true, image: "i:1", name: rt-main, project: fin}`,
		`iq: {knowledgeBases: [{name: policies, sources: [{name: files, type: blob, container: policies}]}]}`,
		`projects: [{name: fin, agents: [{name: bot, instructions: x}]}]`,
		`gateway: {enabled: true, authentication: {audiences: ["api://gw"]}, endpoints: [`+ep+`,
			{name: ep2, path: /m, target: gpt-5, targetType: model},
			{name: ep3, path: /s, target: srch-main, targetType: search},
			{name: ep4, path: /r, target: rt-main, targetType: runtime},
			{name: ep5, path: /k, target: policies, targetType: knowledgeBase, project: fin}]}`))
	ExpectCase(t, "XF014", "", "several projects", models, `observability: {}`,
		`projects: [{name: aa, agents: [{name: bot}]}, {name: bb, agents: [{name: bot}]}]`, `gateway: {enabled: true, endpoints: [`+ep+`]}`)
	MustOK(t, Run(t, models, `observability: {}`, `agents: [{name: bot}]`, `projects: [{name: aa}, {name: bb}]`, `gateway: {enabled: true, endpoints: [`+ep+`]}`))
}

func TestRule15GatewayProfiles(t *testing.T) {
	a := Run(t, gwDoc(`, endpoints: [{name: ep1, path: /a, target: bot, targetType: agent, quotaProfile: qq, limitProfile: ll, routingProfile: rr}]`)...)
	n := 0
	for _, d := range a.Diagnostics {
		if d.Code == "XF015" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("want 3 XF015:\n%s", Lines(a.Diagnostics))
	}
	MustOK(t, Run(t, gwDoc(`, endpoints: [{name: ep1, path: /a, target: bot, targetType: agent, quotaProfile: qq, limitProfile: ll, routingProfile: b1}],
		quotas: {profiles: [{name: qq, scope: user, dailyTokens: 1000}]}, limits: {profiles: [{name: ll}]}, routing: {backends: [{name: b1, target: x}]}`)...))
	ExpectCase(t, "XF015", "gateway.quotaProfile", "", models, `observability: {}`, `gateway: {enabled: true}`, `projects: [{name: fin, gateway: {quotaProfile: ghost}}]`)
}

func TestGatewayRegistrationRequiresEnabledGateway(t *testing.T) {
	ExpectCase(t, "XF111", "", "gateway.enabled", `projects: [{name: fin, gateway: {}}]`)
	disabled := Run(t, `gateway: {enabled: false, endpoints: [`+ep+`]}`)
	if !disabled.OK() || !Codes(disabled, diag.Warning)["XF111"] {
		t.Fatalf("expected an XF111 warning:\n%s", Lines(disabled.Diagnostics))
	}
}

func TestGatewayTrackingChargebackAndTelemetry(t *testing.T) {
	ExpectCase(t, "XF111", "", "chargeback requires", gwDoc(`, tokenTracking: {enabled: false}`)...)
	ExpectCase(t, "XF111", "", "endpoint enables", gwDoc(`, tokenTracking: {enabled: false}, chargeback: {enabled: false}, endpoints: [{name: ep1, path: /a, target: bot, targetType: agent, tokenTracking: true}]`)...)
	MustOK(t, Run(t, gwDoc(`, tokenTracking: {enabled: false}, chargeback: {enabled: false}`)...))
	noObs := gwDoc("")
	noObs = append(noObs[:2:2], noObs[3:]...) // drop `observability: {}`
	MustOK(t, Run(t, noObs...))               // implicit observability
	ExpectCase(t, "XF111", "", "logAnalytics", append(gwDoc(""), `observability: {enabled: false}`)...)
}

func TestGatewayModels(t *testing.T) {
	ExpectCase(t, "XF006", "", "not in gateway.models.allowed", gwDoc(`, models: {default: gpt-5, allowed: [gpt-5-mini]}`)...)
	ExpectCase(t, "XF006", "", "denied", gwDoc(`, models: {default: gpt-5, denied: [gpt-5]}`)...)
	ExpectCase(t, "XF005", "", "no deployment serves it", gwDoc(`, models: {allowed: [gpt-9]}`)...)
	ExpectCase(t, "XF021", "", "internalOnly", gwDoc(`, security: {internalOnly: false}`)...)
}

func TestGatewayDefaultsAreDerived(t *testing.T) {
	p := MustPlan(t, models, `gateway: {enabled: true}`)
	g := p.Config.Gateway
	if got := g.Authentication.Audiences; len(got) != 1 || got[0] != "api://x-foundry-gateway" {
		t.Fatalf("audiences = %v", got)
	}
	if g.Models.Default != "gpt-5" || len(g.Models.Allowed) != 1 {
		t.Fatalf("models = %+v", g.Models)
	}
	for _, kind := range []string{"gateway-authentication", "observability"} {
		found := false
		for _, i := range p.Config.Implicit {
			found = found || i.Kind == kind
		}
		if !found {
			t.Fatalf("missing implicit %s", kind)
		}
	}
	prefixed := MustPlan(t, `defaults: {namingPrefix: ent}`, `gateway: {enabled: true}`)
	if got := prefixed.Config.Gateway.Authentication.Audiences[0]; got != "api://ent-gateway" {
		t.Fatalf("audience = %s", got)
	}
}

func TestGatewayChargebackDimensionsAreTracked(t *testing.T) {
	p := MustPlan(t, gwDoc(`, chargeback: {dimensions: [project, department]}`)...)
	tr := p.Config.Gateway.TokenTracking
	found := false
	for _, d := range tr.Dimensions {
		found = found || d == "department"
	}
	if !found || tr.DepartmentClaim != "department" {
		t.Fatalf("tracking = %+v", tr)
	}
	custom := MustPlan(t, gwDoc(`, tokenTracking: {departmentClaim: dept}, chargeback: {dimensions: [department]}`)...)
	if got := custom.Config.Gateway.TokenTracking.DepartmentClaim; got != "dept" {
		t.Fatalf("claim = %s", got)
	}
	explicit := MustPlan(t, gwDoc(`, models: {default: gpt-5, allowed: [gpt-5]}`)...)
	if got := explicit.Config.Gateway.Models.Allowed; len(got) != 1 {
		t.Fatalf("allowed = %v", got)
	}
}
