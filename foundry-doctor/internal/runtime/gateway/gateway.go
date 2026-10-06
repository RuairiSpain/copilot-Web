// Package gateway contains ARM-based normalisers for API Management gateway
// validation in Phase 8.
package gateway

import (
	"encoding/xml"
	"net/url"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

type Snapshot struct {
	Services        map[string]*Service
	UserAssigned    map[string]ManagedIdentity
	FoundryAccounts map[string]FoundryAccount
	AppInsights     map[string]AppInsightsComponent
	RoleAssignments []RoleAssignment
}

type Service struct {
	Name               string
	SKU                string
	IdentityType       string
	PrincipalID        string
	UserAssignedIDs    []string
	SourceLocation     sdk.Location
	Policies           map[string]PolicyDocument
	APIs               map[string]*API
	Operations         map[string]Operation
	VersionSets        map[string]VersionSet
	Backends           map[string]Backend
	Loggers            map[string]Logger
	Diagnostics        []Diagnostic
	MonitorDiagnostics []MonitorDiagnostic
	ProductAPIs        map[string][]string
}

type API struct {
	Name               string
	BaseName           string
	Path               string
	ServiceURL         string
	VersionSetID       string
	HasOperationPolicy bool
	SourceLocation     sdk.Location
}

type Operation struct {
	Name           string
	APIName        string
	SourceLocation sdk.Location
}

type VersionSet struct {
	ID               string
	VersioningScheme string
}

type Backend struct {
	Name                            string
	URL                             string
	HeaderNames                     []string
	PoolMemberIDs                   []string
	AuthorizationCredentialsPresent bool
	SourceLocation                  sdk.Location
}

type Logger struct {
	Name           string
	LoggerType     string
	ResourceID     string
	SourceLocation sdk.Location
}

type Diagnostic struct {
	Scope              string
	LoggerID           string
	Metrics            bool
	LargeLanguageModel bool
	SourceLocation     sdk.Location
}

type MonitorDiagnostic struct {
	WorkspaceID    string
	Scope          string
	SourceLocation sdk.Location
}

type ManagedIdentity struct {
	ResourceID  string
	ClientID    string
	PrincipalID string
}

type FoundryAccount struct {
	Name           string
	ResourceID     string
	SourceLocation sdk.Location
}

type AppInsightsComponent struct {
	Name           string
	ResourceID     string
	SourceLocation sdk.Location
}

type RoleAssignment struct {
	PrincipalID      string
	RoleDefinitionID string
	Scope            string
	Condition        string
	SourceLocation   sdk.Location
}

type PolicyDocument struct {
	Scope          string
	Format         string
	ExternalLink   bool
	Value          string
	SourceLocation sdk.Location
}

type EffectivePolicy struct {
	External bool
	Inbound  []PolicyElement
}

type PolicyElement struct {
	Name     string
	Text     string
	Attrs    map[string]string
	Children []PolicyElement
}

func BuildSnapshot(in *sdk.Input) Snapshot {
	out := Snapshot{
		Services:        map[string]*Service{},
		UserAssigned:    map[string]ManagedIdentity{},
		FoundryAccounts: map[string]FoundryAccount{},
		AppInsights:     map[string]AppInsightsComponent{},
	}
	if in == nil || in.ARM == nil {
		return out
	}
	for _, r := range in.ARM.Resources() {
		switch strings.ToLower(r.Type) {
		case "microsoft.apimanagement/service":
			name := strings.ToLower(r.Name)
			svc := &Service{
				Name:           r.Name,
				SKU:            r.SKUName,
				SourceLocation: r.Location,
				Policies:       map[string]PolicyDocument{},
				APIs:           map[string]*API{},
				Operations:     map[string]Operation{},
				VersionSets:    map[string]VersionSet{},
				Backends:       map[string]Backend{},
				Loggers:        map[string]Logger{},
				ProductAPIs:    map[string][]string{},
			}
			if typ, ok := nestedString(r.Identity, "type"); ok {
				svc.IdentityType = typ
			}
			if pid, ok := nestedString(r.Identity, "principalId"); ok {
				svc.PrincipalID = pid
			}
			for key := range nestedMapKeys(r.Identity, "userAssignedIdentities") {
				svc.UserAssignedIDs = append(svc.UserAssignedIDs, key)
			}
			out.Services[name] = svc
		case "microsoft.apimanagement/service/apis":
			svc, apiName := serviceChild(r.Name)
			if svc == "" || apiName == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			api := &API{
				Name:           apiName,
				BaseName:       baseAPIName(apiName),
				Path:           strProp(r.Properties, "path"),
				ServiceURL:     strProp(r.Properties, "serviceUrl"),
				VersionSetID:   strProp(r.Properties, "apiVersionSetId"),
				SourceLocation: r.Location,
			}
			s.APIs[strings.ToLower(apiName)] = api
		case "microsoft.apimanagement/service/apis/operations":
			parts := strings.Split(r.Name, "/")
			if len(parts) != 3 {
				continue
			}
			s := ensureService(out.Services, parts[0])
			s.Operations[strings.ToLower(parts[1]+"/"+parts[2])] = Operation{
				Name:           parts[2],
				APIName:        parts[1],
				SourceLocation: r.Location,
			}
		case "microsoft.apimanagement/service/apiversionsets":
			svc, vsName := serviceChild(r.Name)
			if svc == "" || vsName == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.VersionSets[strings.ToLower(vsName)] = VersionSet{ID: resourceIDLike(r), VersioningScheme: strProp(r.Properties, "versioningScheme")}
		case "microsoft.apimanagement/service/backends":
			svc, backendName := serviceChild(r.Name)
			if svc == "" || backendName == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			b := Backend{Name: backendName, URL: strProp(r.Properties, "url"), SourceLocation: r.Location}
			if hdrs, ok := nestedMap(r.Properties, "credentials", "header"); ok {
				for k := range hdrs {
					b.HeaderNames = append(b.HeaderNames, k)
				}
			}
			if _, ok := nestedMap(r.Properties, "credentials", "authorization"); ok {
				b.AuthorizationCredentialsPresent = true
			}
			if pool, ok := nestedMap(r.Properties, "pool"); ok {
				if members, ok := pool["services"].([]any); ok {
					for _, raw := range members {
						if mm, ok := raw.(map[string]any); ok {
							if id := strAny(mm["id"]); id != "" {
								b.PoolMemberIDs = append(b.PoolMemberIDs, id)
							}
						}
					}
				}
			}
			s.Backends[strings.ToLower(backendName)] = b
		case "microsoft.apimanagement/service/loggers":
			svc, loggerName := serviceChild(r.Name)
			if svc == "" || loggerName == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.Loggers[strings.ToLower(loggerName)] = Logger{
				Name:           loggerName,
				LoggerType:     strProp(r.Properties, "loggerType"),
				ResourceID:     strProp(r.Properties, "resourceId"),
				SourceLocation: r.Location,
			}
		case "microsoft.apimanagement/service/diagnostics", "microsoft.apimanagement/service/apis/diagnostics":
			scope := policyScope(r.Type, r.Name)
			svc, _ := serviceChild(r.Name)
			if svc == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.Diagnostics = append(s.Diagnostics, Diagnostic{
				Scope:              scope,
				LoggerID:           strProp(r.Properties, "loggerId"),
				Metrics:            boolProp(r.Properties, "metrics"),
				LargeLanguageModel: nestedBoolValue(r.Properties, "largeLanguageModel"),
				SourceLocation:     r.Location,
			})
		case "microsoft.apimanagement/service/policies", "microsoft.apimanagement/service/apis/policies", "microsoft.apimanagement/service/products/policies", "microsoft.apimanagement/service/apis/operations/policies":
			scope := policyScope(r.Type, r.Name)
			svc, _ := serviceChild(r.Name)
			if svc == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.Policies[scope] = PolicyDocument{
				Scope:          scope,
				Format:         strProp(r.Properties, "format"),
				ExternalLink:   strings.Contains(strings.ToLower(strProp(r.Properties, "format")), "link"),
				Value:          strProp(r.Properties, "value"),
				SourceLocation: r.Location,
			}
			if strings.Count(strings.Trim(r.Name, "/"), "/") >= 3 && strings.HasSuffix(strings.ToLower(r.Type), "/operations/policies") {
				apiName := operationAPIName(r.Name)
				if apiName != "" {
					if api := s.APIs[strings.ToLower(apiName)]; api != nil {
						api.HasOperationPolicy = true
					}
				}
			}
		case "microsoft.apimanagement/service/products/apis":
			parts := strings.Split(r.Name, "/")
			if len(parts) != 3 {
				continue
			}
			s := ensureService(out.Services, parts[0])
			api := strings.ToLower(parts[2])
			s.ProductAPIs[api] = append(s.ProductAPIs[api], strings.ToLower(parts[1]))
		case "microsoft.managedidentity/userassignedidentities":
			id := syntheticResourceID(r)
			mi := ManagedIdentity{
				ResourceID:  id,
				ClientID:    strProp(r.Properties, "clientId"),
				PrincipalID: strProp(r.Properties, "principalId"),
			}
			out.UserAssigned[strings.ToLower(id)] = mi
			out.UserAssigned[strings.ToLower(r.Name)] = mi
			out.UserAssigned[strings.ToLower(lastSegment(id))] = mi
		case "microsoft.authorization/roleassignments":
			out.RoleAssignments = append(out.RoleAssignments, RoleAssignment{
				PrincipalID:      strProp(r.Properties, "principalId"),
				RoleDefinitionID: strProp(r.Properties, "roleDefinitionId"),
				Scope:            chooseNonEmpty(r.Scope, strProp(r.Properties, "scope")),
				Condition:        strProp(r.Properties, "condition"),
				SourceLocation:   r.Location,
			})
		case "microsoft.cognitiveservices/accounts":
			acct := FoundryAccount{Name: r.Name, ResourceID: syntheticResourceID(r), SourceLocation: r.Location}
			out.FoundryAccounts[strings.ToLower(r.Name)] = acct
		case "microsoft.insights/components":
			comp := AppInsightsComponent{Name: r.Name, ResourceID: syntheticResourceID(r), SourceLocation: r.Location}
			out.AppInsights[strings.ToLower(r.Name)] = comp
			out.AppInsights[strings.ToLower(comp.ResourceID)] = comp
		case "microsoft.insights/diagnosticsettings":
			scope := strings.ToLower(strings.TrimSpace(r.Scope))
			if !strings.Contains(scope, "/providers/microsoft.apimanagement/service/") {
				continue
			}
			svcName := apimServiceNameFromID(scope)
			if svcName == "" {
				continue
			}
			s := ensureService(out.Services, svcName)
			s.MonitorDiagnostics = append(s.MonitorDiagnostics, MonitorDiagnostic{
				WorkspaceID:    strProp(r.Properties, "workspaceId"),
				Scope:          scope,
				SourceLocation: r.Location,
			})
		}
	}
	return out
}

func ensureService(m map[string]*Service, name string) *Service {
	key := strings.ToLower(name)
	if s, ok := m[key]; ok {
		return s
	}
	s := &Service{Policies: map[string]PolicyDocument{}, APIs: map[string]*API{}, Operations: map[string]Operation{}, VersionSets: map[string]VersionSet{}, Backends: map[string]Backend{}, Loggers: map[string]Logger{}, ProductAPIs: map[string][]string{}}
	m[key] = s
	return s
}

func EffectivePolicyForAPI(svc *Service, api *API) (EffectivePolicy, bool) {
	if svc == nil || api == nil {
		return EffectivePolicy{}, false
	}
	serviceRoot, serviceDoc := parsePolicy(svc.Policies["service"])
	apiRoot, apiDoc := parsePolicy(svc.Policies["api:"+strings.ToLower(api.Name)])
	hadPolicyDocument := svc.Policies["service"].Value != "" || svc.Policies["api:"+strings.ToLower(api.Name)].Value != ""
	var external bool
	if serviceDoc.ExternalLink || apiDoc.ExternalLink {
		return EffectivePolicy{External: true}, true
	}
	policies := append([]PolicyElement{}, serviceRoot...)
	for _, product := range svc.ProductAPIs[strings.ToLower(api.Name)] {
		doc := svc.Policies["product:"+product]
		hadPolicyDocument = hadPolicyDocument || doc.Value != ""
		if doc.ExternalLink {
			external = true
			continue
		}
		root, _ := parsePolicy(doc)
		if len(root) == 0 {
			continue
		}
		policies = append(policies, expandBase(root, serviceRoot)...)
	}
	apiPolicies := policies
	if len(apiRoot) > 0 {
		apiPolicies = expandBase(apiRoot, policies)
	}
	policies = append([]PolicyElement{}, apiPolicies...)
	for scope, doc := range svc.Policies {
		if !strings.HasPrefix(scope, "operation:"+strings.ToLower(api.Name)+"/") {
			continue
		}
		hadPolicyDocument = true
		if doc.ExternalLink {
			external = true
			continue
		}
		root, _ := parsePolicy(doc)
		if len(root) == 0 {
			continue
		}
		policies = append(policies, expandBase(root, apiPolicies)...)
	}
	if len(policies) == 0 && !api.HasOperationPolicy && !hadPolicyDocument {
		return EffectivePolicy{External: external, Inbound: nil}, false
	}
	return EffectivePolicy{External: external, Inbound: dedupePolicyElements(policies)}, true
}

func parsePolicy(doc PolicyDocument) ([]PolicyElement, PolicyDocument) {
	if doc.Value == "" || doc.ExternalLink {
		return nil, doc
	}
	var root xmlPolicy
	if err := xml.Unmarshal([]byte(doc.Value), &root); err != nil {
		return nil, doc
	}
	for _, child := range root.Children {
		if child.XMLName.Local == "inbound" {
			out := make([]PolicyElement, 0, len(child.Children))
			for _, c := range child.Children {
				out = append(out, c.toPolicyElement())
			}
			return out, doc
		}
	}
	return nil, doc
}

type xmlPolicy struct {
	XMLName  xml.Name
	Children []xmlElement `xml:",any"`
}

type xmlElement struct {
	XMLName  xml.Name
	Attrs    []xml.Attr   `xml:",any,attr"`
	Children []xmlElement `xml:",any"`
	Text     string       `xml:",chardata"`
}

func expandBase(child, parent []PolicyElement) []PolicyElement {
	if len(child) == 0 {
		return parent
	}
	out := make([]PolicyElement, 0, len(child)+len(parent))
	for _, el := range child {
		if el.Name == "base" {
			out = append(out, parent...)
			continue
		}
		out = append(out, el)
	}
	return out
}

func (x xmlElement) toPolicyElement() PolicyElement {
	out := PolicyElement{Name: x.XMLName.Local, Text: strings.TrimSpace(x.Text), Attrs: map[string]string{}}
	for _, a := range x.Attrs {
		out.Attrs[a.Name.Local] = strings.TrimSpace(a.Value)
	}
	for _, c := range x.Children {
		out.Children = append(out.Children, c.toPolicyElement())
	}
	return out
}

func APIMHostLikely(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return strings.HasSuffix(host, ".openai.azure.com") || strings.HasSuffix(host, ".services.ai.azure.com") || strings.HasSuffix(host, ".cognitiveservices.azure.com")
}

func HostFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func (s *Service) AIAPIBacked(api *API) bool {
	if api == nil {
		return false
	}
	if APIMHostLikely(HostFromURL(api.ServiceURL)) {
		return true
	}
	pol, ok := EffectivePolicyForAPI(s, api)
	if !ok || pol.External {
		return false
	}
	for _, el := range pol.Inbound {
		if el.Name != "set-backend-service" {
			continue
		}
		if APIMHostLikely(HostFromURL(el.Attrs["base-url"])) {
			return true
		}
		backendID := strings.ToLower(strings.TrimSpace(el.Attrs["backend-id"]))
		if backendID == "" {
			continue
		}
		if backendAIBacked(s, backendID, map[string]bool{}) {
			return true
		}
	}
	return false
}

func backendAIBacked(s *Service, id string, seen map[string]bool) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if s == nil || id == "" || seen[id] {
		return false
	}
	seen[id] = true
	backend, ok := s.Backends[id]
	if !ok {
		backend, ok = s.Backends[lastSegment(id)]
	}
	if !ok {
		return false
	}
	if APIMHostLikely(HostFromURL(backend.URL)) {
		return true
	}
	for _, member := range backend.PoolMemberIDs {
		if backendAIBacked(s, member, seen) {
			return true
		}
	}
	return false
}

func FindElements(nodes []PolicyElement, name string) []PolicyElement {
	var out []PolicyElement
	for _, node := range nodes {
		if node.Name == name {
			out = append(out, node)
		}
		out = append(out, FindElements(node.Children, name)...)
	}
	return out
}

func HasExpression(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "@(") || strings.Contains(v, "{{")
}

func resourceIDLike(r sdk.ARMResource) string {
	return strings.ToLower("/providers/" + r.Type + "/" + r.Name)
}

func syntheticResourceID(r sdk.ARMResource) string {
	return strings.TrimRight(resourceIDLike(r), "/")
}

func chooseNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func serviceChild(name string) (string, string) {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func operationAPIName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[1]
}

func baseAPIName(name string) string {
	if i := strings.Index(strings.ToLower(name), ";rev="); i >= 0 {
		return name[:i]
	}
	return name
}

func policyScope(typ, name string) string {
	parts := strings.Split(name, "/")
	switch strings.ToLower(typ) {
	case "microsoft.apimanagement/service/policies", "microsoft.apimanagement/service/diagnostics":
		return "service"
	case "microsoft.apimanagement/service/products/policies":
		return "product:" + strings.ToLower(parts[1])
	case "microsoft.apimanagement/service/apis/policies", "microsoft.apimanagement/service/apis/diagnostics":
		return "api:" + strings.ToLower(parts[1])
	case "microsoft.apimanagement/service/apis/operations/policies":
		if len(parts) < 4 {
			return strings.ToLower(name)
		}
		return "operation:" + strings.ToLower(parts[1]) + "/" + strings.ToLower(parts[2])
	default:
		return strings.ToLower(name)
	}
}

func apimServiceNameFromID(id string) string {
	parts := strings.Split(id, "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "service") && i > 0 && strings.EqualFold(parts[i-1], "Microsoft.ApiManagement") {
			return strings.ToLower(parts[i+1])
		}
	}
	return ""
}

func strProp(m map[string]any, key string) string { return strAny(m[key]) }

func strAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func boolProp(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func nestedBoolValue(m map[string]any, key string) bool {
	if mm, ok := m[key].(map[string]any); ok {
		if enabled, ok := mm["enabled"].(bool); ok {
			return enabled
		}
	}
	return false
}

func nestedString(m map[string]any, key string) (string, bool) {
	if len(m) == 0 {
		return "", false
	}
	s, ok := m[key].(string)
	return strings.TrimSpace(s), ok
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

func nestedMapKeys(m map[string]any, key string) map[string]bool {
	out := map[string]bool{}
	mm, ok := m[key].(map[string]any)
	if !ok {
		return out
	}
	for k := range mm {
		out[k] = true
	}
	return out
}

func dedupePolicyElements(in []PolicyElement) []PolicyElement {
	seen := map[string]bool{}
	out := make([]PolicyElement, 0, len(in))
	for _, el := range in {
		key := el.Name + "\x00" + el.Text
		if len(el.Attrs) > 0 {
			key += "\x00" + el.Attrs["name"] + "\x00" + el.Attrs["backend-id"] + "\x00" + el.Attrs["base-url"] + "\x00" + el.Attrs["resource"] + "\x00" + el.Attrs["client-id"]
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, el)
	}
	return out
}
