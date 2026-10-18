// Package iq implements the Phase 8 Foundry IQ and Search rules.
//
// Every rule that needs a data-plane object (knowledge base, knowledge
// source, index, skillset) skips when the opt-in metadata probe did not read
// that plane, and reports uncertain for an object read with an API version
// outside runtimeiq.KnownAPIVersion. Findings name each plane that supplied
// evidence: the data-plane object with its api-version, the ARM resource and
// the azure.yaml connection.
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

// add records a finding. key is a stable discriminator (check kind plus the
// object or field it concerns) so that several findings of one rule on one
// resource keep distinct fingerprints.
func (a *acc) add(loc sdk.Location, typ, name, key, evidence string) {
	a.findings = append(a.findings, sdk.Finding{
		Resource: sdk.ResourceRef{Type: typ, Name: name},
		Key:      key,
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
			if c := strings.Compare(x.Key, y.Key); c != 0 {
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

// Evidence references, one per plane.

func dataRef(family, name, version string) string {
	return "data plane " + family + "/" + name + " (api-version " + orNone(version) + ")"
}

func armRef(typ, name, version string) string {
	return "ARM " + typ + "/" + name + " (api-version " + orNone(version) + ")"
}

func yamlRef(conn string) string { return "azure.yaml services." + conn }

func ev(msg string, refs ...string) string {
	var kept []string
	for _, r := range refs {
		if r != "" {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return msg
	}
	return msg + " [" + strings.Join(kept, "; ") + "]"
}

func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "none"
	}
	return v
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
	if !s.LiveRead && len(s.Connections) == 0 && len(s.Bases) == 0 && len(s.Sources) == 0 && len(s.Indexes) == 0 && len(s.Skillsets) == 0 && len(s.Services) == 0 && len(s.Storage) == 0 && len(s.Deployments) == 0 && len(s.ProbeIssues) == 0 {
		return s, false
	}
	return s, true
}

func mergeSnapshot(dst *runtimeiq.Snapshot, src runtimeiq.Snapshot) {
	if len(src.Connections) > 0 {
		dst.Connections = append(dst.Connections[:0], src.Connections...)
	}
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
	for k, v := range src.Services {
		dst.Services[strings.ToLower(k)] = v
	}
	for k, v := range src.Storage {
		dst.Storage[strings.ToLower(k)] = v
	}
	for k, v := range src.Deployments {
		dst.Deployments[strings.ToLower(k)] = v
	}
	dst.ProbeIssues = append(dst.ProbeIssues, src.ProbeIssues...)
	dst.LiveRead = dst.LiveRead || src.LiveRead
}

func snapshotOrProbeSkip(in *sdk.Input) (runtimeiq.Snapshot, sdk.Result, bool) {
	snap, ok := snapshot(in)
	if !ok {
		return snap, skipUnavailable(), true
	}
	if len(snap.ProbeIssues) > 0 {
		return snap, sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": " + strings.Join(unique(snap.ProbeIssues), "; "), MissingCapability: "Foundry IQ data-plane metadata"}}, true
	}
	return snap, sdk.Result{}, false
}

// liveSnapshot returns the snapshot only when the data plane was read.
func liveSnapshot(in *sdk.Input) (runtimeiq.Snapshot, sdk.Result, bool) {
	snap, res, stop := snapshotOrProbeSkip(in)
	if stop {
		return snap, res, true
	}
	if !snap.LiveRead {
		return snap, skipDataPlane(), true
	}
	return snap, sdk.Result{}, false
}

func skipUnavailable() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no Foundry IQ index metadata input", MissingCapability: "Foundry IQ metadata"}}
}

func skipDataPlane() sdk.Result {
	return sdk.Result{Skipped: &sdk.Skip{
		Reason:            sdk.SkipInputUnavailable + ": Foundry IQ data-plane metadata (knowledge bases, knowledge sources, indexes, skillsets) was not read; run doctor with --iq-metadata",
		MissingCapability: "Foundry IQ data-plane metadata",
	}}
}

// knownVersion records an uncertain note and returns false when an object was
// read with an API version whose shape is not verified.
func knownVersion(a *acc, family, name, version string) bool {
	if runtimeiq.KnownAPIVersion(version) {
		return true
	}
	a.notes = append(a.notes, family+" "+name+" uses api-version "+orNone(version)+" outside the verified set ("+runtimeiq.KnownAPIVersionList+")")
	return false
}

func eval001(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if source.SearchIndexName == "" {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
		idx, ok := snap.Indexes[strings.ToLower(source.SearchIndexName)]
		if !ok {
			a.skips = append(a.skips, "index-metadata-not-readable")
			continue
		}
		if !knownVersion(a, "index", idx.Name, idx.APIVersion) {
			continue
		}
		refs := []string{dataRef("knowledgesources", source.Name, source.APIVersion), dataRef("indexes", idx.Name, idx.APIVersion)}
		for _, field := range source.FilterFields {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "query-hint-filter:"+field,
					ev("searchIndexParameters.queryHints.filters field "+field+" must exist in the index and be filterable", refs...))
			}
		}
		fields, ok := filterFieldsFromBase(source.BaseFilter)
		if !ok {
			a.notes = append(a.notes, "searchIndexParameters.baseFilter of knowledge source "+source.Name+" uses an OData construct the field extractor does not resolve")
		}
		for _, field := range fields {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "base-filter:"+field,
					ev("searchIndexParameters.baseFilter references field "+field+", which is missing or not filterable", refs...))
			}
		}
		if field := strings.TrimSpace(source.SecurityField); field != "" {
			if f := findField(idx, field); f == nil || !f.Filterable {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "security-field:"+field,
					ev("security-filter field "+field+" must be filterable", refs...))
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-source-kind-without-backing-index-input"}}
	}
	return a.result()
}

func eval002(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		idxName := source.BackingIndex()
		if idxName == "" {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
		idx, ok := snap.Indexes[strings.ToLower(idxName)]
		if !ok {
			a.skips = append(a.skips, "index-metadata-not-readable")
			continue
		}
		if !knownVersion(a, "index", idx.Name, idx.APIVersion) {
			continue
		}
		refs := []string{dataRef("indexes", idx.Name, idx.APIVersion), dataRef("knowledgesources", source.Name, source.APIVersion)}
		if !hasStringKey(idx) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "key-field", ev("index must contain a key field of type Edm.String", refs...))
		}
		if !hasSearchableRetrievableString(idx) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "content-field", ev("index must contain a searchable and retrievable string content field", refs...))
		}
		cfgName := source.SemanticConfigurationName
		if cfgName == "" {
			cfgName = idx.DefaultSemanticConfiguration
		}
		cfg := semanticConfig(idx, cfgName)
		if cfg == nil {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "semantic-configuration", ev("index must expose a semantic configuration used by the knowledge source", refs...))
		} else if !semanticContentValid(idx, *cfg) {
			a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "semantic-content:"+cfg.Name, ev("semantic configuration "+cfg.Name+" must reference an existing searchable and retrievable string content field", refs...))
		}
		for _, f := range idx.Fields {
			if !isVectorField(f) {
				continue
			}
			if !f.Searchable || f.Filterable || f.Facetable || f.Sortable || strings.TrimSpace(f.VectorSearchProfile) == "" {
				a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "vector-field:"+f.Name, ev("vector field "+f.Name+" must be searchable, not filterable/facetable/sortable, and reference a vector search profile", refs...))
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-source-kind-without-backing-index-input"}}
	}
	return a.result()
}

func eval003(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, idx := range snap.Indexes {
		seen = true
		if !knownVersion(a, "index", idx.Name, idx.APIVersion) {
			continue
		}
		ref := dataRef("indexes", idx.Name, idx.APIVersion)
		for _, f := range idx.Fields {
			if !isVectorField(f) {
				continue
			}
			key := func(kind string) string { return kind + ":" + f.Name }
			if !allowedVectorType(f.Type) {
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("vector-type"), ev("vector field "+f.Name+" uses an unsupported vector data type "+f.Type, ref))
			}
			switch {
			case f.Dimensions == 0:
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("dimensions"), ev("vector field "+f.Name+" dimensions are missing", ref))
			case f.Dimensions < 2:
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("dimensions"), ev("vector field "+f.Name+" dimensions must be at least 2", ref))
			case f.Dimensions > 4096:
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("dimensions"), ev("vector field "+f.Name+" dimensions exceed the Search service maximum of 4096", ref))
			}
			p := vectorProfile(idx, f.VectorSearchProfile)
			if p == nil {
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("profile"), ev("vector field "+f.Name+" references a vector search profile that does not exist", ref))
				continue
			}
			if p.Algorithm == "" || !contains(idx.VectorAlgorithms, p.Algorithm) {
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("algorithm"), ev("vector search profile "+p.Name+" references an algorithm that does not exist", ref))
			}
			if p.Vectorizer != "" {
				v := vectorizer(idx, p.Vectorizer)
				if v == nil {
					a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("vectorizer"), ev("vector search profile "+p.Name+" references a vectorizer that does not exist", ref))
				} else if strings.EqualFold(v.Kind, "azureOpenAI") {
					if min, max, ok := modelRange(v.ModelName); ok && (f.Dimensions < min || f.Dimensions > max) {
						a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("model-dimensions"), ev("vector field "+f.Name+" dimensions do not match the documented range for the Azure OpenAI vectorizer model "+v.ModelName, ref))
					}
				}
			}
			if p.Compression != "" && !contains(idx.Compressions, p.Compression) {
				a.add(idx.Location, "AzureAISearch/indexes", idx.Name, key("compression"), ev("vector search profile "+p.Name+" references a compression that does not exist", ref))
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "index-metadata-not-readable"}}
	}
	return a.result()
}

func eval004(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		idx, ok := snap.Indexes[strings.ToLower(source.BackingIndex())]
		if !ok {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) || !knownVersion(a, "index", idx.Name, idx.APIVersion) {
			continue
		}
		var skillset *runtimeiq.Skillset
		if name := strings.TrimSpace(source.SkillsetName); name != "" {
			if ss, ok := snap.Skillsets[strings.ToLower(name)]; ok && knownVersion(a, "skillset", ss.Name, ss.APIVersion) {
				skillset = &ss
			}
		}
		idxRef := dataRef("indexes", idx.Name, idx.APIVersion)
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
				a.add(locOf(source), "AzureAISearch/indexes", idx.Name, "model-dimensions:"+f.Name, ev("vector field "+f.Name+" dimensions fall outside the documented range for the embedding model "+v.ModelName, idxRef))
			}
			if skillset != nil {
				for _, sk := range skillset.Skills {
					if !strings.Contains(sk.ODataType, "AzureOpenAIEmbeddingSkill") {
						continue
					}
					if sk.Dimensions != 0 && f.Dimensions != sk.Dimensions {
						a.add(locOf(source), "AzureAISearch/skillsets", skillset.Name, "skill-dimensions:"+f.Name,
							ev("vector field "+f.Name+" dimensions differ from the embedding skill dimensions", idxRef, dataRef("skillsets", skillset.Name, skillset.APIVersion)))
					}
				}
			}
			if dep, ok := deploymentFor(snap, v.ResourceURI, v.DeploymentID); ok && dep.ModelName != "" && !strings.EqualFold(dep.ModelName, v.ModelName) {
				a.add(dep.SourceLocation, "Microsoft.CognitiveServices/accounts/deployments", v.DeploymentID, "deployment-model:"+v.Name,
					ev("deployment model "+dep.ModelName+" does not match the vectorizer modelName "+v.ModelName, idxRef, armRef("Microsoft.CognitiveServices/accounts/deployments", dep.AccountName+"/"+dep.Name, dep.APIVersion)))
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "index-metadata-not-readable"}}
	}
	return a.result()
}

func eval005(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	if len(snap.Bases) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "knowledge-base-metadata-not-readable"}}
	}
	for key, kb := range snap.Bases {
		svc, svcKnown := snap.Services[firstSegment(key)]
		version := kb.APIVersion
		if version == "" {
			a.skips = append(a.skips, "api-version-not-pinned")
			continue
		}
		if !knownVersion(a, "knowledge base", kb.Name, version) {
			continue
		}
		kbRef := dataRef("knowledgebases", kb.Name, version)
		svcRef := ""
		if svcKnown {
			svcRef = armRef("Microsoft.Search/searchServices", svc.Name, svc.APIVersion)
		}
		if kb.UsesWebKnowledgeSource && len(kb.Models) == 0 {
			a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "web-source-model", ev("knowledge bases that use web knowledge sources require at least one model", kbRef))
		}
		switch strings.ToLower(kb.RetrievalReasoningKind) {
		case "auto":
			if version != "2026-08-01-preview" || len(kb.Models) == 0 {
				a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "reasoning-auto", ev("auto retrieval reasoning requires API version 2026-08-01-preview and at least one model", kbRef))
			}
		case "minimal":
			if !strings.EqualFold(kb.OutputMode, "extractiveData") || kb.UsesWebKnowledgeSource {
				a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "reasoning-minimal", ev("minimal retrieval reasoning requires outputMode extractiveData and no web knowledge sources", kbRef))
			}
		case "medium":
			switch {
			case !svcKnown || svc.Location == "":
				a.skips = append(a.skips, "search-service-resource-not-readable")
			default:
				if supported, ok := supportedMediumRegion(svc.Location); !ok {
					a.notes = append(a.notes, "search service "+svc.Name+" is in an undocumented region for medium retrieval reasoning")
				} else if !supported {
					a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "reasoning-medium-region:"+kb.Name, ev("medium retrieval reasoning is not supported in this Search service region", svcRef, kbRef))
				}
			}
		}
		if version == "2026-04-01" && len(kb.Models) > 0 {
			a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "models-on-ga", ev("knowledge base models are not supported by API version 2026-04-01", kbRef))
		}
		if !svcKnown {
			a.skips = append(a.skips, "search-service-resource-not-readable")
			continue
		}
		if len(kb.Models) > 0 && strings.EqualFold(svc.SKU, "free") {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "models-tier:"+kb.Name, ev("knowledge base models require the Basic tier or higher on the Search service", svcRef, kbRef))
		}
		switch compareKnowledgeSourceLimit(svc.SKU, len(kb.KnowledgeSources)) {
		case "fail":
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "source-limit:"+kb.Name, ev("knowledge base exceeds the documented knowledge source limit for the Search service tier", svcRef, kbRef))
		case "uncertain":
			a.notes = append(a.notes, "search service "+svc.Name+" is Basic tier and knowledge source counts above five depend on the service creation date")
		}
	}
	return a.result()
}

func eval006(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seenSkill := false
	for _, ss := range snap.Skillsets {
		if !knownVersion(a, "skillset", ss.Name, ss.APIVersion) {
			continue
		}
		ref := dataRef("skillsets", ss.Name, ss.APIVersion)
		for i, sk := range ss.Skills {
			if !strings.Contains(sk.ODataType, "SplitSkill") || !strings.EqualFold(sk.TextSplitMode, "pages") {
				continue
			}
			seenSkill = true
			key := "split-skill[" + strconv.Itoa(i) + "]"
			if !strings.EqualFold(sk.Unit, "azureOpenAITokens") && (sk.MaximumPageLength < 300 || sk.MaximumPageLength > 50000) {
				a.add(ss.Location, "AzureAISearch/skillsets", ss.Name, key+":page-length", ev("Text Split skill maximumPageLength must be between 300 and 50000 characters", ref))
			}
			if sk.PageOverlapLength < 0 || (sk.MaximumPageLength > 0 && sk.PageOverlapLength*2 >= sk.MaximumPageLength) {
				a.add(ss.Location, "AzureAISearch/skillsets", ss.Name, key+":overlap", ev("Text Split skill pageOverlapLength must be less than half of maximumPageLength", ref))
			}
		}
	}
	if !seenSkill && len(a.notes) == 0 {
		// Hybrid weights are query-time values; no knowledge source or
		// knowledge base property carries them, so they are not read.
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-split-skill-in-project; no-explicit-query-definitions"}}
	}
	return a.result()
}

func eval007(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	if len(snap.Bases) == 0 && len(snap.Sources) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no Foundry IQ knowledge base or knowledge source in the input"}}
	}
	for _, source := range snap.Sources {
		versionKnown, supported := supportedKnowledgeSource(source.APIVersion, source.Kind)
		if !versionKnown {
			knownVersion(a, "knowledge source", source.Name, source.APIVersion)
			continue
		}
		if !supported {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "kind:"+source.Kind,
				ev("knowledge source kind "+source.Kind+" is not supported by the targeted Search API version", dataRef("knowledgesources", source.Name, source.APIVersion)))
		}
	}
	for _, kb := range snap.Bases {
		if len(kb.Models) == 0 {
			a.skips = append(a.skips, "knowledge-base-without-model")
			continue
		}
		kbRef := dataRef("knowledgebases", kb.Name, kb.APIVersion)
		for _, m := range kb.Models {
			if !strings.EqualFold(m.Kind, "azureOpenAI") {
				a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "model-kind:"+m.Path, ev(m.Path+" must use kind azureOpenAI", kbRef))
				continue
			}
			versionKnown, supported := supportedPlanningModel(kb.APIVersion, m.ModelName)
			if !versionKnown {
				knownVersion(a, "knowledge base", kb.Name, kb.APIVersion)
				continue
			}
			if !supported {
				a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "model-name:"+m.Path, ev("planning model "+m.ModelName+" is not in the documented model table for the targeted API version", kbRef))
			}
			if isLegacyPlanningModel(m.ModelName) && strings.ToLower(kb.RetrievalReasoningKind) != "minimal" && anyStoredFilterSource(snap, kb) {
				a.add(kb.Location, "AzureAISearch/knowledgebases", kb.Name, "legacy-model-filters:"+m.Path, ev("stored queryHints filters are incompatible with GPT-4o and GPT-4.1 planning models unless retrieval reasoning is minimal", kbRef))
			}
		}
	}
	return a.result()
}

// eval008 checks that Foundry IQ connections and their data-plane objects
// authenticate with managed identity. The azure.yaml connection leg runs
// without the data plane; every other leg needs it, and absence of an apiKey
// in a GET response is not taken as proof of keyless auth because the
// redaction behaviour of Search GET responses is unverified.
func eval008(in *sdk.Input) sdk.Result {
	snap, res, stop := snapshotOrProbeSkip(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, conn := range snap.Connections {
		switch conn.TargetKind {
		case runtimeiq.ConnectionTargetKnowledgeBaseMCP:
		case runtimeiq.ConnectionTargetUnresolved:
			a.notes = append(a.notes, "connection "+conn.Name+" target is an unresolved ${...} reference")
			continue
		default:
			continue
		}
		seen = true
		ref := yamlRef(conn.Name)
		if !strings.EqualFold(conn.AuthType, "ProjectManagedIdentity") {
			a.add(conn.Location, "azure.yaml", conn.Name, "connection-auth", ev("knowledge base MCP connection should use authType ProjectManagedIdentity", ref))
		}
		if conn.Category != "" && !strings.EqualFold(conn.Category, "RemoteTool") {
			a.add(conn.Location, "azure.yaml", conn.Name, "connection-category", ev("knowledge base MCP connection should use category RemoteTool", ref))
		}
		if conn.Audience != "" && strings.TrimSuffix(strings.ToLower(conn.Audience), "/") != "https://search.azure.com" {
			a.add(conn.Location, "azure.yaml", conn.Name, "connection-audience", ev("knowledge base MCP connection audience should be https://search.azure.com/", ref))
		}
	}
	if !snap.LiveRead {
		if len(a.findings) > 0 {
			return a.result()
		}
		return skipDataPlane()
	}
	for _, source := range snap.Sources {
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
		ref := dataRef("knowledgesources", source.Name, source.APIVersion)
		paramsKey := runtimeiq.ParametersKey(source.Kind)
		switch {
		case source.HasSecretConnection:
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "connection-secret", ev(paramsKey+".connectionString uses a secret instead of a managed identity ResourceId connection", ref))
		case source.ConnectionUnclassified:
			a.notes = append(a.notes, paramsKey+".connectionString of knowledge source "+source.Name+" is redacted or not classifiable")
		}
		if source.ResourceIDConnection != "" && !source.HasSecretConnection {
			svc := serviceForSource(snap, source)
			switch {
			case svc.Name == "":
				a.notes = append(a.notes, "search service hosting knowledge source "+source.Name+" is not in the template, so its managed identity is not visible")
			case strings.HasPrefix(strings.TrimSpace(svc.IdentityType), "["):
				a.notes = append(a.notes, "search service "+svc.Name+" identity type is an unresolved expression")
			case !hasIdentity(svc.IdentityType):
				a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "service-identity:"+source.Name,
					ev("Search service uses a ResourceId connection but has no managed identity", armRef("Microsoft.Search/searchServices", svc.Name, svc.APIVersion), ref))
			}
		}
		for _, m := range source.Models {
			checkModelKey(a, locOf(source), "AzureAISearch/knowledgesources", source.Name, m, ref)
		}
	}
	for _, idx := range snap.Indexes {
		if !knownVersion(a, "index", idx.Name, idx.APIVersion) {
			continue
		}
		ref := dataRef("indexes", idx.Name, idx.APIVersion)
		for i, v := range idx.Vectorizers {
			if !strings.EqualFold(v.Kind, "azureOpenAI") {
				continue
			}
			seen = true
			path := "vectorSearch.vectorizers[" + strconv.Itoa(i) + "].azureOpenAIParameters"
			checkModelKey(a, idx.Location, "AzureAISearch/indexes", idx.Name, runtimeiq.ModelRef{Kind: v.Kind, APIKeySet: v.APIKeySet, AuthIdentitySet: v.AuthIdentitySet, Path: path}, ref)
		}
	}
	for _, kb := range snap.Bases {
		if !knownVersion(a, "knowledge base", kb.Name, kb.APIVersion) {
			continue
		}
		ref := dataRef("knowledgebases", kb.Name, kb.APIVersion)
		for _, m := range kb.Models {
			seen = true
			checkModelKey(a, kb.Location, "AzureAISearch/knowledgebases", kb.Name, m, ref)
		}
	}
	if !seen {
		if len(a.notes) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no Foundry IQ knowledge base MCP connection in the input"}}
	}
	return a.result()
}

func checkModelKey(a *acc, loc sdk.Location, typ, name string, m runtimeiq.ModelRef, ref string) {
	switch {
	case m.APIKeySet:
		a.add(loc, typ, name, "api-key:"+m.Path, ev(m.Path+" includes apiKey instead of using managed identity", ref))
	case m.AuthIdentitySet:
	default:
		a.notes = append(a.notes, "absence of apiKey in a GET response is not proof of keyless authentication (Search GET redaction behaviour is unverified)")
	}
}

func hasIdentity(t string) bool {
	t = strings.TrimSpace(t)
	return t != "" && !strings.EqualFold(t, "None")
}

// eval009 checks document-level access. The ingestion leg needs the
// knowledge-source plane. The header leg is never decided from the
// repository: Learn sets x-ms-query-source-authorization on the agent MCP
// tool headers, a shape Foundry Doctor does not read, so it stays uncertain.
func eval009(in *sdk.Input) sdk.Result {
	req, declared := boolPolicy(in, "knowledge.requireDocumentLevelAccess")
	if !declared || !req {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-access-requirement-declared"}}
	}
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if !permissionIndexedSource(source.Kind) {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
		ref := dataRef("knowledgesources", source.Name, source.APIVersion)
		paramsKey := runtimeiq.ParametersKey(source.Kind)
		if !source.ParametersRead || !source.IngestionRead {
			a.notes = append(a.notes, "knowledge source "+source.Name+" response has no "+paramsKey+".ingestionParameters object")
			continue
		}
		if len(source.IngestionPermissionOptions) == 0 {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "permission-options", ev("permission-protected knowledge source must set "+paramsKey+".ingestionParameters.ingestionPermissionOptions", ref))
		} else if !validPermissionOptions(source.IngestionPermissionOptions) {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "permission-options-values", ev("ingestionPermissionOptions must use only documented values: userIds, groupIds, rbacScope, sensitivityLabels", ref))
		}
		if source.AssetStorePresent {
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "asset-store", ev("permission-enabled knowledge source cannot also set assetStore", ref))
		}
		switch source.APIVersion {
		case "2026-08-01-preview":
		case "2026-04-01":
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "api-version", ev("ingestionPermissionOptions are not supported by API version 2026-04-01; use 2026-08-01-preview", ref))
		default:
			a.notes = append(a.notes, "knowledge source "+source.Name+" uses api-version "+source.APIVersion+", for which Learn does not document ingestionPermissionOptions enforcement")
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-permission-indexed-knowledge-source"}}
	}
	a.notes = append(a.notes, "header-forwarding-configured-outside-repo: x-ms-query-source-authorization is set on the agent MCP tool headers, which are not read from repository metadata")
	return a.result()
}

func eval010(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		if !source.IsADLSGen2 {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
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
			a.add(account.SourceLocation, "Microsoft.Storage/storageAccounts", account.Name, "hns:"+source.Name,
				ev("ADLS Gen2 knowledge source requires hierarchical namespace enabled on the storage account",
					armRef("Microsoft.Storage/storageAccounts", account.Name, ""), dataRef("knowledgesources", source.Name, source.APIVersion)))
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: sdk.SkipInputUnavailable + ": no ADLS Gen2 knowledge source in the input"}}
	}
	return a.result()
}

// eval011 validates ingestionSchedule. The request format of interval is not
// verified (the status API reports "1d"), so a value the XSD duration parser
// does not recognise is uncertain; only a recognised value outside PT5M..P1D
// is a finding.
func eval011(in *sdk.Input) sdk.Result {
	snap, res, stop := liveSnapshot(in)
	if stop {
		return res
	}
	a := &acc{}
	seen := false
	for _, source := range snap.Sources {
		sched := source.RefreshSchedule
		if sched == nil {
			continue
		}
		seen = true
		if !knownVersion(a, "knowledge source", source.Name, source.APIVersion) {
			continue
		}
		ref := dataRef("knowledgesources", source.Name, source.APIVersion)
		interval := strings.TrimSpace(sched.Interval)
		mins, ok := parseSchedule(interval)
		switch {
		case interval == "":
			a.notes = append(a.notes, "ingestionSchedule of knowledge source "+source.Name+" has no interval")
		case !ok:
			a.notes = append(a.notes, "ingestionSchedule.interval of knowledge source "+source.Name+" is not an XSD dayTimeDuration the rule recognises")
		case mins < 5 || mins > 1440:
			a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "schedule-interval", ev("ingestionSchedule.interval must be between PT5M and P1D", ref))
		}
		if st := strings.TrimSpace(sched.StartTime); st != "" {
			t, err := time.Parse(time.RFC3339, st)
			if err != nil || t.UTC().Format(time.RFC3339) != st {
				a.add(locOf(source), "AzureAISearch/knowledgesources", source.Name, "schedule-start", ev("ingestionSchedule.startTime must be a UTC RFC3339 timestamp", ref))
			}
		}
	}
	if !seen {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "schedule-null"}}
	}
	return a.result()
}

// eval012 checks the Search service semantic settings. The tier leg uses the
// template only; the "semantic ranking is used" leg needs the knowledge-source
// plane and skips without it.
func eval012(in *sdk.Input) sdk.Result {
	snap, res, stop := snapshotOrProbeSkip(in)
	if stop {
		return res
	}
	if len(snap.Services) == 0 {
		return sdk.Result{Skipped: &sdk.Skip{Reason: "service-not-in-template-or-subscription"}}
	}
	a := &acc{}
	known := map[string]runtimeiq.SearchService{}
	for key, svc := range snap.Services {
		if !knownSearchManagementVersion(svc.APIVersion) {
			a.notes = append(a.notes, "search service "+svc.Name+" uses management api-version "+orNone(svc.APIVersion)+" outside the verified set")
			continue
		}
		known[key] = svc
		sem := strings.ToLower(strings.TrimSpace(svc.SemanticSearch))
		if (sem == "standard" || strings.EqualFold(svc.KnowledgeRetrieval, "standard")) && strings.EqualFold(svc.SKU, "free") {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "semantic-tier",
				ev("standard semanticSearch or knowledgeRetrieval requires the Basic tier or higher", armRef("Microsoft.Search/searchServices", svc.Name, svc.APIVersion)))
		}
	}
	if !snap.LiveRead {
		if len(a.findings) > 0 {
			return a.result()
		}
		return skipDataPlane()
	}
	used := map[string]string{}
	for key, kb := range snap.Bases {
		for _, name := range kb.KnowledgeSources {
			src, ok := snap.Sources[strings.ToLower(name)]
			if !ok || src.BackingIndex() == "" {
				continue
			}
			if !knownVersion(a, "knowledge source", src.Name, src.APIVersion) {
				continue
			}
			used[firstSegment(key)] = kb.Name
		}
	}
	if len(used) == 0 {
		if len(a.findings) > 0 || len(a.notes) > 0 {
			return a.result()
		}
		return sdk.Result{Skipped: &sdk.Skip{Reason: "no-index-backed-knowledge-source"}}
	}
	for service, kbName := range used {
		svc, ok := known[service]
		if !ok {
			if _, inTemplate := snap.Services[service]; !inTemplate {
				a.skips = append(a.skips, "service-not-in-template-or-subscription")
			}
			continue
		}
		sem := strings.ToLower(strings.TrimSpace(svc.SemanticSearch))
		if sem == "" {
			a.notes = append(a.notes, "management-api-version-without-property: search service "+svc.Name+" does not set semanticSearch in the template")
			continue
		}
		if compareAPIVersion(svc.APIVersion, "2026-03-01-preview") < 0 && sem == "disabled" {
			a.add(svc.SourceLocation, "Microsoft.Search/searchServices", svc.Name, "semantic-disabled",
				ev("semanticSearch disabled turns off semantic ranking used by agentic retrieval",
					armRef("Microsoft.Search/searchServices", svc.Name, svc.APIVersion), "data plane knowledge base "+kbName+" reads an index-backed knowledge source"))
		}
	}
	return a.result()
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

// isVectorField reports a vector field: one of the documented vector EDM
// types (supported-data-types, read 2026-10-18), or a field that declares
// dimensions or a vector search profile. Collection(Edm.String) and the other
// non-vector collections are not vector fields.
func isVectorField(f runtimeiq.Field) bool {
	return allowedVectorType(f.Type) || f.Dimensions > 0 || strings.TrimSpace(f.VectorSearchProfile) != ""
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

// deploymentFor correlates a vectorizer with a template deployment by the
// first host label of resourceUri. That label is the account name only for
// <account>.openai.azure.com style hosts; other hosts (APIM, custom domains)
// simply do not correlate, so the check yields no finding for them.
func deploymentFor(s runtimeiq.Snapshot, uri, deployment string) (runtimeiq.EmbeddingDeployment, bool) {
	host := hostLabel(uri)
	if host == "" || strings.TrimSpace(deployment) == "" {
		return runtimeiq.EmbeddingDeployment{}, false
	}
	d, ok := s.Deployments[strings.ToLower(host+"/"+deployment)]
	return d, ok
}

func hostLabel(uri string) string {
	uri = strings.TrimSpace(uri)
	uri = strings.TrimPrefix(uri, "https://")
	if i := strings.Index(uri, "."); i >= 0 {
		return strings.ToLower(uri[:i])
	}
	return ""
}

func supportedMediumRegion(region string) (supported, ok bool) {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "eastus2", "eastus", "southcentralus", "westus3", "westus2", "westus", "germanywestcentral", "northeurope", "switzerlandnorth", "swedencentral", "spaincentral", "uksouth", "koreacentral", "japaneast", "southeastasia":
		return true, true
	case "":
		return false, false
	default:
		return false, false
	}
}

func compareKnowledgeSourceLimit(sku string, count int) string {
	switch strings.ToLower(strings.TrimSpace(sku)) {
	case "free":
		if count > 3 {
			return "fail"
		}
	case "basic":
		switch {
		case count > 10:
			return "fail"
		case count > 5:
			return "uncertain"
		}
	default:
		if count > 10 {
			return "fail"
		}
	}
	return ""
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

func knownKnowledgeBaseVersion(v string) bool { return runtimeiq.KnownAPIVersion(v) }

func knownKnowledgeSourceVersion(v string) bool { return runtimeiq.KnownAPIVersion(v) }

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

func validPermissionOptions(options []string) bool {
	valid := map[string]bool{
		"userids":           true,
		"groupids":          true,
		"rbacscope":         true,
		"sensitivitylabels": true,
	}
	for _, option := range options {
		if !valid[strings.ToLower(strings.TrimSpace(option))] {
			return false
		}
	}
	return true
}

func storageFromConnection(s runtimeiq.Snapshot, resourceID string) runtimeiq.StorageAccount {
	resourceID = strings.ToLower(resourceID)
	if resourceID == "" {
		return runtimeiq.StorageAccount{}
	}
	for name, acct := range s.Storage {
		if strings.HasSuffix(resourceID, "/storageaccounts/"+strings.ToLower(name)) {
			return acct
		}
	}
	return runtimeiq.StorageAccount{}
}

var schedRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?)?$`)

func parseSchedule(v string) (int, bool) {
	m := schedRe.FindStringSubmatch(strings.TrimSpace(v))
	if len(m) == 0 || strings.TrimSpace(v) == "P" || strings.HasSuffix(strings.TrimSpace(v), "T") {
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

// OData filter field extraction for searchIndexParameters.baseFilter. String
// literals, typed literals (geography'...'), numbers, keywords, known
// functions and lambda range variables are not fields. Anything else the
// extractor does not understand (unknown functions, complex-field paths)
// returns ok=false so the rule reports uncertain instead of guessing.

// odataToken kind is 'i' (identifier), 's' (string literal), 'n' (other
// literal) or 'p' (punctuation); glued means no whitespace precedes it.
type odataToken struct {
	kind  byte
	text  string
	glued bool
}

var odataKeywords = map[string]bool{
	"and": true, "or": true, "not": true, "eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
	"true": true, "false": true, "null": true, "in": true,
}

var odataFunctions = map[string]bool{
	"search.in": true, "search.ismatch": true, "search.ismatchscoring": true, "search.score": true,
	"geo.distance": true, "geo.intersects": true,
}

func filterFieldsFromBase(expr string) ([]string, bool) {
	if strings.TrimSpace(expr) == "" {
		return nil, true
	}
	toks, ok := tokenizeOData(expr)
	if !ok {
		return nil, false
	}
	rangeVars := map[string]bool{}
	var out []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind != 'i' {
			continue
		}
		var next odataToken
		if i+1 < len(toks) {
			next = toks[i+1]
		}
		lower := strings.ToLower(t.text)
		if odataKeywords[lower] {
			continue
		}
		if next.kind == 's' && next.glued {
			i++
			continue
		}
		if next.kind == 'p' && next.text == "(" {
			if odataFunctions[lower] {
				continue
			}
			base, isLambda := lambdaCollection(t.text)
			if !isLambda || base == "" || strings.ContainsAny(base, "/.") {
				return nil, false
			}
			if !rangeVars[base] {
				out = append(out, base)
			}
			if i+3 < len(toks) && toks[i+2].kind == 'i' && toks[i+3].kind == 'p' && toks[i+3].text == ":" {
				rangeVars[toks[i+2].text] = true
				i += 3
			}
			continue
		}
		root := t.text
		if j := strings.Index(root, "/"); j >= 0 {
			root = root[:j]
		}
		if rangeVars[root] {
			continue
		}
		if strings.ContainsAny(t.text, "/.") {
			return nil, false
		}
		out = append(out, t.text)
	}
	return unique(out), true
}

func lambdaCollection(ident string) (string, bool) {
	lower := strings.ToLower(ident)
	for _, suffix := range []string{"/any", "/all"} {
		if strings.HasSuffix(lower, suffix) {
			return ident[:len(ident)-len(suffix)], true
		}
	}
	return "", false
}

func tokenizeOData(expr string) ([]odataToken, bool) {
	var out []odataToken
	prevEnd := -1
	i := 0
	for i < len(expr) {
		c := expr[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		start := i
		glued := start == prevEnd
		switch {
		case c == '\'':
			i++
			closed := false
			for i < len(expr) {
				if expr[i] == '\'' {
					if i+1 < len(expr) && expr[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
			out = append(out, odataToken{kind: 's', glued: glued})
		case isIdentStart(c):
			for i < len(expr) && (isIdentPart(expr[i]) || expr[i] == '.' || expr[i] == '/') {
				i++
			}
			out = append(out, odataToken{kind: 'i', text: expr[start:i], glued: glued})
		case c >= '0' && c <= '9' || c == '-':
			i++
			for i < len(expr) && (isIdentPart(expr[i]) || expr[i] == '.' || expr[i] == ':' || expr[i] == '+' || expr[i] == '-') {
				i++
			}
			out = append(out, odataToken{kind: 'n', glued: glued})
		case c == '(' || c == ')' || c == ',' || c == ':':
			i++
			out = append(out, odataToken{kind: 'p', text: string(c), glued: glued})
		default:
			return nil, false
		}
		prevEnd = i
	}
	return out, true
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
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

// knownSearchManagementVersion lists the Microsoft.Search management versions
// in the FND-IQ-012 catalogue entry.
func knownSearchManagementVersion(v string) bool {
	switch strings.TrimSpace(v) {
	case "2025-05-01", "2026-03-01-preview", "2026-09-01-preview":
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
