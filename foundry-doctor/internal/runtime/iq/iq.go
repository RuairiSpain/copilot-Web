// Package iq contains metadata-only helpers for Foundry IQ and Azure AI Search
// Phase 8 validation.
//
// Response shapes are parsed only for the Search data-plane API versions in
// KnownAPIVersion. The knowledge source and model shapes follow the Learn
// response examples read 2026-10-18 (agentic-knowledge-source-how-to-blob,
// -sharepoint-indexed, -onelake, -search-index and
// vector-search-vectorizer-azure-open-ai); see docs/iq-gateway.md.
package iq

import (
	"context"
	"encoding/json"
	"errors"
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
	scope      = "https://search.azure.com/.default"
	hostSuffix = ".search.windows.net"

	// KnownAPIVersionList names the versions KnownAPIVersion accepts, for
	// skip reasons and documentation.
	KnownAPIVersionList = "2026-04-01, 2026-05-01-preview, 2026-08-01-preview"
)

// ErrUnsupportedAPIVersion is returned before any request is sent when the
// requested api-version is outside KnownAPIVersion.
var ErrUnsupportedAPIVersion = errors.New("iq: unsupported Search data-plane api-version")

// ErrInvalidObjectName is returned before any request is sent when a Search
// object name is not safe to place in a URL.
var ErrInvalidObjectName = errors.New("iq: invalid Search object name")

// KnownAPIVersion reports whether v is a Search data-plane API version whose
// knowledge base, knowledge source, index and skillset shapes this package
// parses. Every other version fails safe: no request, uncertain rules.
func KnownAPIVersion(v string) bool {
	switch strings.TrimSpace(v) {
	case "2026-04-01", "2026-05-01-preview", "2026-08-01-preview":
		return true
	default:
		return false
	}
}

// objectNameRe bounds Search object names before they enter a URL (letters,
// digits, dashes and underscores, up to 128), as internal/runtime/search does.
var objectNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidObjectName reports whether name is safe to place in a Search URL.
func ValidObjectName(name string) bool { return objectNameRe.MatchString(name) }

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

// Connection is an azure.ai.connection service from azure.yaml. Header
// forwarding is not read here: Learn (foundry-iq-connect, 2026-10-18) sets
// x-ms-query-source-authorization on the agent MCP tool headers, not on the
// project connection.
type Connection struct {
	Name          string
	Kind          string
	Endpoint      string
	TargetKind    string
	SearchService string
	KnowledgeBase string
	APIVersion    string
	AuthType      string
	Category      string
	Audience      string
	Location      sdk.Location
}

type KnowledgeBase struct {
	Name                   string
	APIVersion             string
	Location               sdk.Location
	KnowledgeSources       []string
	RetrievalReasoningKind string
	OutputMode             string
	Models                 []ModelRef
	UsesWebKnowledgeSource bool
	UsesIndexedKnowledge   bool
}

// ModelRef is one Azure OpenAI (or AI Services) model reference. Path is the
// JSON path of the reference inside its object, for evidence.
type ModelRef struct {
	Kind            string
	ResourceURI     string
	DeploymentID    string
	ModelName       string
	APIKeySet       bool
	AuthIdentitySet bool
	Path            string
}

type KnowledgeSource struct {
	Name       string
	APIVersion string
	Location   sdk.Location
	Kind       string
	// ParametersRead is true when the kind-specific <kind>Parameters object
	// was present in the response; IngestionRead when its
	// ingestionParameters object was present.
	ParametersRead bool
	IngestionRead  bool
	// SearchIndexName is searchIndexParameters.searchIndexName (an existing
	// index); GeneratedIndexName and SkillsetName are
	// <kind>Parameters.createdResources.index and .skillset.
	SearchIndexName            string
	GeneratedIndexName         string
	SkillsetName               string
	SemanticConfigurationName  string
	BaseFilter                 string
	FilterFields               []string
	SecurityField              string
	ResourceIDConnection       string
	HasSecretConnection        bool
	ConnectionUnclassified     bool
	AssetStorePresent          bool
	IngestionPermissionOptions []string
	IsADLSGen2                 bool
	RefreshSchedule            *Schedule
	Models                     []ModelRef
}

// BackingIndex is the index the knowledge source reads: the existing index of
// a searchIndex source, otherwise the generated index.
func (k KnowledgeSource) BackingIndex() string {
	if k.SearchIndexName != "" {
		return k.SearchIndexName
	}
	return k.GeneratedIndexName
}

// ParametersKey is the JSON member that holds the kind-specific parameters.
func ParametersKey(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "searchindex":
		return "searchIndexParameters"
	case "azureblob":
		return "azureBlobParameters"
	case "indexedsharepoint":
		return "indexedSharePointParameters"
	case "indexedonelake":
		return "indexedOneLakeParameters"
	default:
		return ""
	}
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
	Name            string
	Kind            string
	ResourceURI     string
	DeploymentID    string
	ModelName       string
	APIKeySet       bool
	AuthIdentitySet bool
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

// Snapshot is the correlated Phase 8 view. LiveRead is true only when the
// opt-in data-plane probe ran; rules that need data-plane objects skip, never
// pass, when it is false.
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
	LiveRead    bool
}

func Endpoint(service string) (string, error) {
	if err := rt.ValidateDataPlaneName("search service", service); err != nil {
		return "", err
	}
	return "https://" + service + hostSuffix, nil
}

func (c HTTPClient) GetKnowledgeBase(ctx context.Context, service, name, apiVersion string) (KnowledgeBase, error) {
	var resp struct {
		Name                     string       `json:"name"`
		KnowledgeSources         []namedRef   `json:"knowledgeSources"`
		RetrievalReasoningEffort *kindRef     `json:"retrievalReasoningEffort"`
		OutputMode               string       `json:"outputMode"`
		Models                   []modelShape `json:"models"`
	}
	if err := c.get(ctx, service, "knowledgebases", name, apiVersion, &resp); err != nil {
		return KnowledgeBase{}, err
	}
	out := KnowledgeBase{
		Name:       resp.Name,
		APIVersion: strings.TrimSpace(apiVersion),
		OutputMode: strings.TrimSpace(resp.OutputMode),
	}
	if resp.RetrievalReasoningEffort != nil {
		out.RetrievalReasoningKind = strings.TrimSpace(resp.RetrievalReasoningEffort.Kind)
	}
	for _, ks := range resp.KnowledgeSources {
		if n := strings.TrimSpace(ks.Name); n != "" {
			out.KnowledgeSources = append(out.KnowledgeSources, n)
		}
	}
	for i, m := range resp.Models {
		out.Models = append(out.Models, toModelRef(m, fmt.Sprintf("models[%d]", i)))
	}
	return out, nil
}

func (c HTTPClient) GetKnowledgeSource(ctx context.Context, service, name, apiVersion string) (KnowledgeSource, error) {
	var resp struct {
		Name                        string         `json:"name"`
		Kind                        string         `json:"kind"`
		SearchIndexParameters       map[string]any `json:"searchIndexParameters"`
		AzureBlobParameters         map[string]any `json:"azureBlobParameters"`
		IndexedSharePointParameters map[string]any `json:"indexedSharePointParameters"`
		IndexedOneLakeParameters    map[string]any `json:"indexedOneLakeParameters"`
	}
	if err := c.get(ctx, service, "knowledgesources", name, apiVersion, &resp); err != nil {
		return KnowledgeSource{}, err
	}
	out := KnowledgeSource{
		Name:       resp.Name,
		Kind:       strings.TrimSpace(resp.Kind),
		APIVersion: strings.TrimSpace(apiVersion),
	}
	var params map[string]any
	switch strings.ToLower(out.Kind) {
	case "searchindex":
		params = resp.SearchIndexParameters
		if params != nil {
			out.SearchIndexName = stringAt(params["searchIndexName"])
			out.SemanticConfigurationName = stringAt(params["semanticConfigurationName"])
			out.BaseFilter = stringAt(params["baseFilter"])
			if qh, ok := params["queryHints"].(map[string]any); ok {
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
	case "azureblob":
		params = resp.AzureBlobParameters
		if b, ok := boolAt(params["isADLSGen2"]); ok {
			out.IsADLSGen2 = b
		}
		parseConnection(&out, params)
	case "indexedsharepoint":
		params = resp.IndexedSharePointParameters
		parseConnection(&out, params)
	case "indexedonelake":
		params = resp.IndexedOneLakeParameters
	}
	if params == nil {
		return out, nil
	}
	out.ParametersRead = true
	prefix := ParametersKey(out.Kind)
	parseIngestion(&out, params, prefix)
	if created, ok := mapAt(params, "createdResources"); ok {
		out.GeneratedIndexName = stringAt(created["index"])
		out.SkillsetName = stringAt(created["skillset"])
	}
	return out, nil
}

func parseIngestion(out *KnowledgeSource, params map[string]any, prefix string) {
	ing, ok := mapAt(params, "ingestionParameters")
	if !ok {
		return
	}
	out.IngestionRead = true
	if raw, ok := ing["ingestionPermissionOptions"].([]any); ok {
		for _, v := range raw {
			if s := stringAt(v); s != "" {
				out.IngestionPermissionOptions = append(out.IngestionPermissionOptions, s)
			}
		}
	}
	if v, ok := ing["assetStore"]; ok && v != nil {
		out.AssetStorePresent = true
	}
	if sched, ok := ing["ingestionSchedule"].(map[string]any); ok {
		out.RefreshSchedule = &Schedule{Interval: stringAt(sched["interval"]), StartTime: stringAt(sched["startTime"])}
	}
	for _, key := range []string{"embeddingModel", "chatCompletionModel"} {
		raw, ok := ing[key].(map[string]any)
		if !ok {
			continue
		}
		var m modelShape
		if decodeMap(raw, &m) {
			out.Models = append(out.Models, toModelRef(m, prefix+".ingestionParameters."+key))
		}
	}
	if ai, ok := ing["aiServices"].(map[string]any); ok {
		out.Models = append(out.Models, ModelRef{
			Kind:        "aiServices",
			ResourceURI: stringAt(ai["uri"]),
			APIKeySet:   stringAt(ai["apiKey"]) != "",
			Path:        prefix + ".ingestionParameters.aiServices",
		})
	}
}

func (c HTTPClient) GetIndex(ctx context.Context, service, name, apiVersion string) (Index, error) {
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
	out := Index{Name: resp.Name, APIVersion: strings.TrimSpace(apiVersion), DefaultSemanticConfiguration: strings.TrimSpace(resp.Semantic.DefaultConfiguration)}
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
	for i, v := range resp.VectorSearch.Vectorizers {
		mv := toModelRef(v, fmt.Sprintf("vectorSearch.vectorizers[%d]", i))
		out.Vectorizers = append(out.Vectorizers, Vectorizer{
			Name: v.Name, Kind: v.Kind, ResourceURI: mv.ResourceURI, DeploymentID: mv.DeploymentID, ModelName: mv.ModelName,
			APIKeySet: mv.APIKeySet, AuthIdentitySet: mv.AuthIdentitySet,
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
	out := Skillset{Name: resp.Name, APIVersion: strings.TrimSpace(apiVersion)}
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

// get reads one object definition. The api-version must be known and the
// object name must pass ValidObjectName; nothing is sent otherwise. Knowledge
// objects use the documented /knowledgebases/{name} and
// /knowledgesources/{name} paths; indexes and skillsets use the OData key form
// already used by the Phase 3 Search probe.
func (c HTTPClient) get(ctx context.Context, service, family, name, apiVersion string, out any) error {
	apiVersion = strings.TrimSpace(apiVersion)
	if !KnownAPIVersion(apiVersion) {
		return fmt.Errorf("%w: %q", ErrUnsupportedAPIVersion, apiVersion)
	}
	if !ValidObjectName(name) {
		return fmt.Errorf("%w: %s %q", ErrInvalidObjectName, family, name)
	}
	base, err := Endpoint(service)
	if err != nil {
		return err
	}
	var path string
	switch family {
	case "knowledgebases", "knowledgesources":
		path = "/" + family + "/" + url.PathEscape(name)
	default:
		path = "/" + family + "('" + url.PathEscape(name) + "')"
	}
	q := url.Values{}
	q.Set("api-version", apiVersion)
	return rt.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, base+path+"?"+q.Encode(), hostSuffix, nil, out)
}

type namedRef struct {
	Name string `json:"name"`
}

type kindRef struct {
	Kind string `json:"kind"`
}

// modelShape is a vectorizer, knowledge base model or ingestion model. apiKey
// and authIdentity live inside azureOpenAIParameters
// (vector-search-vectorizer-azure-open-ai, read 2026-10-18).
type modelShape struct {
	Name                  string      `json:"name"`
	Kind                  string      `json:"kind"`
	AzureOpenAIParameters *aoaiParams `json:"azureOpenAIParameters"`
}

type aoaiParams struct {
	ResourceURI  string          `json:"resourceUri"`
	DeploymentID string          `json:"deploymentId"`
	ModelName    string          `json:"modelName"`
	APIKey       *string         `json:"apiKey"`
	AuthIdentity json.RawMessage `json:"authIdentity"`
}

func toModelRef(m modelShape, path string) ModelRef {
	out := ModelRef{Kind: strings.TrimSpace(m.Kind), Path: path}
	if p := m.AzureOpenAIParameters; p != nil {
		out.ResourceURI = strings.TrimSpace(p.ResourceURI)
		out.DeploymentID = strings.TrimSpace(p.DeploymentID)
		out.ModelName = strings.TrimSpace(p.ModelName)
		out.APIKeySet = p.APIKey != nil && strings.TrimSpace(*p.APIKey) != ""
		raw := strings.TrimSpace(string(p.AuthIdentity))
		out.AuthIdentitySet = raw != "" && raw != "null"
	}
	return out
}

func decodeMap(in map[string]any, out any) bool {
	data, err := json.Marshal(in)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
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

// parseConnection classifies connectionString without keeping it: a
// ResourceId= value is managed identity, a known secret marker is
// secret-backed, anything else (including a redacted value) is unclassified.
func parseConnection(out *KnowledgeSource, m map[string]any) {
	if len(m) == 0 {
		return
	}
	conn := stringAt(m["connectionString"])
	if conn == "" {
		return
	}
	switch {
	case ConnectionUsesSecret(conn):
		out.HasSecretConnection = true
		if id := ResourceIDFromConnectionString(conn); id != "" {
			out.ResourceIDConnection = id
		}
	case ResourceIDFromConnectionString(conn) != "":
		out.ResourceIDConnection = ResourceIDFromConnectionString(conn)
	default:
		out.ConnectionUnclassified = true
	}
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
	for _, marker := range []string{"accountkey=", "sharedaccesssignature=", "clientsecret=", "sig=", "user id=", "userid=", "uid=", "password=", "pwd="} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

const (
	ConnectionTargetKnowledgeBaseMCP      = "KnowledgeBaseMCP"
	ConnectionTargetCognitiveSearchTarget = "CognitiveSearchService"
	// ConnectionTargetUnresolved marks a Search-looking or RemoteTool
	// connection whose target is still a ${VAR} reference.
	ConnectionTargetUnresolved = "Unresolved"
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
	if !strings.HasSuffix(host, hostSuffix) {
		return ParsedConnectionEndpoint{}, false
	}
	service := strings.TrimSuffix(host, hostSuffix)
	apiVersion := strings.TrimSpace(u.Query().Get("api-version"))
	switch strings.TrimSpace(u.EscapedPath()) {
	case "", "/":
		return ParsedConnectionEndpoint{TargetKind: ConnectionTargetCognitiveSearchTarget, SearchService: service, APIVersion: apiVersion}, true
	}
	match := kbPathRe.FindStringSubmatch(u.EscapedPath())
	if len(match) != 2 {
		return ParsedConnectionEndpoint{}, false
	}
	kb, err := url.PathUnescape(match[1])
	if err != nil || !ValidObjectName(kb) {
		return ParsedConnectionEndpoint{}, false
	}
	return ParsedConnectionEndpoint{TargetKind: ConnectionTargetKnowledgeBaseMCP, SearchService: service, KnowledgeBase: kb, APIVersion: apiVersion}, true
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
	if services == nil || services.Kind != yamlMapping {
		return nil
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		key, val := services.Content[i], services.Content[i+1]
		if strNode(val, "host") != "azure.ai.connection" {
			continue
		}
		target := strNode(val, "target")
		conn := Connection{
			Name:     key.Value,
			Kind:     "azure.ai.connection",
			Endpoint: target,
			AuthType: strNode(val, "authType"),
			Category: strNode(val, "category"),
			Audience: strNode(val, "audience"),
			Location: sdk.Location{File: d.Path(), Line: key.Line, Column: key.Column},
		}
		if strings.Contains(target, "${") {
			lower := strings.ToLower(target)
			if strings.Contains(lower, hostSuffix) || strings.Contains(lower, "/knowledgebases/") || strings.EqualFold(conn.Category, "RemoteTool") {
				conn.TargetKind = ConnectionTargetUnresolved
				out = append(out, conn)
			}
			continue
		}
		parsed, ok := ParseConnectionEndpoint(target)
		if !ok {
			continue
		}
		conn.TargetKind = parsed.TargetKind
		conn.SearchService = parsed.SearchService
		conn.KnowledgeBase = parsed.KnowledgeBase
		conn.APIVersion = parsed.APIVersion
		out = append(out, conn)
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
