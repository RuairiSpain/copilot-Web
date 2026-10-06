// Package iq contains metadata-only helpers for Foundry IQ and Azure AI Search
// Phase 8 validation.
package iq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	rt "github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
	"go.yaml.in/yaml/v3"
)

const (
	scope                    = "https://search.azure.com/.default"
	apiVersionPreview        = "2026-08-01-preview"
	apiVersionStable         = "2026-04-01"
	modelVersionMgmt         = "2025-06-01"
	searchServiceVersionMgmt = "2026-09-01-preview"
)

type Client interface {
	GetKnowledgeBase(context.Context, string, string, string) (KnowledgeBase, error)
	GetKnowledgeSource(context.Context, string, string, string) (KnowledgeSource, error)
	GetIndex(context.Context, string, string, string) (Index, error)
	GetSkillset(context.Context, string, string, string) (Skillset, error)
}

type HTTPClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
}

type Connection struct {
	Name                    string
	Kind                    string
	Endpoint                string
	TargetKind              string
	SearchService           string
	KnowledgeBase           string
	APIVersion              string
	AuthType                string
	ForwardSourceAuth       bool
	SourceAuthUsesUserToken bool
	ForwardWorkIQAuth       bool
	Location                sdk.Location
}

type KnowledgeBase struct {
	Name                   string
	APIVersion             string
	Location               sdk.Location
	KnowledgeSources       []string
	RetrievalReasoningKind string
	OutputMode             string
	AnswerSynthesis        bool
	Models                 []ModelRef
	VectorQueries          []VectorQuery
	DeclaredSemanticUsage  bool
	UsesWebKnowledgeSource bool
	UsesIndexedKnowledge   bool
}

type ModelRef struct {
	Kind         string
	ResourceURI  string
	DeploymentID string
	ModelName    string
	APIKeySet    bool
}

type VectorQuery struct {
	Weight float64
}

type KnowledgeSource struct {
	Name                       string
	APIVersion                 string
	Location                   sdk.Location
	Kind                       string
	SearchIndexName            string
	SemanticConfigurationName  string
	BaseFilter                 string
	FilterFields               []string
	SecurityField              string
	ResourceIDConnection       string
	HasSecretConnection        bool
	AssetStorePresent          bool
	IngestionPermissionOptions []string
	IsADLSGen2                 bool
	RefreshSchedule            *Schedule
	SkillsetName               string
	SupportsChangeDetection    *bool
	VectorizerModelRefs        []ModelRef
}

type Schedule struct {
	Interval  string
	StartTime string
}

type Index struct {
	Name                         string
	APIVersion                   string
	Location                     sdk.Location
	Fields                       []Field
	DefaultSemanticConfiguration string
	SemanticConfigurations       []SemanticConfiguration
	VectorProfiles               []VectorProfile
	VectorAlgorithms             []string
	Vectorizers                  []Vectorizer
	Compressions                 []string
}

type Field struct {
	Name                string
	Type                string
	Key                 bool
	Searchable          bool
	Retrievable         bool
	Filterable          bool
	Facetable           bool
	Sortable            bool
	Dimensions          int
	VectorSearchProfile string
}

type SemanticConfiguration struct {
	Name          string
	ContentFields []string
}

type VectorProfile struct {
	Name        string
	Algorithm   string
	Vectorizer  string
	Compression string
}

type Vectorizer struct {
	Name         string
	Kind         string
	ResourceURI  string
	DeploymentID string
	ModelName    string
	APIKeySet    bool
}

type Skillset struct {
	Name       string
	APIVersion string
	Location   sdk.Location
	Skills     []Skill
}

type Skill struct {
	ODataType         string
	TextSplitMode     string
	Unit              string
	MaximumPageLength int
	PageOverlapLength int
	DeploymentID      string
	ModelName         string
	Dimensions        int
	APIKeySet         bool
}

type SearchService struct {
	Name               string
	ResourceID         string
	Location           string
	SKU                string
	SemanticSearch     string
	KnowledgeRetrieval string
	IdentityType       string
	APIVersion         string
	SourceLocation     sdk.Location
}

type StorageAccount struct {
	Name           string
	ResourceID     string
	IsHNSEnabled   *bool
	SourceLocation sdk.Location
}

type EmbeddingDeployment struct {
	Name           string
	AccountName    string
	ModelName      string
	APIVersion     string
	SourceLocation sdk.Location
}

type Snapshot struct {
	Connections []Connection
	Bases       map[string]KnowledgeBase
	Sources     map[string]KnowledgeSource
	Indexes     map[string]Index
	Skillsets   map[string]Skillset
	Services    map[string]SearchService
	Storage     map[string]StorageAccount
	Deployments map[string]EmbeddingDeployment
	ProbeIssues []string
}

func Endpoint(service string) (string, error) {
	if err := rt.ValidateDataPlaneName("search service", service); err != nil {
		return "", err
	}
	return "https://" + service + ".search.windows.net", nil
}

func (c HTTPClient) GetKnowledgeBase(ctx context.Context, service, name, apiVersion string) (KnowledgeBase, error) {
	apiVersion = requestedAPIVersion(apiVersion)
	var resp struct {
		Name                     string       `json:"name"`
		KnowledgeSources         []namedRef   `json:"knowledgeSources"`
		RetrievalReasoningEffort kindRef      `json:"retrievalReasoningEffort"`
		OutputMode               string       `json:"outputMode"`
		AnswerSynthesis          any          `json:"answerSynthesis"`
		Models                   []modelShape `json:"models"`
		VectorQueries            []struct {
			Weight float64 `json:"weight"`
		} `json:"vectorQueries"`
	}
	if err := c.get(ctx, service, "knowledgebases", name, apiVersion, &resp); err != nil {
		return KnowledgeBase{}, err
	}
	out := KnowledgeBase{
		Name:                   resp.Name,
		APIVersion:             apiVersion,
		RetrievalReasoningKind: strings.TrimSpace(resp.RetrievalReasoningEffort.Kind),
		OutputMode:             strings.TrimSpace(resp.OutputMode),
		AnswerSynthesis:        resp.AnswerSynthesis != nil,
	}
	for _, ks := range resp.KnowledgeSources {
		if n := strings.TrimSpace(ks.Name); n != "" {
			out.KnowledgeSources = append(out.KnowledgeSources, n)
		}
	}
	for _, m := range resp.Models {
		out.Models = append(out.Models, toModelRef(m))
	}
	for _, q := range resp.VectorQueries {
		out.VectorQueries = append(out.VectorQueries, VectorQuery{Weight: q.Weight})
	}
	return out, nil
}

func (c HTTPClient) GetKnowledgeSource(ctx context.Context, service, name, apiVersion string) (KnowledgeSource, error) {
	apiVersion = requestedAPIVersion(apiVersion)
	var resp struct {
		Name                  string         `json:"name"`
		Kind                  string         `json:"kind"`
		SearchIndexParameters map[string]any `json:"searchIndexParameters"`
		AzureBlobParameters   map[string]any `json:"azureBlobParameters"`
		IndexedSQLParameters  map[string]any `json:"indexedSqlParameters"`
		AssetStore            any            `json:"assetStore"`
		IngestionParameters   map[string]any `json:"ingestionParameters"`
		SkillsetName          string         `json:"skillsetName"`
		Models                []modelShape   `json:"models"`
	}
	if err := c.get(ctx, service, "knowledgesources", name, apiVersion, &resp); err != nil {
		return KnowledgeSource{}, err
	}
	out := KnowledgeSource{
		Name:              resp.Name,
		Kind:              strings.TrimSpace(resp.Kind),
		APIVersion:        apiVersion,
		AssetStorePresent: resp.AssetStore != nil,
		SkillsetName:      strings.TrimSpace(resp.SkillsetName),
	}
	ingestion := resp.IngestionParameters
	if m := resp.SearchIndexParameters; len(m) > 0 {
		out.SearchIndexName = stringAt(m["searchIndexName"])
		out.SemanticConfigurationName = stringAt(m["semanticConfigurationName"])
		out.BaseFilter = stringAt(m["baseFilter"])
		if qh, ok := m["queryHints"].(map[string]any); ok {
			if filters, ok := qh["filters"].([]any); ok {
				for _, raw := range filters {
					if fm, ok := raw.(map[string]any); ok {
						if field := stringAt(fm["field"]); field != "" {
							out.FilterFields = append(out.FilterFields, field)
						}
					}
				}
			}
		}
	}
	switch out.Kind {
	case "azureBlob":
		parseConnection(&out, resp.AzureBlobParameters)
		if b, ok := boolAt(resp.AzureBlobParameters["isADLSGen2"]); ok {
			out.IsADLSGen2 = b
		}
		if m, ok := mapAt(resp.AzureBlobParameters, "ingestionParameters"); ok {
			ingestion = m
			if _, ok := m["assetStore"]; ok {
				out.AssetStorePresent = true
			}
		}
	case "indexedSql":
		parseConnection(&out, resp.IndexedSQLParameters)
	case "indexedSharePoint":
		parseConnection(&out, resp.IngestionParameters)
	}
	if len(ingestion) > 0 {
		if raw, ok := ingestion["ingestionPermissionOptions"].([]any); ok {
			for _, v := range raw {
				if s := stringAt(v); s != "" {
					out.IngestionPermissionOptions = append(out.IngestionPermissionOptions, s)
				}
			}
		}
		if sched, ok := ingestion["ingestionSchedule"].(map[string]any); ok {
			out.RefreshSchedule = &Schedule{Interval: stringAt(sched["interval"]), StartTime: stringAt(sched["startTime"])}
		}
	}
	for _, m := range resp.Models {
		out.VectorizerModelRefs = append(out.VectorizerModelRefs, toModelRef(m))
	}
	return out, nil
}

func (c HTTPClient) GetIndex(ctx context.Context, service, name, apiVersion string) (Index, error) {
	apiVersion = requestedAPIVersion(apiVersion)
	var resp struct {
		Name   string `json:"name"`
		Fields []struct {
			Name                string `json:"name"`
			Type                string `json:"type"`
			Key                 bool   `json:"key"`
			Searchable          bool   `json:"searchable"`
			Retrievable         bool   `json:"retrievable"`
			Filterable          bool   `json:"filterable"`
			Facetable           bool   `json:"facetable"`
			Sortable            bool   `json:"sortable"`
			Dimensions          int    `json:"dimensions"`
			VectorSearchProfile string `json:"vectorSearchProfile"`
		} `json:"fields"`
		Semantic struct {
			DefaultConfiguration string `json:"defaultConfiguration"`
			Configurations       []struct {
				Name              string `json:"name"`
				PrioritizedFields struct {
					PrioritizedContentFields []struct {
						FieldName string `json:"fieldName"`
					} `json:"prioritizedContentFields"`
				} `json:"prioritizedFields"`
			} `json:"configurations"`
		} `json:"semantic"`
		VectorSearch struct {
			Profiles []struct {
				Name        string `json:"name"`
				Algorithm   string `json:"algorithm"`
				Vectorizer  string `json:"vectorizer"`
				Compression string `json:"compression"`
			} `json:"profiles"`
			Algorithms []struct {
				Name string `json:"name"`
			} `json:"algorithms"`
			Vectorizers  []modelShape `json:"vectorizers"`
			Compressions []struct {
				Name string `json:"name"`
			} `json:"compressions"`
		} `json:"vectorSearch"`
	}
	if err := c.get(ctx, service, "indexes", name, apiVersion, &resp); err != nil {
		return Index{}, err
	}
	out := Index{Name: resp.Name, APIVersion: apiVersion, DefaultSemanticConfiguration: strings.TrimSpace(resp.Semantic.DefaultConfiguration)}
	for _, f := range resp.Fields {
		out.Fields = append(out.Fields, Field{
			Name: f.Name, Type: f.Type, Key: f.Key, Searchable: f.Searchable, Retrievable: f.Retrievable,
			Filterable: f.Filterable, Facetable: f.Facetable, Sortable: f.Sortable, Dimensions: f.Dimensions,
			VectorSearchProfile: f.VectorSearchProfile,
		})
	}
	for _, cfg := range resp.Semantic.Configurations {
		item := SemanticConfiguration{Name: cfg.Name}
		for _, field := range cfg.PrioritizedFields.PrioritizedContentFields {
			if n := strings.TrimSpace(field.FieldName); n != "" {
				item.ContentFields = append(item.ContentFields, n)
			}
		}
		out.SemanticConfigurations = append(out.SemanticConfigurations, item)
	}
	for _, p := range resp.VectorSearch.Profiles {
		out.VectorProfiles = append(out.VectorProfiles, VectorProfile{Name: p.Name, Algorithm: p.Algorithm, Vectorizer: p.Vectorizer, Compression: p.Compression})
	}
	for _, a := range resp.VectorSearch.Algorithms {
		if n := strings.TrimSpace(a.Name); n != "" {
			out.VectorAlgorithms = append(out.VectorAlgorithms, n)
		}
	}
	for _, v := range resp.VectorSearch.Vectorizers {
		mv := toModelRef(v)
		out.Vectorizers = append(out.Vectorizers, Vectorizer{
			Name: v.Name, Kind: v.Kind, ResourceURI: mv.ResourceURI, DeploymentID: mv.DeploymentID, ModelName: mv.ModelName, APIKeySet: mv.APIKeySet,
		})
	}
	for _, comp := range resp.VectorSearch.Compressions {
		if n := strings.TrimSpace(comp.Name); n != "" {
			out.Compressions = append(out.Compressions, n)
		}
	}
	return out, nil
}

func (c HTTPClient) GetSkillset(ctx context.Context, service, name, apiVersion string) (Skillset, error) {
	apiVersion = requestedAPIVersion(apiVersion)
	var resp struct {
		Name   string `json:"name"`
		Skills []struct {
			ODataType         string `json:"@odata.type"`
			TextSplitMode     string `json:"textSplitMode"`
			Unit              string `json:"unit"`
			MaximumPageLength int    `json:"maximumPageLength"`
			PageOverlapLength int    `json:"pageOverlapLength"`
			DeploymentID      string `json:"deploymentId"`
			ModelName         string `json:"modelName"`
			Dimensions        int    `json:"dimensions"`
			APIKey            string `json:"apiKey"`
		} `json:"skills"`
	}
	if err := c.get(ctx, service, "skillsets", name, apiVersion, &resp); err != nil {
		return Skillset{}, err
	}
	out := Skillset{Name: resp.Name, APIVersion: apiVersion}
	for _, s := range resp.Skills {
		out.Skills = append(out.Skills, Skill{
			ODataType: s.ODataType, TextSplitMode: s.TextSplitMode, Unit: s.Unit,
			MaximumPageLength: s.MaximumPageLength, PageOverlapLength: s.PageOverlapLength,
			DeploymentID: s.DeploymentID, ModelName: s.ModelName, Dimensions: s.Dimensions,
			APIKeySet: strings.TrimSpace(s.APIKey) != "",
		})
	}
	return out, nil
}

func (c HTTPClient) get(ctx context.Context, service, family, name, apiVersion string, out any) error {
	base, err := Endpoint(service)
	if err != nil {
		return err
	}
	u := fmt.Sprintf("%s/%s('%s')?api-version=%s", base, family, url.PathEscape(name), apiVersion)
	return rt.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, ".search.windows.net", nil, out)
}

type namedRef struct {
	Name string `json:"name"`
}

type kindRef struct {
	Kind string `json:"kind"`
}

type modelShape struct {
	Name                  string `json:"name"`
	Kind                  string `json:"kind"`
	APIKey                string `json:"apiKey"`
	AzureOpenAIParameters struct {
		ResourceURI  string `json:"resourceUri"`
		DeploymentID string `json:"deploymentId"`
		ModelName    string `json:"modelName"`
	} `json:"azureOpenAIParameters"`
}

func toModelRef(m modelShape) ModelRef {
	return ModelRef{
		Kind:         strings.TrimSpace(m.Kind),
		ResourceURI:  strings.TrimSpace(m.AzureOpenAIParameters.ResourceURI),
		DeploymentID: strings.TrimSpace(m.AzureOpenAIParameters.DeploymentID),
		ModelName:    strings.TrimSpace(m.AzureOpenAIParameters.ModelName),
		APIKeySet:    strings.TrimSpace(m.APIKey) != "",
	}
}

func stringAt(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func mapAt(m map[string]any, key string) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	nested, ok := m[key].(map[string]any)
	return nested, ok
}

func boolAt(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}

func parseConnection(out *KnowledgeSource, m map[string]any) {
	if len(m) == 0 {
		return
	}
	conn := stringAt(m["connectionString"])
	if conn == "" {
		return
	}
	if id := ResourceIDFromConnectionString(conn); id != "" {
		out.ResourceIDConnection = id
	}
	out.HasSecretConnection = ConnectionUsesSecret(conn)
}

func requestedAPIVersion(apiVersion string) string {
	if strings.TrimSpace(apiVersion) == "" {
		return apiVersionPreview
	}
	return strings.TrimSpace(apiVersion)
}

func ResourceIDFromConnectionString(v string) string {
	for _, part := range strings.Split(v, ";") {
		key, val, ok := strings.Cut(part, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "ResourceId") {
			id := strings.TrimSpace(val)
			if strings.HasPrefix(strings.ToLower(id), "/subscriptions/") {
				return id
			}
		}
	}
	return ""
}

func ConnectionUsesSecret(v string) bool {
	s := strings.ToLower(v)
	for _, marker := range []string{"accountkey=", "sharedaccesssignature=", "clientsecret=", "************sig=", "user id=", "userid=", "uid=", "password=", "pwd="} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

const (
	ConnectionTargetKnowledgeBaseMCP      = "KnowledgeBaseMCP"
	ConnectionTargetCognitiveSearchTarget = "CognitiveSearchService"
)

type ParsedConnectionEndpoint struct {
	TargetKind    string
	SearchService string
	KnowledgeBase string
	APIVersion    string
}

var kbPathRe = regexp.MustCompile(`(?i)^/knowledgebases/([^/]+)/mcp/?$`)

func ParseConnectionEndpoint(raw string) (ParsedConnectionEndpoint, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return ParsedConnectionEndpoint{}, false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".search.windows.net") {
		return ParsedConnectionEndpoint{}, false
	}
	service := strings.TrimSuffix(host, ".search.windows.net")
	apiVersion := strings.TrimSpace(u.Query().Get("api-version"))
	switch strings.TrimSpace(u.EscapedPath()) {
	case "", "/":
		return ParsedConnectionEndpoint{TargetKind: ConnectionTargetCognitiveSearchTarget, SearchService: service, APIVersion: apiVersion}, true
	}
	match := kbPathRe.FindStringSubmatch(u.EscapedPath())
	if len(match) != 2 {
		return ParsedConnectionEndpoint{}, false
	}
	return ParsedConnectionEndpoint{TargetKind: ConnectionTargetKnowledgeBaseMCP, SearchService: service, KnowledgeBase: match[1], APIVersion: apiVersion}, true
}

func ParseKnowledgeBaseEndpoint(raw string) (service, kb string, ok bool) {
	parsed, ok := ParseConnectionEndpoint(raw)
	if !ok || parsed.TargetKind != ConnectionTargetKnowledgeBaseMCP {
		return "", "", false
	}
	return parsed.SearchService, parsed.KnowledgeBase, true
}

func SearchServicesFromARM(in *sdk.Input) map[string]SearchService {
	out := map[string]SearchService{}
	if in == nil || in.ARM == nil {
		return out
	}
	for _, r := range in.ARM.Resources() {
		if !strings.EqualFold(r.Type, "Microsoft.Search/searchServices") {
			continue
		}
		svc := SearchService{
			Name:           r.Name,
			Location:       r.Region,
			SKU:            r.SKUName,
			APIVersion:     r.APIVersion,
			SourceLocation: r.Location,
		}
		if v, ok := nestedString(r.Properties, "semanticSearch"); ok {
			svc.SemanticSearch = v
		}
		if v, ok := nestedString(r.Properties, "knowledgeRetrieval"); ok {
			svc.KnowledgeRetrieval = v
		}
		if typ, ok := nestedString(r.Identity, "type"); ok {
			svc.IdentityType = typ
		}
		out[strings.ToLower(r.Name)] = svc
	}
	return out
}

func StorageAccountsFromARM(in *sdk.Input) map[string]StorageAccount {
	out := map[string]StorageAccount{}
	if in == nil || in.ARM == nil {
		return out
	}
	for _, r := range in.ARM.Resources() {
		if !strings.EqualFold(r.Type, "Microsoft.Storage/storageAccounts") {
			continue
		}
		acct := StorageAccount{Name: r.Name, SourceLocation: r.Location}
		if b, ok := nestedBool(r.Properties, "isHnsEnabled"); ok {
			acct.IsHNSEnabled = &b
		}
		out[strings.ToLower(r.Name)] = acct
	}
	return out
}

func EmbeddingDeploymentsFromARM(in *sdk.Input) map[string]EmbeddingDeployment {
	out := map[string]EmbeddingDeployment{}
	if in == nil || in.ARM == nil {
		return out
	}
	for _, r := range in.ARM.Resources() {
		if !strings.EqualFold(r.Type, "Microsoft.CognitiveServices/accounts/deployments") {
			continue
		}
		dep := EmbeddingDeployment{
			Name:           lastSegment(r.Name),
			AccountName:    firstSegment(r.Name),
			APIVersion:     r.APIVersion,
			SourceLocation: r.Location,
		}
		if model, ok := nestedMap(r.Properties, "model"); ok {
			if name, ok := nestedString(model, "name"); ok {
				dep.ModelName = name
			}
		}
		out[strings.ToLower(dep.AccountName+"/"+dep.Name)] = dep
	}
	return out
}

func ConnectionsFromAzureYAML(in *sdk.Input) []Connection {
	d, ok := loadDoc(in)
	if !ok {
		return nil
	}
	var out []Connection
	services := child(d.Root(), "services")
	if services == nil || services.Kind != 4 {
		return nil
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		key, val := services.Content[i], services.Content[i+1]
		if strNode(val, "host") != "azure.ai.connection" {
			continue
		}
		target := strNode(val, "target")
		parsed, ok := ParseConnectionEndpoint(target)
		if !ok {
			continue
		}
		forwardSourceAuth, sourceAuthUsesUserToken, forwardWorkIQAuth := connectionAuthHeaders(val)
		out = append(out, Connection{
			Name:                    key.Value,
			Kind:                    "azure.ai.connection",
			Endpoint:                target,
			TargetKind:              parsed.TargetKind,
			SearchService:           parsed.SearchService,
			KnowledgeBase:           parsed.KnowledgeBase,
			APIVersion:              parsed.APIVersion,
			AuthType:                strNode(val, "authType"),
			ForwardSourceAuth:       forwardSourceAuth,
			SourceAuthUsesUserToken: sourceAuthUsesUserToken,
			ForwardWorkIQAuth:       forwardWorkIQAuth,
			Location:                sdk.Location{File: d.Path(), Line: key.Line, Column: key.Column},
		})
	}
	slices.SortFunc(out, func(a, b Connection) int { return strings.Compare(a.Name, b.Name) })
	return out
}

type document interface {
	sdk.AzureYAMLView
	Root() *yaml.Node
}

func loadDoc(in *sdk.Input) (document, bool) {
	if in == nil || in.AzureYAML == nil {
		return nil, false
	}
	d, ok := in.AzureYAML.(document)
	return d, ok && d.Root() != nil
}

const (
	yamlScalar  = 8
	yamlMapping = 4
)

func child(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yamlMapping {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func strNode(n *yaml.Node, key string) string {
	c := child(n, key)
	if c == nil || c.Kind != yamlScalar {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func connectionAuthHeaders(n *yaml.Node) (forwardSourceAuth, sourceAuthUsesUserToken, forwardWorkIQAuth bool) {
	for _, key := range []string{"headers", "requestHeaders", "forwardHeaders"} {
		h := child(n, key)
		if h == nil {
			continue
		}
		if present, usesUserToken := headerNamed(h, "x-ms-query-source-authorization"); present {
			forwardSourceAuth = true
			sourceAuthUsesUserToken = sourceAuthUsesUserToken || usesUserToken
		}
		if present, _ := headerNamed(h, "x-ms-query-work-iq-source-authorization"); present {
			forwardWorkIQAuth = true
		}
	}
	return forwardSourceAuth, sourceAuthUsesUserToken, forwardWorkIQAuth
}

func headerNamed(n *yaml.Node, name string) (present, usesUserToken bool) {
	switch {
	case n == nil:
		return false, false
	case n.Kind == yamlMapping:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if strings.EqualFold(n.Content[i].Value, name) {
				return true, scalarUsesUserToken(n.Content[i+1])
			}
			if present, usesUserToken := headerNamed(n.Content[i+1], name); present {
				return true, usesUserToken
			}
		}
	}
	return false, false
}

func scalarUsesUserToken(n *yaml.Node) bool {
	return n != nil && n.Kind == yamlScalar && strings.Contains(strings.ToLower(strings.TrimSpace(n.Value)), "user_token")
}

func nestedString(m map[string]any, path ...string) (string, bool) {
	cur := any(m)
	for _, p := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = next[p]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return strings.TrimSpace(s), ok
}

func nestedBool(m map[string]any, path ...string) (bool, bool) {
	cur := any(m)
	for _, p := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return false, false
		}
		cur, ok = next[p]
		if !ok {
			return false, false
		}
	}
	b, ok := cur.(bool)
	return b, ok
}

func nestedMap(m map[string]any, path ...string) (map[string]any, bool) {
	cur := any(m)
	for _, p := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = next[p]
		if !ok {
			return nil, false
		}
	}
	out, ok := cur.(map[string]any)
	return out, ok
}

func firstSegment(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[:i]
	}
	return s
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
