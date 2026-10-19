// Package gateway contains ARM-based normalisers for API Management gateway
// validation in Phase 8.
//
// Effective inbound policy is resolved per operation and per product scope
// (global -> product -> API -> operation), honouring <base/> at every scope as
// described in Learn set-edit-policies (read 2026-10-18). Policy text that
// does not parse as XML, external policy links and include-fragment references
// whose fragment is not in the template make a variant unresolved; callers
// must report it as uncertain, never as "policy absent".
package gateway

import (
	"encoding/xml"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// KnownAPIVersionList names the API Management resource versions the GW
// catalogue entries were verified against.
const KnownAPIVersionList = "2024-05-01, 2025-09-01-preview"

// maxFragmentDepth bounds nested include-fragment expansion.
const maxFragmentDepth = 4

// KnownAPIVersion reports whether v is an API Management resource API version
// listed in the FND-GW-* catalogue entries.
func KnownAPIVersion(v string) bool {
	switch strings.TrimSpace(v) {
	case "2024-05-01", "2025-09-01-preview":
		return true
	default:
		return false
	}
}

type Snapshot struct {
	Services        map[string]*Service
	UserAssigned    map[string]ManagedIdentity
	FoundryAccounts map[string]FoundryAccount
	AppInsights     map[string]AppInsightsComponent
	RoleAssignments []RoleAssignment
}

type Service struct {
	Name               string
	APIVersion         string
	Declared           bool
	SKU                string
	IdentityType       string
	PrincipalID        string
	UserAssignedIDs    []string
	SourceLocation     sdk.Location
	Policies           map[string]PolicyDocument
	Fragments          map[string]PolicyDocument
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
	APIVersion         string
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

// Diagnostic is an API Management diagnostic. LargeLanguageModel is true when
// properties.largeLanguageModel.logs is "enabled" (LLMDiagnosticSettings,
// Microsoft.ApiManagement/service/diagnostics 2025-09-01-preview template
// reference, read 2026-10-18).
type Diagnostic struct {
	Scope              string
	APIVersion         string
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

// ManagedIdentity is a user-assigned identity in the template. ClientID and
// PrincipalID are literal only when the template states them, which authored
// templates normally do not: they are runtime values.
type ManagedIdentity struct {
	Name        string
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
	APIVersion     string
	ExternalLink   bool
	Value          string
	SourceLocation sdk.Location
}

// EffectivePolicy is the inbound policy of one operation of an API reached
// through one product scope (Product "" is the path without a product-scope
// policy; Operation "" is the API scope when the template declares no
// operation). Unresolved lists why the inbound policy could not be normalised;
// External is true whenever Unresolved is not empty.
type EffectivePolicy struct {
	Operation  string
	Product    string
	External   bool
	Unresolved []string
	Inbound    []PolicyElement
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
			svc := ensureService(out.Services, r.Name)
			svc.Name = r.Name
			svc.APIVersion = r.APIVersion
			svc.Declared = true
			svc.SKU = r.SKUName
			svc.SourceLocation = r.Location
			if typ, ok := nestedString(r.Identity, "type"); ok {
				svc.IdentityType = typ
			}
			if pid, ok := nestedString(r.Identity, "principalId"); ok {
				svc.PrincipalID = pid
			}
			keys := nestedMapKeys(r.Identity, "userAssignedIdentities")
			svc.UserAssignedIDs = svc.UserAssignedIDs[:0]
			for key := range keys {
				svc.UserAssignedIDs = append(svc.UserAssignedIDs, key)
			}
			slices.Sort(svc.UserAssignedIDs)
		case "microsoft.apimanagement/service/apis":
			svc, apiName := serviceChild(r.Name)
			if svc == "" || apiName == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			api := &API{
				Name:           apiName,
				APIVersion:     r.APIVersion,
				BaseName:       baseAPIName(apiName),
				Path:           strProp(r.Properties, "path"),
				ServiceURL:     strProp(r.Properties, "serviceUrl"),
				VersionSetID:   strProp(r.Properties, "apiVersionSetId"),
				SourceLocation: r.Location,
			}
			if prev := s.APIs[strings.ToLower(apiName)]; prev != nil {
				api.HasOperationPolicy = prev.HasOperationPolicy
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
				slices.Sort(b.HeaderNames)
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
			llm, _ := nestedMap(r.Properties, "largeLanguageModel")
			s.Diagnostics = append(s.Diagnostics, Diagnostic{
				Scope:              scope,
				APIVersion:         r.APIVersion,
				LoggerID:           strProp(r.Properties, "loggerId"),
				Metrics:            boolProp(r.Properties, "metrics"),
				LargeLanguageModel: strings.EqualFold(strAny(llm["logs"]), "enabled"),
				SourceLocation:     r.Location,
			})
		case "microsoft.apimanagement/service/policies", "microsoft.apimanagement/service/apis/policies", "microsoft.apimanagement/service/products/policies", "microsoft.apimanagement/service/apis/operations/policies":
			scope := policyScope(r.Type, r.Name)
			svc, _ := serviceChild(r.Name)
			if svc == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.Policies[scope] = policyDocument(in.ARM, scope, r)
			if strings.Count(strings.Trim(r.Name, "/"), "/") >= 3 && strings.HasSuffix(strings.ToLower(r.Type), "/operations/policies") {
				apiName := operationAPIName(r.Name)
				if apiName != "" {
					if api := s.APIs[strings.ToLower(apiName)]; api != nil {
						api.HasOperationPolicy = true
					}
				}
			}
		case "microsoft.apimanagement/service/policyfragments":
			svc, fragment := serviceChild(r.Name)
			if svc == "" || fragment == "" {
				continue
			}
			s := ensureService(out.Services, svc)
			s.Fragments[strings.ToLower(fragment)] = policyDocument(in.ARM, "fragment:"+strings.ToLower(fragment), r)
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
				Name:        r.Name,
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
	for _, s := range out.Services {
		for key, products := range s.ProductAPIs {
			slices.Sort(products)
			s.ProductAPIs[key] = slices.Compact(products)
		}
	}
	return out
}

func policyDocument(model sdk.ARMModel, scope string, r sdk.ARMResource) PolicyDocument {
	format := strProp(r.Properties, "format")
	return PolicyDocument{
		Scope:          scope,
		Format:         format,
		APIVersion:     r.APIVersion,
		ExternalLink:   strings.Contains(strings.ToLower(format), "link"),
		Value:          resolvePolicyValue(model, strProp(r.Properties, "value")),
		SourceLocation: r.Location,
	}
}

// variablesOnlyRe matches a value that is exactly one variables() call, the
// form Bicep compiles loadTextContent('policy.xml') to: "[variables('$fxv#0')]".
var variablesOnlyRe = regexp.MustCompile(`^\[\s*variables\(\s*'([^']+)'\s*\)\s*\]$`)

// resolvePolicyValue returns the policy XML for a value that reads one compiled
// template variable, so a policy loaded from a file is analysed instead of
// reported as unparseable. A variable that is missing, is not a string or is
// still an expression leaves the value as it is, and the rules then treat the
// policy as unresolved rather than as a pass.
func resolvePolicyValue(model sdk.ARMModel, value string) string {
	m := variablesOnlyRe.FindStringSubmatch(strings.TrimSpace(value))
	if m == nil {
		return value
	}
	lookup, ok := model.(interface{ Variable(string) (any, bool) })
	if !ok {
		return value
	}
	v, found := lookup.Variable(m[1])
	lit, isStr := v.(string)
	if !found || !isStr || strings.HasPrefix(strings.TrimSpace(lit), "[") {
		return value
	}
	return lit
}

func ensureService(m map[string]*Service, name string) *Service {
	key := strings.ToLower(name)
	if s, ok := m[key]; ok {
		return s
	}
	s := &Service{
		Name:        name,
		Policies:    map[string]PolicyDocument{},
		Fragments:   map[string]PolicyDocument{},
		APIs:        map[string]*API{},
		Operations:  map[string]Operation{},
		VersionSets: map[string]VersionSet{},
		Backends:    map[string]Backend{},
		Loggers:     map[string]Logger{},
		ProductAPIs: map[string][]string{},
	}
	m[key] = s
	return s
}

// ParsedPolicy is the inbound section of one policy document.
type ParsedPolicy struct {
	HasInbound bool
	Inbound    []PolicyElement
}

// ParsePolicy parses a policy document strictly. APIM accepts policy text
// that is not well-formed XML (policy expressions with quotes, < or &&); such
// text returns an error and must be treated as unresolved, not as empty.
func ParsePolicy(value string) (ParsedPolicy, error) {
	var root xmlPolicy
	if err := xml.Unmarshal([]byte(value), &root); err != nil {
		return ParsedPolicy{}, err
	}
	for _, child := range root.Children {
		if child.XMLName.Local == "inbound" {
			out := ParsedPolicy{HasInbound: true}
			for _, c := range child.Children {
				out.Inbound = append(out.Inbound, c.toPolicyElement())
			}
			return out, nil
		}
	}
	return ParsedPolicy{}, nil
}

func parseFragment(value string) ([]PolicyElement, error) {
	var root xmlPolicy
	if err := xml.Unmarshal([]byte(value), &root); err != nil {
		return nil, err
	}
	var out []PolicyElement
	for _, c := range root.Children {
		out = append(out, c.toPolicyElement())
	}
	return out, nil
}

type scopeInbound struct {
	present    bool
	elements   []PolicyElement
	unresolved []string
}

func (s *Service) scopeInbound(scope string) scopeInbound {
	doc, ok := s.Policies[scope]
	if !ok {
		return scopeInbound{}
	}
	out := scopeInbound{present: true}
	switch {
	case doc.ExternalLink:
		out.unresolved = append(out.unresolved, "policy at scope "+scope+" is an external link (format "+doc.Format+")")
		return out
	case doc.Value == "":
		out.unresolved = append(out.unresolved, "policy at scope "+scope+" has no literal value")
		return out
	}
	parsed, err := ParsePolicy(doc.Value)
	if err != nil {
		out.unresolved = append(out.unresolved, "policy at scope "+scope+" does not parse as XML (format "+orNone(doc.Format)+")")
		return out
	}
	if !parsed.HasInbound {
		out.unresolved = append(out.unresolved, "policy at scope "+scope+" has no inbound section")
		return out
	}
	out.elements, out.unresolved = s.expandFragments(parsed.Inbound, 0)
	return out
}

// expandFragments replaces include-fragment with the fragment's elements when
// the fragment is defined in the template; anything else is unresolved.
func (s *Service) expandFragments(nodes []PolicyElement, depth int) ([]PolicyElement, []string) {
	var out []PolicyElement
	var issues []string
	for _, n := range nodes {
		if n.Name != "include-fragment" {
			if len(n.Children) > 0 {
				kids, more := s.expandFragments(n.Children, depth)
				n.Children = kids
				issues = append(issues, more...)
			}
			out = append(out, n)
			continue
		}
		id := strings.TrimSpace(n.Attrs["fragment-id"])
		frag, ok := s.Fragments[strings.ToLower(id)]
		switch {
		case depth >= maxFragmentDepth:
			issues = append(issues, "policy fragment "+id+" is nested too deeply")
		case !ok:
			issues = append(issues, "policy fragment "+id+" is not defined in the template")
		case frag.Value == "":
			issues = append(issues, "policy fragment "+id+" has no literal value")
		default:
			els, err := parseFragment(frag.Value)
			if err != nil {
				issues = append(issues, "policy fragment "+id+" does not parse as XML")
				continue
			}
			expanded, more := s.expandFragments(els, depth+1)
			out = append(out, expanded...)
			issues = append(issues, more...)
		}
	}
	return out, issues
}

// operationsOf returns the operations of api declared in the template or
// carrying an operation-scope policy, sorted; [""] when there are none.
func (s *Service) operationsOf(api string) []string {
	seen := map[string]bool{}
	var ops []string
	add := func(op string) {
		if op != "" && !seen[op] {
			seen[op] = true
			ops = append(ops, op)
		}
	}
	for key := range s.Operations {
		if a, op, ok := strings.Cut(key, "/"); ok && a == api {
			add(op)
		}
	}
	for scope := range s.Policies {
		if rest, ok := strings.CutPrefix(scope, "operation:"+api+"/"); ok {
			add(rest)
		}
	}
	if len(ops) == 0 {
		return []string{""}
	}
	slices.Sort(ops)
	return ops
}

// policyScopesFor lists the policy documents that can apply to api.
func (s *Service) policyScopesFor(api *API) []string {
	key := strings.ToLower(api.Name)
	var out []string
	for scope := range s.Policies {
		switch {
		case scope == "service", scope == "api:"+key, strings.HasPrefix(scope, "operation:"+key+"/"):
			out = append(out, scope)
		case strings.HasPrefix(scope, "product:") && slices.Contains(s.ProductAPIs[key], strings.TrimPrefix(scope, "product:")):
			out = append(out, scope)
		}
	}
	slices.Sort(out)
	return out
}

// HasPolicyDocument reports whether any policy document in the template can
// apply to api (service, product, API or operation scope).
func (s *Service) HasPolicyDocument(api *API) bool {
	return api != nil && (len(s.policyScopesFor(api)) > 0 || api.HasOperationPolicy)
}

// UnverifiedVersions lists the APIM resources behind api (the service when
// it is in the template, the API and every policy document that applies to
// it) whose apiVersion is outside KnownAPIVersion.
func (s *Service) UnverifiedVersions(api *API) []string {
	var out []string
	check := func(what, v string) {
		if !KnownAPIVersion(v) {
			out = append(out, what+" uses api-version "+orNone(v))
		}
	}
	if s.Declared {
		check("service "+s.Name, s.APIVersion)
	}
	check("API "+api.Name, api.APIVersion)
	for _, scope := range s.policyScopesFor(api) {
		check("policy at scope "+scope, s.Policies[scope].APIVersion)
	}
	return out
}

// EffectivePoliciesForAPI resolves the inbound policy of every operation of
// api through every product the API belongs to, plus the path without a
// product-scope policy. A scope document replaces what it inherits unless its
// inbound section contains <base/>.
func EffectivePoliciesForAPI(svc *Service, api *API) []EffectivePolicy {
	if svc == nil || api == nil {
		return nil
	}
	key := strings.ToLower(api.Name)
	global := svc.scopeInbound("service")
	apiScope := svc.scopeInbound("api:" + key)
	products := append([]string{""}, svc.ProductAPIs[key]...)
	var out []EffectivePolicy
	for _, op := range svc.operationsOf(key) {
		var opScope scopeInbound
		if op != "" {
			opScope = svc.scopeInbound("operation:" + key + "/" + op)
		}
		for _, product := range products {
			var productScope scopeInbound
			if product != "" {
				productScope = svc.scopeInbound("product:" + product)
			}
			ep := EffectivePolicy{Operation: op, Product: product}
			var cur []PolicyElement
			for _, sc := range []scopeInbound{global, productScope, apiScope, opScope} {
				if !sc.present {
					continue
				}
				ep.Unresolved = append(ep.Unresolved, sc.unresolved...)
				cur = expandBase(sc.elements, cur)
			}
			ep.External = len(ep.Unresolved) > 0
			ep.Inbound = cur
			out = append(out, ep)
		}
	}
	return out
}

// EffectivePolicyForAPI is the API-level view used by the cost and
// reliability rules: the de-duplicated union of every operation and product
// variant. External is true when any variant is unresolved. ok is false when
// no policy document applies to the API.
func EffectivePolicyForAPI(svc *Service, api *API) (EffectivePolicy, bool) {
	if svc == nil || api == nil {
		return EffectivePolicy{}, false
	}
	if len(svc.policyScopesFor(api)) == 0 && !api.HasOperationPolicy {
		return EffectivePolicy{}, false
	}
	var merged EffectivePolicy
	seen := map[string]bool{}
	for _, v := range EffectivePoliciesForAPI(svc, api) {
		if v.External {
			merged.External = true
			merged.Unresolved = append(merged.Unresolved, v.Unresolved...)
		}
		for _, el := range v.Inbound {
			k := elementKey(el)
			if seen[k] {
				continue
			}
			seen[k] = true
			merged.Inbound = append(merged.Inbound, el)
		}
	}
	return merged, true
}

func elementKey(el PolicyElement) string {
	keys := make([]string, 0, len(el.Attrs))
	for k := range el.Attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(el.Name)
	b.WriteString("\x00")
	b.WriteString(el.Text)
	for _, k := range keys {
		b.WriteString("\x00" + k + "=" + el.Attrs[k])
	}
	for _, c := range el.Children {
		b.WriteString("\x01" + elementKey(c))
	}
	return b.String()
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

// expandBase applies one scope's inbound elements over what it inherits:
// <base/> inserts the inherited elements; without <base/> nothing is
// inherited.
func expandBase(child, parent []PolicyElement) []PolicyElement {
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

// TopLevel returns the elements named name directly in nodes (after <base/>
// and fragment expansion), not inside a conditional such as <choose>.
func TopLevel(nodes []PolicyElement, name string) []PolicyElement {
	var out []PolicyElement
	for _, n := range nodes {
		if n.Name == name {
			out = append(out, n)
		}
	}
	return out
}

// Conditional reports whether an element named name exists only inside a
// <choose> branch of nodes.
func Conditional(nodes []PolicyElement, name string) bool {
	for _, n := range nodes {
		if n.Name == "choose" && len(FindElements(n.Children, name)) > 0 {
			return true
		}
	}
	return false
}

// AIAPIBacked reports whether api fronts a model endpoint: its serviceUrl, or
// a set-backend-service in any resolved policy variant, points at an Azure
// OpenAI, Foundry or Cognitive Services host.
func (s *Service) AIAPIBacked(api *API) bool {
	if api == nil {
		return false
	}
	if APIMHostLikely(HostFromURL(api.ServiceURL)) {
		return true
	}
	for _, v := range EffectivePoliciesForAPI(s, api) {
		for _, el := range FindElements(v.Inbound, "set-backend-service") {
			if APIMHostLikely(HostFromURL(el.Attrs["base-url"])) {
				return true
			}
			backendID := strings.ToLower(strings.TrimSpace(el.Attrs["backend-id"]))
			if backendID != "" && backendAIBacked(s, backendID, map[string]bool{}) {
				return true
			}
		}
	}
	return false
}

// AIBackingUnknown reports whether api's backing cannot be decided because a
// policy variant is unresolved and the serviceUrl is not a model host.
func (s *Service) AIBackingUnknown(api *API) bool {
	if api == nil || s.AIAPIBacked(api) {
		return false
	}
	for _, v := range EffectivePoliciesForAPI(s, api) {
		if v.External {
			return true
		}
	}
	return false
}

func orNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "none"
	}
	return v
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
