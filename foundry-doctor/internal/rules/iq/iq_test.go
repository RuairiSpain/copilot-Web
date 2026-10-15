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

func inputFor(snap runtimeiq.Snapshot) *sdk.Input {
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

func TestIQ006ChunkingAndVectorWeights(t *testing.T) {
	got := mustEval(t, "FND-IQ-006", inputFor(runtimeiq.Snapshot{
		Skillsets: map[string]runtimeiq.Skillset{
			"ss": {Name: "ss", Skills: []runtimeiq.Skill{{ODataType: "#SplitSkill", TextSplitMode: "pages", MaximumPageLength: 100, PageOverlapLength: 60}}},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", VectorQueries: []runtimeiq.VectorQuery{{Weight: 0}}},
		},
	}))
	if len(got.Findings) < 2 {
		t.Fatalf("expected findings, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-006", inputFor(runtimeiq.Snapshot{
		Skillsets: map[string]runtimeiq.Skillset{
			"ss": {Name: "ss", Skills: []runtimeiq.Skill{{ODataType: "#SplitSkill", TextSplitMode: "pages", MaximumPageLength: 1200, PageOverlapLength: 100}}},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", VectorQueries: []runtimeiq.VectorQuery{{Weight: 1}}},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ007SupportedKindsAndModels(t *testing.T) {
	got := mustEval(t, "FND-IQ-007", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"src": {Name: "src", Kind: "indexedSharePoint", APIVersion: "2026-04-01"},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", APIVersion: "2026-04-01", Models: []runtimeiq.ModelRef{{Kind: "azureOpenAI", ModelName: "gpt-5.6-sol"}}},
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

func TestIQ008ManagedIdentityOnly(t *testing.T) {
	got := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"blob": {
				Name:                 "blob",
				APIVersion:           "2026-08-01-preview",
				HasSecretConnection:  true,
				ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs",
			},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", KnowledgeSources: []string{"blob"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc"},
		},
		Connections: []runtimeiq.Connection{{Name: "iq", AuthType: "ApiKey"}},
	}))
	if len(got.Findings) < 3 {
		t.Fatalf("expected findings, got %+v", got)
	}
	for _, f := range got.Findings {
		if strings.Contains(strings.ToLower(f.Evidence), "supersecret") {
			t.Fatalf("evidence leaked secret: %+v", f)
		}
	}
	clean := mustEval(t, "FND-IQ-008", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{
			"blob": {
				Name:                 "blob",
				APIVersion:           "2026-08-01-preview",
				ResourceIDConnection: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Storage/storageAccounts/docs",
			},
		},
		Bases: map[string]runtimeiq.KnowledgeBase{
			"svc/kb": {Name: "kb", KnowledgeSources: []string{"blob"}},
		},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", IdentityType: "SystemAssigned"},
		},
		Connections: []runtimeiq.Connection{{Name: "iq", AuthType: "ProjectManagedIdentity"}},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}
}

func TestIQ009DocumentAccessRequirement(t *testing.T) {
	in := &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-04-01"},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", AuthType: "ProjectManagedIdentity", ForwardSourceAuth: false}},
		}},
	}
	got := mustEval(t, "FND-IQ-009", in)
	if len(got.Findings) != 3 {
		t.Fatalf("expected three findings, got %+v", got)
	}
	clean := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-08-01-preview", IngestionPermissionOptions: []string{"userIds"}},
				"s2": {Name: "s2", Kind: "remoteSharePoint", APIVersion: "2026-08-01-preview"},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", AuthType: "ProjectManagedIdentity", ForwardSourceAuth: true, SourceAuthUsesUserToken: true}},
		}},
	})
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}

	workIQHeaderOnly := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-08-01-preview", IngestionPermissionOptions: []string{"userIds"}},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", AuthType: "ProjectManagedIdentity", ForwardWorkIQAuth: true}},
		}},
	})
	if len(workIQHeaderOnly.Findings) != 1 || !strings.Contains(workIQHeaderOnly.Findings[0].Evidence, "x-ms-query-source-authorization") {
		t.Fatalf("expected source-auth finding, got %+v", workIQHeaderOnly)
	}

	serviceRoot := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-08-01-preview", IngestionPermissionOptions: []string{"userIds"}},
			},
			Connections: []runtimeiq.Connection{{Name: "search", TargetKind: runtimeiq.ConnectionTargetCognitiveSearchTarget, AuthType: "ApiKey"}},
		}},
	})
	if len(serviceRoot.Findings) != 0 || serviceRoot.Skipped != nil {
		t.Fatalf("expected service-root connection to be ignored by IQ-009, got %+v", serviceRoot)
	}

	badVersion := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-05-01-preview", IngestionPermissionOptions: []string{"userIds"}},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", ForwardSourceAuth: true, SourceAuthUsesUserToken: true}},
		}},
	})
	if len(badVersion.Findings) != 1 || !strings.Contains(badVersion.Findings[0].Evidence, "2026-08-01-preview") {
		t.Fatalf("expected preview-version finding, got %+v", badVersion)
	}

	badOption := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-08-01-preview", IngestionPermissionOptions: []string{"ownerIds"}},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", ForwardSourceAuth: true, SourceAuthUsesUserToken: true}},
		}},
	})
	if len(badOption.Findings) != 1 || !strings.Contains(badOption.Findings[0].Evidence, "documented values") {
		t.Fatalf("expected enum finding, got %+v", badOption)
	}

	missingUserToken := mustEval(t, "FND-IQ-009", &sdk.Input{
		Policy: policy{"knowledge.requireDocumentLevelAccess": true},
		ARM: snapshotModel{snap: runtimeiq.Snapshot{
			Sources: map[string]runtimeiq.KnowledgeSource{
				"s1": {Name: "s1", Kind: "azureBlob", APIVersion: "2026-08-01-preview", IngestionPermissionOptions: []string{"userIds"}},
			},
			Connections: []runtimeiq.Connection{{Name: "iq", ForwardSourceAuth: true}},
		}},
	})
	if len(missingUserToken.Findings) != 1 || !strings.Contains(missingUserToken.Findings[0].Evidence, "signed-in user token") {
		t.Fatalf("expected user-token finding, got %+v", missingUserToken)
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
	got := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": {Name: "src", SearchIndexName: "docs"}},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2025-05-01", SemanticSearch: "disabled"},
		},
	}))
	if len(got.Findings) != 1 {
		t.Fatalf("expected one finding, got %+v", got)
	}
	uncertain := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": {Name: "src", SearchIndexName: "docs"}},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2099-01-01", SemanticSearch: "standard"},
		},
	}))
	if uncertain.Skipped == nil || !strings.Contains(uncertain.Skipped.Reason, "uncertain:") {
		t.Fatalf("expected uncertain, got %+v", uncertain)
	}
	clean := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Sources: map[string]runtimeiq.KnowledgeSource{"src": {Name: "src", SearchIndexName: "docs"}},
		Services: map[string]runtimeiq.SearchService{
			"svc": {Name: "svc", APIVersion: "2026-03-01-preview", SemanticSearch: "standard", SKU: "basic", KnowledgeRetrieval: "standard"},
		},
	}))
	if len(clean.Findings) != 0 || clean.Skipped != nil {
		t.Fatalf("expected pass, got %+v", clean)
	}

	probeFailure := mustEval(t, "FND-IQ-012", inputFor(runtimeiq.Snapshot{
		Services:    map[string]runtimeiq.SearchService{"svc": {Name: "svc", APIVersion: "2026-03-01-preview"}},
		ProbeIssues: []string{"knowledge base metadata could not be read for connection iq"},
	}))
	if probeFailure.Skipped == nil || !strings.Contains(probeFailure.Skipped.Reason, "knowledge base metadata could not be read") {
		t.Fatalf("expected probe failure skip, got %+v", probeFailure)
	}
}

func boolPtr(v bool) *bool { return &v }
