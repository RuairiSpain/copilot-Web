package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RuntimeFoundry ------------------------------------------------------------

func (a *Adapter) GetProject(ctx context.Context, subscriptionID, resourceGroup, account, project string) (FoundryProject, error) {
	if err := requireSub(subscriptionID); err != nil {
		return FoundryProject{}, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) || !segOnlyRe.MatchString(project) {
		return FoundryProject{}, fmt.Errorf("%w: project coordinates", ErrInvalidInput)
	}
	// Shape: cognitiveservices.json 2026-09-01, Project (identity: Identity with
	// principalId and userAssignedIdentities map of UserAssignedIdentity).
	var resp struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Identity struct {
			Type                   string `json:"type"`
			PrincipalID            string `json:"principalId"`
			UserAssignedIdentities map[string]struct {
				PrincipalID string `json:"principalId"`
			} `json:"userAssignedIdentities"`
		} `json:"identity"`
		Properties struct {
			ProvisioningState string `json:"provisioningState"`
		} `json:"properties"`
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/projects/"+project)
	if err := a.c.get(ctx, path, nil, &resp); err != nil {
		return FoundryProject{}, err
	}
	var ua []string
	for _, v := range resp.Identity.UserAssignedIdentities {
		if subIDRe.MatchString(v.PrincipalID) {
			ua = append(ua, strings.ToLower(v.PrincipalID))
		}
	}
	principal := resp.Identity.PrincipalID
	if principal != "" && !subIDRe.MatchString(principal) {
		principal = ""
	}
	return FoundryProject{
		ID: resp.ID, Name: resp.Name, AccountID: subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account),
		PrincipalID: strings.ToLower(principal), UserAssignedPrincipalIDs: sortedUnique(ua),
		IdentityType: resp.Identity.Type, ProvisioningState: resp.Properties.ProvisioningState,
	}, nil
}

// GetAccountNetwork reads the account's public network access and private endpoint connection count.
func (a *Adapter) GetAccountNetwork(ctx context.Context, subscriptionID, resourceGroup, account string) (AccountNetwork, error) {
	if err := requireSub(subscriptionID); err != nil {
		return AccountNetwork{}, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) {
		return AccountNetwork{}, fmt.Errorf("%w: account coordinates", ErrInvalidInput)
	}
	var resp struct {
		Properties struct {
			PublicNetworkAccess        string            `json:"publicNetworkAccess"`
			PrivateEndpointConnections []json.RawMessage `json:"privateEndpointConnections"`
		} `json:"properties"`
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account)
	if err := a.c.get(ctx, path, nil, &resp); err != nil {
		return AccountNetwork{}, err
	}
	return AccountNetwork{PublicNetworkAccess: resp.Properties.PublicNetworkAccess, PrivateEndpointConnections: len(resp.Properties.PrivateEndpointConnections)}, nil
}

func (a *Adapter) ListProjectCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]CapabilityHost, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) || !segOnlyRe.MatchString(project) {
		return nil, fmt.Errorf("%w: project capability host coordinates", ErrInvalidInput)
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/projects/"+project+"/capabilityHosts")
	return a.listCapabilityHosts(ctx, path)
}

func (a *Adapter) ListAccountCapabilityHosts(ctx context.Context, subscriptionID, resourceGroup, account string) ([]CapabilityHost, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) {
		return nil, fmt.Errorf("%w: account capability host coordinates", ErrInvalidInput)
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/capabilityHosts")
	return a.listCapabilityHosts(ctx, path)
}

func (a *Adapter) listCapabilityHosts(ctx context.Context, path string) ([]CapabilityHost, error) {
	items, _, err := a.c.list(ctx, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]CapabilityHost, 0, len(items))
	for _, raw := range items {
		var h struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Name       string `json:"name"`
			Properties struct {
				ProvisioningState        string   `json:"provisioningState"`
				AIServiceConnections     []string `json:"aiServicesConnections"`
				StorageConnections       []string `json:"storageConnections"`
				ThreadStorageConnections []string `json:"threadStorageConnections"`
				VectorStoreConnections   []string `json:"vectorStoreConnections"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("%w: capability host: %v", ErrDecode, err)
		}
		out = append(out, CapabilityHost{
			ID: h.ID, Type: h.Type, Name: h.Name, ProvisioningState: h.Properties.ProvisioningState,
			AIServiceConnections:     sortedUnique(h.Properties.AIServiceConnections),
			StorageConnections:       sortedUnique(h.Properties.StorageConnections),
			ThreadStorageConnections: sortedUnique(h.Properties.ThreadStorageConnections),
			VectorStoreConnections:   sortedUnique(h.Properties.VectorStoreConnections),
		})
	}
	return out, nil
}

func (a *Adapter) ListProjectConnections(ctx context.Context, subscriptionID, resourceGroup, account, project string) ([]FoundryConnection, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) || !segOnlyRe.MatchString(project) {
		return nil, fmt.Errorf("%w: project connection coordinates", ErrInvalidInput)
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/projects/"+project+"/connections")
	return a.listFoundryConnections(ctx, path)
}

func (a *Adapter) ListAccountConnections(ctx context.Context, subscriptionID, resourceGroup, account string) ([]FoundryConnection, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) {
		return nil, fmt.Errorf("%w: account connection coordinates", ErrInvalidInput)
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/connections")
	return a.listFoundryConnections(ctx, path)
}

func (a *Adapter) listFoundryConnections(ctx context.Context, path string) ([]FoundryConnection, error) {
	items, _, err := a.c.list(ctx, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]FoundryConnection, 0, len(items))
	for _, raw := range items {
		// ConnectionPropertiesV2 (cognitiveservices.json 2026-09-01): authType, category,
		// error, metadata (string map), peRequirement, peStatus, target. Credentials are
		// never decoded.
		var c struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Name       string `json:"name"`
			Properties struct {
				Category      string         `json:"category"`
				AuthType      string         `json:"authType"`
				Error         string         `json:"error"`
				Target        string         `json:"target"`
				PERequirement string         `json:"peRequirement"`
				PEStatus      string         `json:"peStatus"`
				Metadata      map[string]any `json:"metadata"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("%w: connection: %v", ErrDecode, err)
		}
		target := ConnectionTarget{Endpoint: c.Properties.Target}
		if rid := metadataString(c.Properties.Metadata, "ResourceId"); rid != "" {
			target.ResourceID = rid
		} else if IsARMResourceID(c.Properties.Target) {
			target.ResourceID = strings.TrimSpace(c.Properties.Target)
		}
		target.IndexNames = extractStrings(c.Properties.Metadata, "index", "indexName", "indexes")
		target.IndexerNames = extractStrings(c.Properties.Metadata, "indexer", "indexerName", "indexers")
		out = append(out, FoundryConnection{
			ID: c.ID, Type: c.Type, Name: c.Name, Category: c.Properties.Category, AuthType: c.Properties.AuthType,
			Error: c.Properties.Error, PERequirement: c.Properties.PERequirement, PEStatus: c.Properties.PEStatus, Target: target,
		})
	}
	return out, nil
}

// IsARMResourceID reports whether s looks like a literal ARM resource or scope ID.
func IsARMResourceID(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "/subscriptions/")
}

func metadataString(m map[string]any, key string) string {
	for k, v := range m {
		if strings.EqualFold(k, key) {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func extractStrings(m map[string]any, keys ...string) []string {
	if len(m) == 0 {
		return nil
	}
	var out []string
	for _, k := range keys {
		if s := metadataString(m, k); s != "" {
			out = append(out, s)
		}
	}
	return sortedUnique(out)
}

func (a *Adapter) ListAccountDeployments(ctx context.Context, subscriptionID, resourceGroup, account string) ([]AccountDeployment, error) {
	if err := requireSub(subscriptionID); err != nil {
		return nil, err
	}
	if !segOnlyRe.MatchString(resourceGroup) || !segOnlyRe.MatchString(account) {
		return nil, fmt.Errorf("%w: account deployment coordinates", ErrInvalidInput)
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/deployments")
	items, _, err := a.c.list(ctx, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]AccountDeployment, 0, len(items))
	for _, raw := range items {
		var d struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Name string `json:"name"`
			SKU  struct {
				Name     string `json:"name"`
				Capacity int    `json:"capacity"`
			} `json:"sku"`
			Properties struct {
				DeploymentState   string `json:"deploymentState"`
				ProvisioningState string `json:"provisioningState"`
				DynamicThrottling bool   `json:"dynamicThrottlingEnabled"`
				CurrentCapacity   int    `json:"currentCapacity"`
				RateLimits        []struct {
					Count int `json:"count"`
				} `json:"rateLimits"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("%w: deployment: %v", ErrDecode, err)
		}
		limit := 0
		for _, rl := range d.Properties.RateLimits {
			limit += rl.Count
		}
		capacity := d.Properties.CurrentCapacity
		if capacity == 0 {
			capacity = d.SKU.Capacity
		}
		out = append(out, AccountDeployment{
			ID: d.ID, Type: d.Type, Name: d.Name, DeploymentState: d.Properties.DeploymentState, ProvisioningState: d.Properties.ProvisioningState, SKUName: d.SKU.Name,
			DynamicThrottling: d.Properties.DynamicThrottling, CurrentCapacity: capacity, ProvisionedRateLimit: limit,
		})
	}
	return out, nil
}

// RuntimeRBAC ---------------------------------------------------------------

// ListRoleAssignments lists the assignments of principalID at, above and below
// scope (subscription, resource group or resource ID) and splits them by their
// relation to scope. $filter=principalId eq {id} returns assignments at, above
// or below the scope (authorization-RoleAssignmentsCalls.json 2022-04-01), so
// the split is done here: only an assignment whose scope equals or is a
// path-boundary ancestor of scope grants access at scope.
func (a *Adapter) ListRoleAssignments(ctx context.Context, scope, principalID string) (RoleAssignmentSet, error) {
	scope = strings.TrimRight(strings.TrimSpace(scope), "/")
	if !IsARMResourceID(scope) || !subIDRe.MatchString(principalID) {
		return RoleAssignmentSet{}, fmt.Errorf("%w: role assignment query", ErrInvalidInput)
	}
	q := url.Values{"$filter": {"principalId eq " + principalID}}
	items, truncated, err := a.c.list(ctx, scope+"/providers/Microsoft.Authorization/roleAssignments", q, 0)
	if err != nil {
		return RoleAssignmentSet{}, err
	}
	set := RoleAssignmentSet{Truncated: truncated}
	for _, raw := range items {
		var r struct {
			Properties struct {
				RoleDefinitionID string `json:"roleDefinitionId"`
				PrincipalID      string `json:"principalId"`
				Scope            string `json:"scope"`
				Condition        string `json:"condition"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return RoleAssignmentSet{}, fmt.Errorf("%w: role assignment: %v", ErrDecode, err)
		}
		ra := RoleAssignment{
			RoleDefinitionID: r.Properties.RoleDefinitionID,
			PrincipalID:      r.Properties.PrincipalID,
			Scope:            r.Properties.Scope,
			Condition:        r.Properties.Condition,
		}
		switch {
		case ra.Scope == "":
			set.Unresolved = append(set.Unresolved, ra)
		case LockCovers(ra.Scope, scope):
			set.Covering = append(set.Covering, ra)
		case LockCovers(scope, ra.Scope):
			set.Below = append(set.Below, ra)
		case isHierarchyScope(ra.Scope):
			// Root or management group: an ancestor only if the hierarchy says so,
			// which this read cannot prove.
			set.Unresolved = append(set.Unresolved, ra)
		}
	}
	return set, nil
}

func isHierarchyScope(s string) bool {
	s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
	return s == "" || strings.HasPrefix(s, "/providers/microsoft.management/managementgroups/")
}

func (a *Adapter) ListCosmosSQLRoleAssignments(ctx context.Context, accountID, principalID string) ([]CosmosSQLRoleAssignment, error) {
	if !strings.Contains(strings.ToLower(accountID), "/providers/microsoft.documentdb/databaseaccounts/") || !subIDRe.MatchString(principalID) {
		return nil, fmt.Errorf("%w: cosmos sql role assignment query", ErrInvalidInput)
	}
	items, _, err := a.c.list(ctx, strings.TrimRight(accountID, "/")+"/sqlRoleAssignments", nil, 0)
	if err != nil {
		return nil, err
	}
	out := []CosmosSQLRoleAssignment{}
	for _, raw := range items {
		// SqlRoleAssignmentResource (cosmos rbac.json 2025-10-15): roleDefinitionId
		// (full ARM ID), scope (full ARM data-plane path), principalId.
		var r struct {
			Properties struct {
				RoleDefinitionID string `json:"roleDefinitionId"`
				PrincipalID      string `json:"principalId"`
				Scope            string `json:"scope"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%w: cosmos sql role assignment: %v", ErrDecode, err)
		}
		if strings.EqualFold(r.Properties.PrincipalID, principalID) {
			out = append(out, CosmosSQLRoleAssignment{
				RoleDefinitionID: r.Properties.RoleDefinitionID,
				PrincipalID:      r.Properties.PrincipalID,
				Scope:            r.Properties.Scope,
			})
		}
	}
	return out, nil
}

// RuntimeMonitor ------------------------------------------------------------

var (
	metricNameRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]{0,127}$`)
	dimensionNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
)

// metricsTop is the per-resource series cap sent as top. The spec says top
// defaults to 10 when $filter is set, so splitting by deployment needs more.
const metricsTop = 50

const defaultMetricWindow = time.Hour

// metricTimespan renders the documented "startDateTime_ISO/endDateTime_ISO" form.
func metricTimespan(end time.Time, window time.Duration) string {
	end = end.UTC().Truncate(time.Second)
	start := end.Add(-window)
	const layout = "2006-01-02T15:04:05Z"
	return start.Format(layout) + "/" + end.Format(layout)
}

// QueryAccountMetrics reads one metric of a Foundry (Cognitive Services)
// account, split by the requested dimensions, over an explicit timespan.
// Series are attributed through their metadatavalues; nothing is inferred
// from the metric name.
func (a *Adapter) QueryAccountMetrics(ctx context.Context, accountID string, mq MetricQuery) (MetricResult, error) {
	if !strings.Contains(strings.ToLower(accountID), "/providers/microsoft.cognitiveservices/accounts/") {
		return MetricResult{}, fmt.Errorf("%w: account id", ErrInvalidInput)
	}
	if !metricNameRe.MatchString(mq.MetricName) {
		return MetricResult{}, fmt.Errorf("%w: metric name", ErrInvalidInput)
	}
	switch mq.Aggregation {
	case "Total", "Maximum", "Average", "Minimum", "Count":
	default:
		return MetricResult{}, fmt.Errorf("%w: aggregation %q", ErrInvalidInput, mq.Aggregation)
	}
	window := mq.Window
	if window <= 0 {
		window = defaultMetricWindow
	}
	q := url.Values{
		"metricnames": {mq.MetricName},
		"aggregation": {mq.Aggregation},
		"timespan":    {metricTimespan(a.c.now(), window)},
		// PT1M is the only time grain the supported-metrics table lists for the
		// AzureOpenAI metrics used here.
		"interval": {"PT1M"},
	}
	if len(mq.SplitDimensions) > 0 {
		terms := make([]string, 0, len(mq.SplitDimensions))
		for _, d := range mq.SplitDimensions {
			if !dimensionNameRe.MatchString(d) {
				return MetricResult{}, fmt.Errorf("%w: dimension name", ErrInvalidInput)
			}
			terms = append(terms, d+" eq '*'")
		}
		q.Set("$filter", strings.Join(terms, " and "))
		q.Set("top", strconv.Itoa(metricsTop))
	}
	var resp struct {
		Value []struct {
			ErrorCode  string `json:"errorCode"`
			Timeseries []struct {
				Metadatavalues []struct {
					Name struct {
						Value string `json:"value"`
					} `json:"name"`
					Value string `json:"value"`
				} `json:"metadatavalues"`
				Data []struct {
					Total   *float64 `json:"total"`
					Maximum *float64 `json:"maximum"`
					Average *float64 `json:"average"`
					Minimum *float64 `json:"minimum"`
					Count   *float64 `json:"count"`
				} `json:"data"`
			} `json:"timeseries"`
		} `json:"value"`
	}
	if err := a.c.get(ctx, strings.TrimRight(accountID, "/")+"/providers/Microsoft.Insights/metrics", q, &resp); err != nil {
		return MetricResult{}, err
	}
	var out MetricResult
	for _, m := range resp.Value {
		if m.ErrorCode != "" && !strings.EqualFold(m.ErrorCode, "Success") && safeCode.MatchString(m.ErrorCode) {
			out.ErrorCode = m.ErrorCode
		}
		for _, ts := range m.Timeseries {
			s := MetricSeries{Dimensions: map[string]string{}}
			for _, md := range ts.Metadatavalues {
				s.Dimensions[strings.ToLower(md.Name.Value)] = md.Value
			}
			var sum float64
			for _, d := range ts.Data {
				var v *float64
				switch mq.Aggregation {
				case "Total":
					v = d.Total
				case "Maximum":
					v = d.Maximum
				case "Average":
					v = d.Average
				case "Minimum":
					v = d.Minimum
				case "Count":
					v = d.Count
				}
				if v == nil {
					continue
				}
				s.Points++
				switch mq.Aggregation {
				case "Maximum":
					if s.Points == 1 || *v > s.Value {
						s.Value = *v
					}
				case "Minimum":
					if s.Points == 1 || *v < s.Value {
						s.Value = *v
					}
				default:
					sum += *v
				}
			}
			switch mq.Aggregation {
			case "Average":
				if s.Points > 0 {
					s.Value = sum / float64(s.Points)
				}
			case "Total", "Count":
				s.Value = sum
			}
			out.Series = append(out.Series, s)
		}
	}
	if len(mq.SplitDimensions) > 0 && len(out.Series) >= metricsTop {
		out.Truncated = true
	}
	sort.SliceStable(out.Series, func(i, j int) bool {
		return seriesKey(out.Series[i]) < seriesKey(out.Series[j])
	})
	return out, nil
}

func seriesKey(s MetricSeries) string {
	keys := make([]string, 0, len(s.Dimensions))
	for k := range s.Dimensions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + s.Dimensions[k] + ";")
	}
	return b.String()
}

func (a *Adapter) ListDiagnosticSettings(ctx context.Context, resourceID string) ([]RuntimeDiagnosticSetting, error) {
	if !strings.Contains(strings.ToLower(resourceID), "/providers/microsoft.cognitiveservices/accounts/") {
		return nil, fmt.Errorf("%w: resource id", ErrInvalidInput)
	}
	items, _, err := a.c.list(ctx, strings.TrimRight(resourceID, "/")+"/providers/Microsoft.Insights/diagnosticSettings", nil, 0)
	if err != nil {
		return nil, err
	}
	out := []RuntimeDiagnosticSetting{}
	for _, raw := range items {
		// DiagnosticSettings (diagnosticsSettings_API.json 2021-05-01-preview): workspaceId is the
		// full ARM ID; logs[] has category, categoryGroup, enabled; logAnalyticsDestinationType
		// is "Dedicated" or null.
		var s struct {
			Name       string `json:"name"`
			SystemData struct {
				CreatedAt      string `json:"createdAt"`
				LastModifiedAt string `json:"lastModifiedAt"`
			} `json:"systemData"`
			Properties struct {
				WorkspaceID                 string `json:"workspaceId"`
				LogAnalyticsDestinationType string `json:"logAnalyticsDestinationType"`
				Logs                        []struct {
					Category      string `json:"category"`
					CategoryGroup string `json:"categoryGroup"`
					Enabled       bool   `json:"enabled"`
				} `json:"logs"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("%w: diagnostic setting: %v", ErrDecode, err)
		}
		cats, groups := []string{}, []string{}
		for _, l := range s.Properties.Logs {
			if !l.Enabled {
				continue
			}
			if l.Category != "" {
				cats = append(cats, l.Category)
			}
			if l.CategoryGroup != "" {
				groups = append(groups, l.CategoryGroup)
			}
		}
		minutes, known := a.ageMinutes(s.SystemData.LastModifiedAt, s.SystemData.CreatedAt)
		out = append(out, RuntimeDiagnosticSetting{
			Name: s.Name, WorkspaceID: s.Properties.WorkspaceID, Categories: sortedUnique(cats), CategoryGroups: sortedUnique(groups),
			LogAnalyticsDestinationType: s.Properties.LogAnalyticsDestinationType, AgeKnown: known, AgeMinutes: minutes,
		})
	}
	return out, nil
}

func (a *Adapter) ageMinutes(candidates ...string) (int, bool) {
	for _, raw := range candidates {
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return max(int(a.c.now().Sub(ts)/time.Minute), 0), true
		}
	}
	return 0, false
}

// Log Analytics -------------------------------------------------------------

var (
	workspaceIDRe = regexp.MustCompile(`(?i)^/subscriptions/(` + guidRe + `)/resourcegroups/(` + segRe + `)/providers/microsoft\.operationalinsights/workspaces/(` + segRe + `)$`)
	// logQueryRe is the only query text the client sends (ADR-006 item 7).
	logQueryRe = regexp.MustCompile(`^AzureDiagnostics \| where ResourceProvider == "MICROSOFT\.COGNITIVESERVICES" \| where ResourceId endswith "/ACCOUNTS/([A-Za-z0-9._-]{1,64})" \| where TimeGenerated > ago\([1-9][0-9]{0,2}h\) \| summarize count\(\) by Category$`)
	logSpanRe  = regexp.MustCompile(`^PT[1-9][0-9]{0,2}H$`)
	// kqlForbidden lists the tokens ADR-006 item 7 rejects (take, top, message, *)
	// and operators that could widen the projection or the data touched.
	kqlForbidden = regexp.MustCompile(`(?i)\b(take|top|limit|message|project|extend|union|join|search|print|invoke|evaluate|externaldata|find|parse|sample|distinct|mv-expand)\b|[*;]`)
)

// logQueryBody is the only request body the Log Analytics operation accepts.
type logQueryBody struct {
	Query    string `json:"query"`
	Timespan string `json:"timespan"`
}

// DiagnosticCountsKQL returns the shipped aggregate-only template for RUN-007
// with the account name as the only variable part. The name is validated
// against the ARM name charset first; KQL endswith is case-insensitive.
func DiagnosticCountsKQL(accountName string, hours int) (string, bool) {
	if !safeAccountName.MatchString(accountName) || hours <= 0 || hours > 999 {
		return "", false
	}
	return `AzureDiagnostics | where ResourceProvider == "MICROSOFT.COGNITIVESERVICES" | where ResourceId endswith "/ACCOUNTS/` +
		strings.ToUpper(accountName) + `" | where TimeGenerated > ago(` + strconv.Itoa(hours) + `h) | summarize count() by Category`, true
}

func validateLogQueryBody(body any) error {
	b, ok := body.(logQueryBody)
	if !ok {
		return fmt.Errorf("%w: log query body", ErrNotAllowed)
	}
	m := logQueryRe.FindStringSubmatch(b.Query)
	if m == nil {
		return fmt.Errorf("%w: log query is not the shipped template", ErrNotAllowed)
	}
	// Scan the template without the variable name (a name may legitimately contain "top").
	if kqlForbidden.MatchString(strings.Replace(b.Query, m[1], "", 1)) {
		return fmt.Errorf("%w: log query contains a forbidden operator", ErrNotAllowed)
	}
	if !logSpanRe.MatchString(b.Timespan) {
		return fmt.Errorf("%w: log query timespan", ErrNotAllowed)
	}
	return nil
}

var safeAccountName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// workspaceCustomerID resolves properties.customerId (the workspace GUID the
// query path needs) from the workspace ARM resource ID.
func (a *Adapter) workspaceCustomerID(ctx context.Context, workspaceResourceID string) (string, error) {
	m := workspaceIDRe.FindStringSubmatch(strings.TrimRight(strings.TrimSpace(workspaceResourceID), "/"))
	if m == nil {
		return "", fmt.Errorf("%w: workspace resource id", ErrInvalidInput)
	}
	path := subPath(m[1], "/resourceGroups/"+m[2]+"/providers/Microsoft.OperationalInsights/workspaces/"+m[3])
	var resp struct {
		Properties struct {
			CustomerID string `json:"customerId"`
		} `json:"properties"`
	}
	if err := a.c.get(ctx, path, nil, &resp); err != nil {
		return "", err
	}
	if !subIDRe.MatchString(resp.Properties.CustomerID) {
		return "", fmt.Errorf("%w: workspace has no customerId", ErrDecode)
	}
	return resp.Properties.CustomerID, nil
}

// QueryDiagnosticCounts counts the account's AzureDiagnostics rows per
// Category. Only aggregate counts are kept. The call goes through the
// allow-listed Log Analytics operation (declared origin, token scope, path and
// KQL template), after resolving the workspace GUID through an allow-listed
// workspace read.
func (a *Adapter) QueryDiagnosticCounts(ctx context.Context, workspaceResourceID, accountName string, hours int) (RuntimeLogsResult, error) {
	kql, ok := DiagnosticCountsKQL(accountName, hours)
	if !ok {
		return RuntimeLogsResult{}, fmt.Errorf("%w: workspace query", ErrInvalidInput)
	}
	guid, err := a.workspaceCustomerID(ctx, workspaceResourceID)
	if err != nil {
		return RuntimeLogsResult{}, err
	}
	var resp struct {
		Tables []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"tables"`
	}
	body := logQueryBody{Query: kql, Timespan: "PT" + strconv.Itoa(hours) + "H"}
	if err := a.c.post(ctx, "/v1/workspaces/"+guid+"/query", body, &resp); err != nil {
		return RuntimeLogsResult{}, err
	}
	out := RuntimeLogsResult{Counts: map[string]int64{}}
	// The result table is always PrimaryResult (response-format.md).
	for _, t := range resp.Tables {
		if t.Name != "PrimaryResult" {
			continue
		}
		for _, row := range t.Rows {
			if len(row) < 2 {
				continue
			}
			cat, _ := row[0].(string)
			switch n := row[1].(type) {
			case float64:
				out.Counts[cat] += int64(n)
			case json.Number:
				if v, err := n.Int64(); err == nil {
					out.Counts[cat] += v
				}
			}
		}
	}
	return out, nil
}
