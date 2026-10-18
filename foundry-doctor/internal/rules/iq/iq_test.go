package iq

import (
	"context"
	"strings"
	"testing"

	runtimeiq "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/iq"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type snapshotModel struct {
	resources []sdk.ARMResource
	snap      runtimeiq.Snapshot
}

func (m snapshotModel) Resources() []sdk.ARMResource   { return m.resources }
func (m snapshotModel) IQSnapshot() runtimeiq.Snapshot { return m.snap }

type policy map[string]any

func (p policy) Get(k string) (any, bool) { v, ok := p[k]; return v, ok }

func ruleByID(id string) sdk.Rule {
	for _, r := range Register() {
		if r.ID() == id {
			return r
		}
	}
	return nil
}

func mustEval(t *testing.T, id string, in *sdk.Input) sdk.Result {
	t.Helper()
	out, err := ruleByID(id).Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// inputFor models a run where the opt-in data-plane probe read the snapshot.
// Objects without an explicit API version get the verified preview version.
func inputFor(snap runtimeiq.Snapshot) *sdk.Input {
	snap.LiveRead = true
	const v = "2026-08-01-preview"
	for k, x := range snap.Sources {
		if x.APIVersion == "" {
			x.APIVersion = v
			snap.Sources[k] = x
		}
	}
	for k, x := range snap.Indexes {
		if x.APIVersion == "" {
			x.APIVersion = v
			snap.Indexes[k] = x
		}
	}
	for k, x := range snap.Skillsets {
		if x.APIVersion == "" {
			x.APIVersion = v
			snap.Skillsets[k] = x
		}
	}
	for k, x := range snap.Bases {
		if x.APIVersion == "" {
			x.APIVersion = v
			snap.Bases[k] = x
		}
	}
	return &sdk.Input{ARM: snapshotModel{snap: snap}}
}

// localInput models --local or a run without --iq-metadata: no data plane.
func localInput(snap runtimeiq.Snapshot) *sdk.Input {
	snap.LiveRead = false
	return &sdk.Input{ARM: snapshotModel{snap: snap}}
}

func TestRegisterIDs(t *testing.T) {
	want := []string{
		"FND-IQ-001", "FND-IQ-002", "FND-IQ-003", "FND-IQ-004", "FND-IQ-005", "FND-IQ-006",
		"FND-IQ-007", "FND-IQ-008", "FND-IQ-009", "FND-IQ-010", "FND-IQ-011", "FND-IQ-012",
	}
	rs := Register()
	if len(rs) != len(want) {
		t.Fatalf("got %d rules want %d", len(rs), len(want))
	}
	for i, r := range rs {
		if r.ID() != want[i] {
			t.Fatalf("rule[%d]=%s want %s", i, r.ID(), want[i])
		}
	}
}

func TestIQ001FilterFields(t *testing.T) {
	idx := runtimeiq.Index{Name: "docs", Fields: []runtimeiq.Field{
		{Name: "content", Type: "Edm.String", Searchable: true, Retrievable: true},
		{Name: "acl", Type: "Edm.String", Filterable: false},
	}}
	src := runtimeiq.KnowledgeSource{Name: "src", SearchIndexName: "docs", FilterFields: []string{"acl"}}
	if got := mustEval(t, "FND-IQ-001", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": idx},
	})); len(got.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", got)
	}
	idx.Fields[1].Filterable = true
	idx.Fields = append(idx.Fields, runtimeiq.Field{Name: "sec", Type: "Edm.String", Filterable: true})
	src.BaseFilter = "sec eq true"
	src.SecurityField = "sec"
	if got := mustEval(t, "FND-IQ-001", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": idx},
	})); len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("expected pass, got %+v", got)
	}
}

func TestIQ002IndexShape(t *testing.T) {
	src := runtimeiq.KnowledgeSource{Name: "src", SearchIndexName: "docs"}
	bad := runtimeiq.Index{Name: "docs", Fields: []runtimeiq.Field{
		{Name: "id", Type: "Edm.Int32", Key: true},
	}}
	if got := mustEval(t, "FND-IQ-002", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": bad},
	})); len(got.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", got)
	}
	good := runtimeiq.Index{
		Name: "docs",
		Fields: []runtimeiq.Field{
			{Name: "id", Type: "Edm.String", Key: true},
			{Name: "content", Type: "Edm.String", Searchable: true, Retrievable: true},
			{Name: "vec", Type: "Collection(Edm.Single)", Searchable: true, Dimensions: 1536, VectorSearchProfile: "p1"},
		},
		DefaultSemanticConfiguration: "default",
		SemanticConfigurations:       []runtimeiq.SemanticConfiguration{{Name: "default", ContentFields: []string{"content"}}},
		VectorProfiles:               []runtimeiq.VectorProfile{{Name: "p1", Algorithm: "a1", Vectorizer: "v1"}},
		VectorAlgorithms:             []string{"a1"},
		Vectorizers:                  []runtimeiq.Vectorizer{{Name: "v1", Kind: "azureOpenAI", ModelName: "text-embedding-ada-002"}},
	}
	if got := mustEval(t, "FND-IQ-002", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": good},
	})); len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("expected pass, got %+v", got)
	}
}

func TestIQ003VectorFieldChecks(t *testing.T) {
	valid := runtimeiq.Index{
		Name:             "good",
		Fields:           []runtimeiq.Field{{Name: "vec", Type: "Collection(Edm.Single)", Searchable: true, Dimensions: 1536, VectorSearchProfile: "p1"}},
		VectorProfiles:   []runtimeiq.VectorProfile{{Name: "p1", Algorithm: "a1", Vectorizer: "v1"}},
		VectorAlgorithms: []string{"a1"},
		Vectorizers:      []runtimeiq.Vectorizer{{Name: "v1", Kind: "azureOpenAI", ModelName: "text-embedding-ada-002"}},
	}
	invalid := runtimeiq.Index{
		Name:   "bad",
		Fields: []runtimeiq.Field{{Name: "vec", Type: "Collection(Edm.String)", Searchable: true, Dimensions: 3072, VectorSearchProfile: "missing"}},
	}
	got := mustEval(t, "FND-IQ-003", inputFor(runtimeiq.Snapshot{
		Indexes: map[string]runtimeiq.Index{"good": valid, "bad": invalid},
	}))
	if len(got.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", got)
	}
	if clean := mustEval(t, "FND-IQ-003", inputFor(runtimeiq.Snapshot{
		Indexes: map[string]runtimeiq.Index{"good": valid},
	})); len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ004EmbeddingConsistency(t *testing.T) {
	src := runtimeiq.KnowledgeSource{Name: "src", SearchIndexName: "docs", SkillsetName: "chunker"}
	idx := runtimeiq.Index{
		Name:             "docs",
		Fields:           []runtimeiq.Field{{Name: "vec", Type: "Collection(Edm.Single)", Searchable: true, Dimensions: 1536, VectorSearchProfile: "p1"}},
		VectorProfiles:   []runtimeiq.VectorProfile{{Name: "p1", Algorithm: "a1", Vectorizer: "v1"}},
		VectorAlgorithms: []string{"a1"},
		Vectorizers: []runtimeiq.Vectorizer{{
			Name: "v1", Kind: "azureOpenAI", ModelName: "text-embedding-3-large", DeploymentID: "embed", ResourceURI: "https://acct.openai.azure.com",
		}},
	}
	ss := runtimeiq.Skillset{Name: "chunker", Skills: []runtimeiq.Skill{{ODataType: "#Microsoft.Skills.Text.AzureOpenAIEmbeddingSkill", Dimensions: 3072}}}
	got := mustEval(t, "FND-IQ-004", inputFor(runtimeiq.Snapshot{
		Sources:   map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes:   map[string]runtimeiq.Index{"docs": idx},
		Skillsets: map[string]runtimeiq.Skillset{"chunker": ss},
		Deployments: map[string]runtimeiq.EmbeddingDeployment{
			"acct/embed": {Name: "embed", AccountName: "acct", ModelName: "text-embedding-ada-002"},
		},
	}))
	if len(got.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", got)
	}
	ss.Skills[0].Dimensions = 1536
	got = mustEval(t, "FND-IQ-004", inputFor(runtimeiq.Snapshot{
		Sources:   map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes:   map[string]runtimeiq.Index{"docs": idx},
		Skillsets: map[string]runtimeiq.Skillset{"chunker": ss},
		Deployments: map[string]runtimeiq.EmbeddingDeployment{
			"acct/embed": {Name: "embed", AccountName: "acct", ModelName: "text-embedding-3-large"},
		},
	}))
	if len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("expected pass, got %+v", got)
	}
}

func TestIQ005KnowledgeBaseReasoningAndLimits(t *testing.T) {
	got := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-04-01", RetrievalReasoningKind: "auto", KnowledgeSources: []string{"a", "b", "c", "d"}, Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI"}}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "free", APIVersion: "2025-05-01"},
		},
	}))
	if len(got.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", RetrievalReasoningKind: "minimal", OutputMode: "extractiveData"},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "basic", APIVersion: "2025-05-01"},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}

	uncertain := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", RetrievalReasoningKind: "medium", KnowledgeSources: []string{"a", "b", "c", "d", "e", "f"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "basic", Location: "moonbase"},
		},
	}))
	if uncertain.Skipped == nil || !strings.Contains(uncertain.Skipped.Reason, "undocumented region") || !strings.Contains(uncertain.Skipped.Reason, "creation date") {
		t.Fatalf("expected uncertain result, got %+v", uncertain)
	}

	fail := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", KnowledgeSources: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "basic", Location: "eastus"},
		},
	}))
	if len(fail.Findings) != 1 {
		t.Fatalf("expected hard limit finding, got %+v", fail)
	}

	webMissingModel := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", UsesWebKnowledgeSource: true},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "basic", Location: "eastus"},
		},
	}))
	if len(webMissingModel.Findings) != 1 || !strings.Contains(webMissingModel.Findings[0].Evidence, "web knowledge sources require at least one model") {
		t.Fatalf("expected web-model finding, got %+v", webMissingModel)
	}

	freeTierModel := mustEval(t, "FND-IQ-005", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI"}}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", SKU: "free", Location: "eastus"},
		},
	}))
	if len(freeTierModel.Findings) != 1 || !strings.Contains(freeTierModel.Findings[0].Evidence, "Basic tier or higher") {
		t.Fatalf("expected free-tier model finding, got %+v", freeTierModel)
	}
}

func TestIQ006ChunkingBounds(t *testing.T) {
	got := mustEval(t, "FND-IQ-006", inputFor(runtimeiq.Snapshot{
		Skillsets: map[string]runtimeiq.Skillset{
			"ss": {Name: "ss", Skills: []runtimeiq.Skill{{ODataType: "#SplitSkill", TextSplitMode: "pages", MaximumPageLength: 100, PageOverlapLength: 60}}},
		},
	}))
	if len(got.Findings) != 2 {
		t.Fatalf("expected two findings, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-006", inputFor(runtimeiq.Snapshot{
		Skillsets: map[string]runtimeiq.Skillset{
			"ss": {Name: "ss", Skills: []runtimeiq.Skill{{ODataType: "#SplitSkill", TextSplitMode: "pages", MaximumPageLength: 1200, PageOverlapLength: 100}}},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
	none := mustEval(t, "FND-IQ-006", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{"svc/kb": {Name: "kb"}},
	}))
	if none.Skipped == nil || !strings.Contains(none.Skipped.Reason, "no-explicit-query-definitions") {
		t.Fatalf("expected skip without split skill, got %+v", none)
	}
}

func TestIQ007SupportedKindsAndModels(t *testing.T) {
	got := mustEval(t, "FND-IQ-007", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"src": {Name: "src", Kind: "indexedSharePoint", APIVersion: "2026-04-01"},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-04-01", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", ModelName: "gpt-5.6-sol", Path: "models[0]"}}},
		},
	}))
	if len(got.Findings) != 2 {
		t.Fatalf("expected two findings, got %+v", got)
	}
	uncertain := mustEval(t, "FND-IQ-007", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"src": {Name: "src", Kind: "searchIndex", APIVersion: "2099-01-01"},
		},
	}))
	if uncertain.Skipped == nil || !strings.Contains(uncertain.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain result for unknown version, got %+v", uncertain)
	}
	clean := mustEval(t, "FND-IQ-007", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"src": {Name: "src", Kind: "indexedSharePoint", APIVersion: "2026-08-01-preview"},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-08-01-preview", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", ModelName: "gpt-5.6-sol"}}},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func kbConn(name, auth string) runtimeiq.Connection {
	return runtimeiq.Connection{Name: name, TargetKind: runtimeiq.ConnectionTargetKnowledgeBaseMCP, AuthType: auth}
}

func TestIQ008ManagedIdentityOnly(t *testing.T) {
	got := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"blob": {Name: "blob", Kind: "azureBlob", HasSecretConnection: true},
			"lake": {Name: "lake", Kind: "azureBlob", ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs"},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", KnowledgeSources: []string{"blob", "lake"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", IdentityType: "None"},
		},
		Connections: []runtimeiq.Connection{kbConn("iq", "ApiKey")},
	}))
	if len(got.Findings) != 3 {
		t.Fatalf("expected secret, identity and auth findings, got %+v", got)
	}
	for _, f := range got.Findings {
		if f.Key == "" || !strings.Contains(f.Evidence, "[") {
			t.Fatalf("finding without key or plane evidence: %+v", f)
		}
		if strings.Contains(strings.ToLower(f.Evidence), "supersecret") {
			t.Fatalf("evidence leaked secret: %+v", f)
		}
	}
	clean := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"blob": {Name: "blob", Kind: "azureBlob", ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs"},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", KnowledgeSources: []string{"blob"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", IdentityType: "SystemAssigned"},
		},
		Connections: []runtimeiq.Connection{kbConn("iq", "ProjectManagedIdentity")},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ008ModelKeys(t *testing.T) {
	withKey := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", APIKeySet: true, Path: "models[0].azureOpenAIParameters"}}},
		},
		Indexes: map[string]runtimeiq.Index{
			"docs": {Name: "docs", Vectorizers: []runtimeiq.Vectorizer{{Name: "v1", Kind: "azureOpenAI", APIKeySet: true}}},
		},
		Sources: map[string]runtimeiq.KnowledgeSource{
			"blob": {Name: "blob", Kind: "azureBlob", Models: []runtimeiq.ModelRef{{Kind: "aiServices", APIKeySet: true, Path: "azureBlobParameters.ingestionParameters.aiServices"}}},
		},
	}))
	if len(withKey.Findings) != 3 {
		t.Fatalf("expected three apiKey findings, got %+v", withKey)
	}
	noKey := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", Path: "models[0].azureOpenAIParameters"}}},
		},
	}))
	if len(noKey.Findings) != 0 || noKey.Skipped == nil || !strings.Contains(noKey.Skipped.Reason, "not proof of keyless") {
		t.Fatalf("expected uncertain without apiKey evidence, got %+v", noKey)
	}
	identity := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", AuthIdentitySet: true}}},
		},
	}))
	if len(identity.Findings) != 0 || identity.Skipped != nil {
		t.Fatalf("expected pass with authIdentity, got %+v", identity)
	}
	redacted := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"sp": {Name: "sp", Kind: "indexedSharePoint", ConnectionUnclassified: true},
		},
	}))
	if len(redacted.Findings) != 0 || redacted.Skipped == nil || !strings.Contains(redacted.Skipped.Reason, "redacted") {
		t.Fatalf("expected uncertain for redacted connection, got %+v", redacted)
	}
}

func TestIQ008WithoutDataPlane(t *testing.T) {
	skip := mustEval(t, "FND-IQ-008", localInput(runtimeiq.Snapshot{
		Connections: []runtimeiq.Connection{kbConn("iq", "ProjectManagedIdentity")},
	}))
	if len(skip.Findings) != 0 || skip.Skipped == nil || !strings.Contains(skip.Skipped.Reason, "data-plane metadata") {
		t.Fatalf("expected skip naming the data plane, got %+v", skip)
	}
	conn := runtimeiq.Connection{Name: "iq", TargetKind: runtimeiq.ConnectionTargetKnowledgeBaseMCP, AuthType: "ApiKey", Category: "CustomKeys", Audience: "https://example.invalid"}
	fail := mustEval(t, "FND-IQ-008", localInput(runtimeiq.Snapshot{Connections: []runtimeiq.Connection{conn}}))
	if len(fail.Findings) != 3 {
		t.Fatalf("expected auth, category and audience findings, got %+v", fail)
	}
	unresolved := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Connections: []runtimeiq.Connection{{Name: "iq", TargetKind: runtimeiq.ConnectionTargetUnresolved}},
	}))
	if unresolved.Skipped == nil || !strings.Contains(unresolved.Skipped.Reason, "unresolved") {
		t.Fatalf("expected uncertain for unresolved target, got %+v", unresolved)
	}
}

func permissionSource(name, version string, options ...string) runtimeiq.KnowledgeSource {
	return runtimeiq.KnowledgeSource{
		Name: name, Kind: "azureBlob", APIVersion: version, ParametersRead: true, IngestionRead: true,
		IngestionPermissionOptions: options,
	}
}

func requireAccess(snap runtimeiq.Snapshot) *sdk.Input {
	in := inputFor(snap)
	in.Policy = policy{"knowledge.requireDocumentLevelAccess": true}
	return in
}

func TestIQ009DocumentAccessRequirement(t *testing.T) {
	notDeclared := mustEval(t, "FND-IQ-009", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"s1": permissionSource("s1", "2026-04-01")},
	}))
	if notDeclared.Skipped == nil || notDeclared.Skipped.Reason != "no-access-requirement-declared" {
		t.Fatalf("expected no-requirement skip, got %+v", notDeclared)
	}
	got := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"s1": permissionSource("s1", "2026-04-01")},
	}))
	if len(got.Findings) != 2 {
		t.Fatalf("expected options and api-version findings, got %+v", got)
	}
	headerOnly := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"s1": permissionSource("s1", "2026-08-01-preview", "userIds", "groupIds"),
			"s2": {Name: "s2", Kind: "remoteSharePoint", APIVersion: "2026-08-01-preview"},
		},
		Connections: []runtimeiq.Connection{kbConn("iq", "ProjectManagedIdentity")},
	}))
	if len(headerOnly.Findings) != 0 || headerOnly.Skipped == nil || !strings.Contains(headerOnly.Skipped.Reason, "header-forwarding-configured-outside-repo") {
		t.Fatalf("expected the header leg to stay uncertain, got %+v", headerOnly)
	}
	asset := permissionSource("s1", "2026-08-01-preview", "userIds")
	asset.AssetStorePresent = true
	assetStore := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{Sources: map[string]runtimeiq.KnowledgeSource{"s1": asset}}))
	if len(assetStore.Findings) != 1 || assetStore.Findings[0].Key != "asset-store" {
		t.Fatalf("expected asset store finding, got %+v", assetStore)
	}
	badOption := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"s1": permissionSource("s1", "2026-08-01-preview", "ownerIds")},
	}))
	if len(badOption.Findings) != 1 || !strings.Contains(badOption.Findings[0].Evidence, "documented values") {
		t.Fatalf("expected enum finding, got %+v", badOption)
	}
	for _, version := range []string{"2026-05-01-preview", "2027-01-01-preview"} {
		res := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{"s1": permissionSource("s1", version, "userIds")},
		}))
		if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "uncertain:") {
			t.Fatalf("version %s: expected uncertain, got %+v", version, res)
		}
	}
	missingParams := mustEval(t, "FND-IQ-009", requireAccess(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"s1": {Name: "s1", Kind: "indexedSharePoint", APIVersion: "2026-08-01-preview"}},
	}))
	if len(missingParams.Findings) != 0 || missingParams.Skipped == nil || !strings.Contains(missingParams.Skipped.Reason, "indexedSharePointParameters.ingestionParameters") {
		t.Fatalf("expected uncertain when the parameters object is absent, got %+v", missingParams)
	}
	local := localInput(runtimeiq.Snapshot{Connections: []runtimeiq.Connection{kbConn("iq", "ProjectManagedIdentity")}})
	local.Policy = policy{"knowledge.requireDocumentLevelAccess": true}
	if res := mustEval(t, "FND-IQ-009", local); res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "data-plane metadata") {
		t.Fatalf("expected data-plane skip in local mode, got %+v", res)
	}
}

func TestIQ010ADLSRequiresHNS(t *testing.T) {
	source := runtimeiq.KnowledgeSource{
		Name: "lake", IsADLSGen2: true,
		ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs",
	}
	got := mustEval(t, "FND-IQ-010", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"lake": source},
		Storage: map[string]runtimeiq.StorageAccount{"docs": {Name: "docs", IsHNSEnabled: boolPtr(false)}},
	}))
	if len(got.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-010", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"lake": source},
		Storage: map[string]runtimeiq.StorageAccount{"docs": {Name: "docs", IsHNSEnabled: boolPtr(true)}},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ011ScheduleValidation(t *testing.T) {
	got := mustEval(t, "FND-IQ-011", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"bad": {Name: "bad", RefreshSchedule: &runtimeiq.Schedule{Interval: "PT4M", StartTime: "2026-01-01T00:00:00+01:00"}},
		},
	}))
	if len(got.Findings) != 2 {
		t.Fatalf("expected two findings, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-011", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"ok": {Name: "ok", RefreshSchedule: &runtimeiq.Schedule{Interval: "PT10M", StartTime: "2026-01-01T00:00:00Z"}},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ012SemanticCapacityAndVersion(t *testing.T) {
	bases := map[string]runtimeiq.KnowledgeBase{"svc/kb": {Name: "kb", KnowledgeSources: []string{"src"}}}
	sources := map[string]runtimeiq.KnowledgeSource{"src": {Name: "src", SearchIndexName: "docs"}}
	got := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Bases:   bases,
		Sources: sources,
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "disabled"},
		},
	}))
	if len(got.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", got)
	}
	uncertain := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Bases:   bases,
		Sources: sources,
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2099-01-01", SemanticSearch: "standard"},
		},
	}))
	if uncertain.Skipped == nil || !strings.Contains(uncertain.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain, got %+v", uncertain)
	}
	clean := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Bases:   bases,
		Sources: sources,
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2026-03-01-preview", SemanticSearch: "standard", SKU: "basic", KnowledgeRetrieval: "standard"},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
	unset := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Bases:    bases,
		Sources:  sources,
		Services: map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SKU: "basic"}},
	}))
	if unset.Skipped == nil || !strings.Contains(unset.Skipped.Reason, "management-api-version-without-property") {
		t.Fatalf("expected uncertain when semanticSearch is not set, got %+v", unset)
	}
	noSemantic := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Bases:    map[string]runtimeiq.KnowledgeBase{"svc/kb": {Name: "kb", KnowledgeSources: []string{"web"}}},
		Sources:  map[string]runtimeiq.KnowledgeSource{"web": {Name: "web", Kind: "web"}},
		Services: map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "disabled"}},
	}))
	if len(noSemantic.Findings) != 0 || noSemantic.Skipped == nil || noSemantic.Skipped.Reason != "no-index-backed-knowledge-source" {
		t.Fatalf("expected skip, not pass, without an index-backed source, got %+v", noSemantic)
	}
	local := mustEval(t, "FND-IQ-012", localInput(runtimeiq.Snapshot{
		Services: map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "disabled"}},
	}))
	if len(local.Findings) != 0 || local.Skipped == nil || !strings.Contains(local.Skipped.Reason, "data-plane metadata") {
		t.Fatalf("expected data-plane skip in local mode, got %+v", local)
	}
	freeLocal := mustEval(t, "FND-IQ-012", localInput(runtimeiq.Snapshot{
		Services: map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "standard", SKU: "free"}},
	}))
	if len(freeLocal.Findings) != 1 {
		t.Fatalf("expected the template-only tier finding, got %+v", freeLocal)
	}

	probeFailure := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Services:    map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2026-03-01-preview"}},
		ProbeIssues: []string{"knowledge base metadata could not be read for connection iq"},
	}))
	if probeFailure.Skipped == nil || !strings.Contains(probeFailure.Skipped.Reason, "knowledge base metadata could not be read") {
		t.Fatalf("expected probe failure skip, got %+v", probeFailure)
	}
}

// Every IQ rule must fail safe for an object read with an unknown API
// version: uncertain, never a finding and never a pass.
func TestIQRulesUnknownAPIVersionIsUncertain(t *testing.T) {
	const v = "2027-01-01-preview"
	schedule := &runtimeiq.Schedule{Interval: "PT10M"}
	snap := runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"s1": {
			Name: "s1", Kind: "azureBlob", APIVersion: v, SearchIndexName: "docs", SkillsetName: "ss",
			FilterFields: []string{"missing"}, IsADLSGen2: true, ParametersRead: true, IngestionRead: true,
			ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs",
			RefreshSchedule:      schedule,
		}},
		Indexes: map[string]runtimeiq.Index{"docs": {Name: "docs", APIVersion: v, Fields: []runtimeiq.Field{{Name: "vec", Type: "Collection(Edm.String)", Dimensions: 1}}}},
		Skillsets: map[string]runtimeiq.Skillset{"ss": {Name: "ss", APIVersion: v, Skills: []runtimeiq.Skill{
			{ODataType: "#SplitSkill", TextSplitMode: "pages", MaximumPageLength: 1, PageOverlapLength: 1},
		}}},
		Bases:    map[string]runtimeiq.KnowledgeBase{"svc/kb": {Name: "kb", APIVersion: v, KnowledgeSources: []string{"s1"}, Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", APIKeySet: true}}}},
		Services: map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "standard", SKU: "basic", Location: "eastus"}},
		Storage:  map[string]runtimeiq.StorageAccount{"docs": {Name: "docs", IsHNSEnabled: boolPtr(false)}},
	}
	for _, r := range Register() {
		in := inputFor(snap)
		in.Policy = policy{"knowledge.requireDocumentLevelAccess": true}
		res, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Findings) != 0 || res.Skipped == nil || !strings.Contains(res.Skipped.Reason, "uncertain:") {
			t.Fatalf("%s: expected uncertain for api-version %s, got %+v", r.ID(), v, res)
		}
	}
}

// Without the opt-in data-plane probe no IQ rule may pass.
func TestIQRulesWithoutDataPlaneNeverPass(t *testing.T) {
	snap := runtimeiq.Snapshot{
		Connections: []runtimeiq.Connection{kbConn("iq", "ProjectManagedIdentity")},
		Services:    map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "standard", SKU: "basic"}},
	}
	for _, r := range Register() {
		in := localInput(snap)
		in.Policy = policy{"knowledge.requireDocumentLevelAccess": true}
		res, err := r.Evaluate(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Findings) == 0 && res.Skipped == nil {
			t.Fatalf("%s passed without the data plane", r.ID())
		}
	}
}

func TestFilterFieldsFromBase(t *testing.T) {
	tests := []struct {
		expr string
		want []string
		ok   bool
	}{
		{"isPublished eq true and accessScope eq 'public'", []string{"isPublished", "accessScope"}, true},
		{"category eq 'Benefits'", []string{"category"}, true},
		{"rating ge 4 and lastRenovated lt 2020-01-01T00:00:00Z", []string{"rating", "lastRenovated"}, true},
		{"deleted eq null or not (status ne 'it''s')", []string{"deleted", "status"}, true},
		{"search.in(region, 'emea,apac', ',')", []string{"region"}, true},
		{"groupIds/any(g: g eq 'abc') and userIds/all(u: search.in(u, 'x'))", []string{"groupIds", "userIds"}, true},
		{"geo.distance(location, geography'POINT(-122.13 47.67)') le 10", []string{"location"}, true},
		{"search.ismatch('foo', 'title')", nil, true},
		{"Address/City eq 'Seattle'", nil, false},
		{"custom.fn(x) eq 1", nil, false},
		{"title eq 'unterminated", nil, false},
		{"", nil, true},
	}
	for _, tt := range tests {
		got, ok := filterFieldsFromBase(tt.expr)
		if ok != tt.ok || strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Fatalf("filterFieldsFromBase(%q) = %v, %v; want %v, %v", tt.expr, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIQ001LearnBaseFilterExample(t *testing.T) {
	idx := runtimeiq.Index{Name: "docs", Fields: []runtimeiq.Field{
		{Name: "isPublished", Type: "Edm.Boolean", Filterable: true},
		{Name: "accessScope", Type: "Edm.String", Filterable: true},
	}}
	src := runtimeiq.KnowledgeSource{Name: "src", SearchIndexName: "docs", BaseFilter: "isPublished eq true and accessScope eq 'public'"}
	got := mustEval(t, "FND-IQ-001", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": idx},
	}))
	if len(got.Findings) != 0 || got.Skipped != nil {
		t.Fatalf("Learn baseFilter example must pass, got %+v", got)
	}
	src.BaseFilter = "Address/City eq 'Seattle'"
	unresolved := mustEval(t, "FND-IQ-001", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": src},
		Indexes: map[string]runtimeiq.Index{"docs": idx},
	}))
	if len(unresolved.Findings) != 0 || unresolved.Skipped == nil || !strings.Contains(unresolved.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain for a complex-field path, got %+v", unresolved)
	}
}

func TestIQ002PermissionCollectionIsNotVector(t *testing.T) {
	idx := runtimeiq.Index{
		Name: "docs",
		Fields: []runtimeiq.Field{
			{Name: "id", Type: "Edm.String", Key: true},
			{Name: "content", Type: "Edm.String", Searchable: true, Retrievable: true},
			{Name: "groupIds", Type: "Collection(Edm.String)", Filterable: true},
		},
		DefaultSemanticConfiguration: "default",
		SemanticConfigurations:       []runtimeiq.SemanticConfiguration{{Name: "default", ContentFields: []string{"content"}}},
	}
	snap := runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": {Name: "src", SearchIndexName: "docs"}},
		Indexes: map[string]runtimeiq.Index{"docs": idx},
	}
	for _, id := range []string{"FND-IQ-002", "FND-IQ-003"} {
		if got := mustEval(t, id, inputFor(snap)); len(got.Findings) != 0 || got.Skipped != nil {
			t.Fatalf("%s: filterable Collection(Edm.String) must not be a vector field, got %+v", id, got)
		}
	}
}

func boolPtr(v bool) *bool { return &v }
