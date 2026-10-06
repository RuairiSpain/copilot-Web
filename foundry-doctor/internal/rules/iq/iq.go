// Package iq implements the Phase 8 Foundry IQ and Search rules.
package iq

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	runtimeiq "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime/iq"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func Register() []sdk.Rule {
	return []sdk.Rule{
		rule{id: "FND-IQ-001", fn: eval001},
		rule{id: "FND-IQ-002", fn: eval002},
		rule{id: "FND-IQ-003", fn: eval003},
		rule{id: "FND-IQ-004", fn: eval004},
		rule{id: "FND-IQ-005", fn: eval005},
		rule{id: "FND-IQ-006", fn: eval006},
		rule{id: "FND-IQ-007", fn: eval007},
		rule{id: "FND-IQ-008", fn: eval008},
		rule{id: "FND-IQ-009", fn: eval009},
		rule{id: "FND-IQ-010", fn: eval010},
		rule{id: "FND-IQ-011", fn: eval011},
		rule{id: "FND-IQ-012", fn: eval012},
	}
}

type rule struct {
	id string
	fn func(*sdk.Input) sdk.Result
}

func (r rule) ID() string { return r.id }

func (r rule) Evaluate(ctx context.Context, in *sdk.Input) (sdk.Result, error) {
	if err := ctx.Err(); err != nil {
		return sdk.Result{}, fmt.Errorf("%s: %w", r.id, err)
	}
	return r.fn(in), nil
}

type snapshotProvider interface{ IQSnapshot() runtimeiq.Snapshot }

type acc struct {
	findings []sdk.Finding
	skips    []string
	notes    []string
}

func (a *acc) add(loc sdk.Location, typ, name, evidence string) {
	a.findings = append(a.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: typ, Name: name},
		Location: loc,
		Evidence: evidence,
	})
}

func (a *acc) result() sdk.Result {
	if len(a.findings) > 0 {
		slices.SortFunc(a.findings, func(x, y sdk.Finding) int {
			if c := strings.Compare(x.Resource.Name, y.Resource.Name); c != 0 {
				return c
			}
			return strings.Compare(x.Evidence, y.Evidence)
		})
		return sdk.Result{Findings: a.findings}
	}
	if len(a.skips) > 0 {
		slices.Sort(a.skips)
		return sdk.Result{Skipped: &sdk.Skip{Reason: strings.Join(unique(a.skips), "; ")}}
	}
	if len(a.notes) > 0 {
		slices.Sort(a.notes)
		return sdk.Result{Skipped: &sdk.Skip{Reason: "uncertain: " + strings.Join(unique(a.notes), "; ")}}
	}
	return sdk.Result{}
}

func snapshot(in *sdk.Input) (runtimeiq.Snapshot, bool) {
	s := runtimeiq.Snapshot{
		Services:    runtimeiq.SearchServicesFromARM(in),
		Storage:     runtimeiq.StorageAccountsFromARM(in),
		Deployments: runtimeiq.EmbeddingDeploymentsFromARM(in),
		Connections: runtimeiq.ConnectionsFromAzureYAML(in),
		Bases:       map[string]runtimeiq.KnowledgeBase{},
		Sources:     map[string]runtimeiq.KnowledgeSource{},
		Indexes:     map[string]runtimeiq.Index{},
		Skillsets:   map[string]runtimeiq.Skillset{},
	}
	if in != nil && in.ARM != nil {
		if p, ok := in.ARM.(snapshotProvider); ok {
			ps := p.IQSnapshot()
			mergeSnapshot(&s, ps)
		}
	}
	if len(s.Bases) == 0 && len(s.Sources) == 0 && len(s.Indexes) == 0 && len(s.Skillsets) == 0 {
		return s, false
	}
	return s, true
}

func mergeSnapshot(dst *runtimeiq.Snapshot, src runtimeiq.Snapshot) {
	for k, v := range src.Bases {
		dst.Bases[strings.ToLower(k)] = v
	}
	for k, v := range src.Sources {
		dst.Sources[strings.ToLower(k)] = v
	}
	for k, v := range src.Indexes {
		dst.Indexes[strings.ToLower(k)] = v
	}
	for k, v := range src.Skillsets {
		dst.Skillsets[strings.ToLower(k)] = v
	}
	if len(src.Services) > 0 {
		if dst.Services == nil {
			dst.Services = map[string]runtimeiq.SearchService{}
		}
		for k, v := range src.Services {
			dst.Services[strings.ToLower(k)] = v
		}
	}
	if len(src.Storage) > 0 {
		for k, v := range src.Storage {
			dst.Storage[strings.ToLower(k)] = v
		}
	}
	if len(src.Deployments) > 0 {
		for k, v := range src.Deployments {
			dst.Deployments[strings.ToLower(k)] = v
		}
	}
	if len(src.Connections) > 0 {
		dst.Connections = append(dst.Connections, src.Connections...)
	}
}

func eval001(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if source.SearchIndexName == "" {
			continue
		}
		seen = true
		idx, ok := snap.Indexes[strings.ToLower(source.SearchIndexName)]
		if !ok {
			a.skips = append(a.skips, "index-metadata-not-readable")
			continue
		}
		for _, field := range source.FilterFields {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "knowledge source filter field must exist in the index and be filterable")
			}
		}
		for _, field := range filterFieldsFromBase(source.BaseFilter) {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "baseFilter references a field that is missing or not filterable")
			}
		}
		if field := strings.TrimSpace(source.SecurityField); field != "" {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "security-filter field must be filterable")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-source-kind-without-backing-index-input"}}
	}
	return a.result()
}

func eval002(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if source.SearchIndexName == "" {
			continue
		}
		seen = true
		idx, ok := snap.Indexes[strings.ToLower(source.SearchIndexName)]
		if !ok {
			a.skips = append(a.skips, "index-metadata-not-readable")
			continue
		}
		if !hasStringKey(idx) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "index must contain a key field of type Edm.String")
		}
		if !hasSearchableRetrievableString(idx) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "index must contain a searchable and retrievable string content field")
		}
		cfgName := source.SemanticConfigurationName
		if cfgName == "" {
			cfgName = idx.DefaultSemanticConfiguration
		}
		cfg := semanticConfig(idx, cfgName)
		if cfg == nil {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "index must expose a semantic configuration used by the knowledge source")
		} else if !semanticContentValid(idx, *cfg) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "semantic configuration must reference an existing searchable and retrievable string content field")
		}
		for _, f := range idx.Fields {
			if !isVectorField(f) {
				continue
			}
			if !f.Searchable || f.Filterable || f.Facetable || f.Sortable || strings.TrimSpace(f.VectorSearchProfile) == "" {
				a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "vector fields must be searchable, not filterable/facetable/sortable, and reference a vector search profile")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-source-kind-without-backing-index-input"}}
	}
	return a.result()
}

func eval003(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, idx := range snap.Indexes {
		seen = true
		for _, f := range idx.Fields {
			if !isVectorField(f) {
				continue
			}
			if !allowedVectorType(f.Type) {
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field uses an unsupported vector data type")
			}
			switch {
			case f.Dimensions == 0:
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field dimensions are missing")
			case f.Dimensions < 2:
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field dimensions must be at least 2")
			case f.Dimensions > 4096:
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field dimensions exceed the Search service maximum of 4096")
			}
			p := vectorProfile(idx, f.VectorSearchProfile)
			if p == nil {
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field references a vector search profile that does not exist")
				continue
			}
			if p.Algorithm == "" || !contains(idx.VectorAlgorithms, p.Algorithm) {
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector search profile references an algorithm that does not exist")
			}
			if p.Vectorizer != "" {
				v := vectorizer(idx, p.Vectorizer)
				if v == nil {
					a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector search profile references a vectorizer that does not exist")
				} else if strings.EqualFold(v.Kind, "azureOpenAI") {
					if min, max, ok := modelRange(v.ModelName); ok && (f.Dimensions < min || f.Dimensions > max) {
						a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector field dimensions do not match the documented range for the Azure OpenAI vectorizer model")
					}
				}
			}
			if p.Compression != "" && !contains(idx.Compressions, p.Compression) {
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vector search profile references a compression that does not exist")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "index-metadata-not-readable"}}
	}
	return a.result()
}

func eval004(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		idx, ok := snap.Indexes[strings.ToLower(source.SearchIndexName)]
		if !ok {
			continue
		}
		seen = true
		var skillset *runtimeiq.Skillset
		if name := strings.TrimSpace(source.SkillsetName); name != "" {
			if ss, ok := snap.Skillsets[strings.ToLower(name)]; ok {
				skillset = &ss
			}
		}
		for _, f := range idx.Fields {
			if !isVectorField(f) {
				continue
			}
			p := vectorProfile(idx, f.VectorSearchProfile)
			if p == nil {
				continue
			}
			v := vectorizer(idx, p.Vectorizer)
			if v == nil || !strings.EqualFold(v.Kind, "azureOpenAI") {
				a.skips = append(a.skips, "non-azure-openai-vectorizer")
				continue
			}
			if min, max, ok := modelRange(v.ModelName); ok && (f.Dimensions < min || f.Dimensions > max) {
				a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "vector field dimensions fall outside the documented range for the embedding model")
			}
			if skillset != nil {
				for _, sk := range skillset.Skills {
					if !strings.Contains(sk.ODataType, "AzureOpenAIEmbeddingSkill") {
						continue
					}
					if sk.Dimensions != 0 && f.Dimensions != sk.Dimensions {
						a.add(locOf(source), "AzureAISearch/skillsets", skillset.Name, "vector field dimensions differ from the embedding skill dimensions")
					}
				}
			}
			if depModel := deploymentModel(snap, v.ResourceURI, v.DeploymentID); depModel != "" && !strings.EqualFold(depModel, v.ModelName) {
				a.add(locOf(source), "Microsoft.CognitiveServices/accounts/deployments", v.DeploymentID, "deployment model metadata does not match the vectorizer modelName")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "index-metadata-not-readable"}}
	}
	return a.result()
}

func eval005(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	if len(snap.Bases) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-base-metadata-not-readable"}}
	}
	for key, kb := range snap.Bases {
		service := firstSegment(key)
		svc := snap.Services[service]
		version := kb.APIVersion
		if version == "" {
			a.skips = append(a.skips, "api-version-not-pinned")
			continue
		}
		if !knownKnowledgeBaseVersion(version) {
			a.notes = append(a.notes, "knowledge base "+kb.Name+" uses an unsupported or unknown API version "+version)
			continue
		}
		switch strings.ToLower(kb.RetrievalReasoningKind) {
		case "auto":
			if version != "2026-08-01-preview" || len(kb.Models) == 0 {
				a.add(svc.SourceLocation, "AzureAISearch/knowledgebases", kb.Name, "auto retrieval reasoning requires API version 2026-08-01-preview and at least one model")
			}
		case "minimal":
			if !strings.EqualFold(kb.OutputMode, "extractiveData") || kb.AnswerSynthesis || kb.UsesWebKnowledgeSource {
				a.add(svc.SourceLocation, "AzureAISearch/knowledgebases", kb.Name, "minimal retrieval reasoning requires outputMode extractiveData, no answer synthesis, and no web knowledge sources")
			}
		case "medium":
			if svc.Location == "" {
				a.skips = append(a.skips, "search-service-resource-not-readable")
			} else if !supportedMediumRegion(svc.Location) {
				a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "medium retrieval reasoning is not supported in this Search service region")
			}
		}
		if version == "2026-04-01" && len(kb.Models) > 0 {
			a.add(svc.SourceLocation, "AzureAISearch/knowledgebases", kb.Name, "knowledge base models are not supported by API version 2026-04-01")
		}
		if len(kb.KnowledgeSources) > tierKnowledgeSourceLimit(svc.SKU) {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "knowledge base exceeds the documented knowledge source limit for the Search service tier")
		}
	}
	return a.result()
}

func eval006(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seenSkill := false
	for _, ss := range snap.Skillsets {
		for _, sk := range ss.Skills {
			if !strings.Contains(sk.ODataType, "SplitSkill") || !strings.EqualFold(sk.TextSplitMode, "pages") {
				continue
			}
			seenSkill = true
			if !strings.EqualFold(sk.Unit, "azureOpenAITokens") && (sk.MaximumPageLength < 300 || sk.MaximumPageLength > 50000) {
				a.add(sdk.Location{}, "AzureAISearch/skillsets", ss.Name, "Text Split skill maximumPageLength must be between 300 and 50000 characters")
			}
			if sk.PageOverlapLength < 0 || (sk.MaximumPageLength > 0 && sk.PageOverlapLength*2 >= sk.MaximumPageLength) {
				a.add(sdk.Location{}, "AzureAISearch/skillsets", ss.Name, "Text Split skill pageOverlapLength must be less than half of maximumPageLength")
			}
		}
	}
	seenWeight := false
	for _, kb := range snap.Bases {
		for _, q := range kb.VectorQueries {
			seenWeight = true
			if q.Weight <= 0 {
				a.add(sdk.Location{}, "AzureAISearch/knowledgebases", kb.Name, "vector query weight must be greater than zero")
			}
		}
	}
	if !seenSkill && !seenWeight {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-split-skill-in-project; no-explicit-query-definitions"}}
	}
	return a.result()
}

func eval007(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	if len(snap.Bases) == 0 && len(snap.Sources) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	for _, source := range snap.Sources {
		versionKnown, supported := supportedKnowledgeSource(source.APIVersion, source.Kind)
		if !versionKnown {
			a.notes = append(a.notes, "knowledge source "+source.Name+" uses an unsupported or unknown API version "+source.APIVersion)
			continue
		}
		if !supported {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "knowledge source kind is not supported by the targeted Search API version")
		}
	}
	for _, kb := range snap.Bases {
		if len(kb.Models) == 0 {
			a.skips = append(a.skips, "knowledge-base-without-model")
			continue
		}
		for _, m := range kb.Models {
			if !strings.EqualFold(m.Kind, "azureOpenAI") {
				a.add(sdk.Location{}, "AzureAISearch/knowledgebases", kb.Name, "knowledge base models must use kind azureOpenAI")
				continue
			}
			versionKnown, supported := supportedPlanningModel(kb.APIVersion, m.ModelName)
			if !versionKnown {
				a.notes = append(a.notes, "knowledge base "+kb.Name+" uses an unsupported or unknown API version "+kb.APIVersion)
				continue
			}
			if !supported {
				a.add(sdk.Location{}, "AzureAISearch/knowledgebases", kb.Name, "knowledge base planning model is not in the documented model table for the targeted API version")
			}
			if isLegacyPlanningModel(m.ModelName) && strings.ToLower(kb.RetrievalReasoningKind) != "minimal" && anyStoredFilterSource(snap, kb) {
				a.add(sdk.Location{}, "AzureAISearch/knowledgebases", kb.Name, "stored queryHints filters are incompatible with GPT-4o and GPT-4.1 planning models unless retrieval reasoning is minimal")
			}
		}
	}
	return a.result()
}

func eval008(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		seen = true
		if source.HasSecretConnection {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "knowledge source connection uses a secret instead of a managed identity ResourceId connection")
		}
		if source.ResourceIDConnection != "" {
			if svc := serviceForSource(snap, source); svc.Name != "" && strings.TrimSpace(svc.IdentityType) == "" {
				a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "Search service uses a ResourceId connection but has no managed identity")
			}
		}
		for _, m := range source.VectorizerModelRefs {
			if m.APIKeySet {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "knowledge source model parameters include apiKey instead of using managed identity")
			}
		}
	}
	for _, idx := range snap.Indexes {
		for _, v := range idx.Vectorizers {
			if v.APIKeySet {
				seen = true
				a.add(sdk.Location{}, "AzureAISearch/indexes", idx.Name, "vectorizer includes apiKey instead of using managed identity")
			}
		}
	}
	for _, kb := range snap.Bases {
		for _, m := range kb.Models {
			if m.APIKeySet {
				seen = true
				a.add(sdk.Location{}, "AzureAISearch/knowledgebases", kb.Name, "knowledge base model includes apiKey instead of using managed identity")
			}
		}
	}
	for _, conn := range snap.Connections {
		seen = true
		if !strings.EqualFold(conn.AuthType, "ProjectManagedIdentity") {
			a.add(conn.Location, "azure.yaml", conn.Name, "knowledge base MCP connection should use authType ProjectManagedIdentity")
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	return a.result()
}

func eval009(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	req, declared := boolPolicy(in, "knowledge.requireDocumentLevelAccess")
	if !declared || !req {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-access-requirement-declared"}}
	}
	a := &acc{}
	for _, source := range snap.Sources {
		if !permissionIndexedSource(source.Kind) {
			continue
		}
		if len(source.IngestionPermissionOptions) == 0 {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "permission-protected knowledge source must set ingestionPermissionOptions")
		}
		if source.AssetStorePresent {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "permission-enabled knowledge source cannot also set assetStore")
		}
		if source.APIVersion == "2026-04-01" {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "ingestionPermissionOptions require API version 2026-08-01-preview")
		} else if !knownKnowledgeSourceVersion(source.APIVersion) {
			a.notes = append(a.notes, "knowledge source "+source.Name+" uses an unsupported or unknown API version "+source.APIVersion)
		}
	}
	for _, conn := range snap.Connections {
		if !conn.ForwardUserToken {
			a.add(conn.Location, "azure.yaml", conn.Name, "permission-enabled Foundry IQ connections must forward x-ms-query-source-authorization")
		}
	}
	return a.result()
}

func eval010(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if !source.IsADLSGen2 {
			continue
		}
		seen = true
		account := storageFromConnection(snap, source.ResourceIDConnection)
		if account.Name == "" {
			a.skips = append(a.skips, "connection-string-not-resourceid")
			continue
		}
		if account.IsHNSEnabled == nil {
			a.skips = append(a.skips, "hns-value-from-unresolved-parameter")
			continue
		}
		if !*account.IsHNSEnabled {
			a.add(locOf(source), "Microsoft.Storage/storageAccounts", account.Name, "ADLS Gen2 knowledge source requires hierarchical namespace enabled on the storage account")
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
	}
	return a.result()
}

func eval011(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		sched := source.RefreshSchedule
		if sched == nil {
			continue
		}
		seen = true
		if strings.TrimSpace(sched.Interval) == "" {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "ingestionSchedule.interval is required")
			continue
		}
		mins, ok := parseSchedule(sched.Interval)
		if !ok || mins < 5 || mins > 1440 {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "ingestionSchedule.interval must be an XSD dayTimeDuration between PT5M and P1D")
		}
		if st := strings.TrimSpace(sched.StartTime); st != "" {
			t, err := time.Parse(time.RFC3339, st)
			if err != nil || t.UTC().Format(time.RFC3339) != st {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "ingestionSchedule.startTime must be a UTC RFC3339 timestamp")
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "schedule-null"}}
	}
	return a.result()
}

func eval012(in *sdk.Input) sdk.Result {
	snap, ok := snapshot(in)
	if !ok {
		return skipUnavailable()
	}
	if len(snap.Services) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "service-not-in-template-or-subscription"}}
	}
	a := &acc{}
	semanticUsed := false
	for _, source := range snap.Sources {
		if source.SearchIndexName != "" {
			semanticUsed = true
		}
	}
	if !semanticUsed {
		return sdk.Result{}
	}
	for _, svc := range snap.Services {
		if !knownSearchManagementVersion(svc.APIVersion) {
			a.notes = append(a.notes, "search service "+svc.Name+" uses an unsupported or unknown API version "+svc.APIVersion)
			continue
		}
		sem := strings.ToLower(strings.TrimSpace(svc.SemanticSearch))
		if sem == "" {
			continue
		}
		if compareAPIVersion(svc.APIVersion, "2026-03-01-preview") < 0 && sem == "disabled" {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "semanticSearch disabled turns off semantic ranking used by agentic retrieval")
		}
		if (sem == "standard" || strings.EqualFold(svc.KnowledgeRetrieval, "standard")) && strings.EqualFold(svc.SKU, "free") {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "standard semanticSearch or knowledgeRetrieval requires the Basic tier or higher")
		}
	}
	return a.result()
}

func skipUnavailable() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable}}
}

func locOf(source runtimeiq.KnowledgeSource) sdk.Location { return source.Location }

func hasStringKey(idx runtimeiq.Index) bool {
	for _, f := range idx.Fields {
		if f.Key && strings.EqualFold(f.Type, "Edm.String") {
			return true
		}
	}
	return false
}

func hasSearchableRetrievableString(idx runtimeiq.Index) bool {
	for _, f := range idx.Fields {
		if strings.EqualFold(f.Type, "Edm.String") && f.Searchable && f.Retrievable {
			return true
		}
	}
	return false
}

func findField(idx runtimeiq.Index, name string) *runtimeiq.Field {
	for i := range idx.Fields {
		if strings.EqualFold(idx.Fields[i].Name, name) {
			return &idx.Fields[i]
		}
	}
	return nil
}

func semanticConfig(idx runtimeiq.Index, name string) *runtimeiq.SemanticConfiguration {
	for i := range idx.SemanticConfigurations {
		if strings.EqualFold(idx.SemanticConfigurations[i].Name, name) {
			return &idx.SemanticConfigurations[i]
		}
	}
	return nil
}

func semanticContentValid(idx runtimeiq.Index, cfg runtimeiq.SemanticConfiguration) bool {
	if len(cfg.ContentFields) == 0 {
		return false
	}
	for _, name := range cfg.ContentFields {
		if f := findField(idx, name); f != nil && strings.EqualFold(f.Type, "Edm.String") && f.Searchable && f.Retrievable {
			return true
		}
	}
	return false
}

func isVectorField(f runtimeiq.Field) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.Type)), "collection(edm.") || f.Dimensions > 0 || strings.TrimSpace(f.VectorSearchProfile) != ""
}

func allowedVectorType(t string) bool {
	switch strings.TrimSpace(t) {
	case "Collection(Edm.Single)", "Collection(Edm.Half)", "Collection(Edm.Int16)", "Collection(Edm.SByte)", "Collection(Edm.Byte)":
		return true
	}
	return false
}

func vectorProfile(idx runtimeiq.Index, name string) *runtimeiq.VectorProfile {
	for i := range idx.VectorProfiles {
		if strings.EqualFold(idx.VectorProfiles[i].Name, name) {
			return &idx.VectorProfiles[i]
		}
	}
	return nil
}

func vectorizer(idx runtimeiq.Index, name string) *runtimeiq.Vectorizer {
	for i := range idx.Vectorizers {
		if strings.EqualFold(idx.Vectorizers[i].Name, name) {
			return &idx.Vectorizers[i]
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(item, v) {
			return true
		}
	}
	return false
}

func modelRange(name string) (int, int, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "text-embedding-ada-002":
		return 1536, 1536, true
	case "text-embedding-3-small":
		return 1, 1536, true
	case "text-embedding-3-large":
		return 1, 3072, true
	default:
		return 0, 0, false
	}
}

func deploymentModel(s runtimeiq.Snapshot, uri, deployment string) string {
	u, _, ok := runtimeiq.ParseKnowledgeBaseEndpoint(uri)
	if ok {
		_ = u
	}
	host := hostLabel(uri)
	if host == "" {
		return ""
	}
	key := strings.ToLower(host + "/" + deployment)
	if d, ok := s.Deployments[key]; ok {
		return d.ModelName
	}
	return ""
}

func hostLabel(uri string) string {
	uri = strings.TrimSpace(uri)
	uri = strings.TrimPrefix(uri, "https://")
	if i := strings.Index(uri, "."); i >= 0 {
		return strings.ToLower(uri[:i])
	}
	return ""
}

func supportedMediumRegion(region string) bool {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "eastus", "westus3", "westeurope", "swedencentral":
		return true
	default:
		return false
	}
}

func tierKnowledgeSourceLimit(sku string) int {
	switch strings.ToLower(strings.TrimSpace(sku)) {
	case "free":
		return 3
	case "basic":
		return 5
	default:
		return 10
	}
}

func supportedKnowledgeSource(version, kind string) (bool, bool) {
	base := map[string]bool{"searchindex": true, "azureblob": true, "indexedonelake": true, "web": true}
	preview := map[string]bool{
		"indexedsharepoint": true, "indexedsql": true, "remotesharepoint": true, "workiq": true,
		"file": true, "mcpserver": true, "fabricdataagent": true, "fabricontology": true,
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !knownKnowledgeSourceVersion(version) {
		return false, false
	}
	if base[kind] {
		return true, true
	}
	return true, version != "2026-04-01" && preview[kind]
}

func supportedPlanningModel(version, name string) (bool, bool) {
	common := map[string]bool{
		"gpt-4o": true, "gpt-4o-mini": true, "gpt-4.1": true, "gpt-4.1-mini": true, "gpt-4.1-nano": true,
		"gpt-5": true, "gpt-5-mini": true, "gpt-5-nano": true,
	}
	add0501 := map[string]bool{"gpt-5.1": true, "gpt-5.2": true, "gpt-5.4": true, "gpt-5.4-mini": true, "gpt-5.4-nano": true}
	add0801 := map[string]bool{"gpt-5.5": true, "gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true}
	if !knownKnowledgeBaseVersion(version) {
		return false, false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if common[name] {
		return true, true
	}
	if version != "2026-04-01" && add0501[name] {
		return true, true
	}
	return true, version == "2026-08-01-preview" && add0801[name]
}

func isLegacyPlanningModel(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(name, "gpt-4o") || strings.HasPrefix(name, "gpt-4.1")
}

func anyStoredFilterSource(s runtimeiq.Snapshot, kb runtimeiq.KnowledgeBase) bool {
	for _, name := range kb.KnowledgeSources {
		if src, ok := s.Sources[strings.ToLower(name)]; ok && (len(src.FilterFields) > 0 || strings.TrimSpace(src.BaseFilter) != "") {
			return true
		}
	}
	return false
}

func serviceForSource(s runtimeiq.Snapshot, source runtimeiq.KnowledgeSource) runtimeiq.SearchService {
	for key, kb := range s.Bases {
		for _, name := range kb.KnowledgeSources {
			if !strings.EqualFold(name, source.Name) {
				continue
			}
			if svc, ok := s.Services[strings.ToLower(firstSegment(key))]; ok {
				return svc
			}
		}
	}
	return runtimeiq.SearchService{}
}

func boolPolicy(in *sdk.Input, key string) (bool, bool) {
	if in == nil || in.Policy == nil {
		return false, false
	}
	v, ok := in.Policy.Get(key)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func permissionIndexedSource(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "azureblob", "indexedonelake", "indexedsharepoint":
		return true
	default:
		return false
	}
}

func storageFromConnection(s runtimeiq.Snapshot, resourceID string) runtimeiq.StorageAccount {
	resourceID = strings.ToLower(resourceID)
	for name, acct := range s.Storage {
		if strings.Contains(resourceID, "/storageaccounts/"+strings.ToLower(name)) {
			return acct
		}
	}
	return runtimeiq.StorageAccount{}
}

var schedRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?)?$`)

func parseSchedule(v string) (int, bool) {
	m := schedRe.FindStringSubmatch(strings.TrimSpace(v))
	if len(m) == 0 {
		return 0, false
	}
	total := 0
	for i, mult := range []int{1440, 60, 1} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return 0, false
		}
		total += n * mult
	}
	return total, true
}

func filterFieldsFromBase(expr string) []string {
	if strings.TrimSpace(expr) == "" {
		return nil
	}
	parts := fieldTokenRe.FindAllString(strings.ToLower(expr), -1)
	var out []string
	for _, p := range parts {
		if keyword[p] {
			continue
		}
		out = append(out, p)
	}
	return unique(out)
}

var fieldTokenRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

var keyword = map[string]bool{
	"and": true, "or": true, "not": true, "eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
	"search": true, "any": true, "all": true, "true": true, "false": true,
}

func compareAPIVersion(a, b string) int {
	switch {
	case a == b:
		return 0
	case a < b:
		return -1
	default:
		return 1
	}
}

func knownKnowledgeBaseVersion(v string) bool {
	switch strings.TrimSpace(v) {
	case "2026-04-01", "2026-05-01-preview", "2026-08-01-preview":
		return true
	default:
		return false
	}
}

func knownKnowledgeSourceVersion(v string) bool { return knownKnowledgeBaseVersion(v) }

func knownSearchManagementVersion(v string) bool {
	switch strings.TrimSpace(v) {
	case "2024-07-01", "2025-05-01", "2026-03-01-preview", "2026-09-01-preview":
		return true
	default:
		return false
	}
}

func firstSegment(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
