package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
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
	var resp struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Identity struct {
			Type        string `json:"type"`
			PrincipalID string `json:"principalId"`
		} `json:"identity"`
		Properties struct {
			ProvisioningState string `json:"provisioningState"`
		} `json:"properties"`
	}
	path := subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account+"/projects/"+project)
	if err := a.c.get(ctx, path, nil, &resp); err != nil {
		return FoundryProject{}, err
	}
	return FoundryProject{
		ID: resp.ID, Name: resp.Name, AccountID: subPath(subscriptionID, "/resourceGroups/"+resourceGroup+"/providers/Microsoft.CognitiveServices/accounts/"+account),
		PrincipalID: resp.Identity.PrincipalID, IdentityType: resp.Identity.Type, ProvisioningState: resp.Properties.ProvisioningState,
	}, nil
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
			return nil, fmt.Errorf("azure: decode capability host: %w", err)
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
		var c struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Name       string `json:"name"`
			Properties struct {
				Category string         `json:"category"`
				AuthType string         `json:"authType"`
				Error    string         `json:"error"`
				Target   string         `json:"target"`
				Metadata map[string]any `json:"metadata"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("azure: decode connection: %w", err)
		}
		target := ConnectionTarget{Endpoint: c.Properties.Target}
		if rid, ok := c.Properties.Metadata["ResourceId"].(string); ok && strings.TrimSpace(rid) != "" {
			target.ResourceID = rid
		} else if rid, ok := c.Properties.Metadata["resourceId"].(string); ok && strings.TrimSpace(rid) != "" {
			target.ResourceID = rid
		}
		target.IndexNames = extractStrings(c.Properties.Metadata, "index", "indexName", "indexes")
		target.IndexerNames = extractStrings(c.Properties.Metadata, "indexer", "indexerName", "indexers")
		out = append(out, FoundryConnection{
			ID: c.ID, Type: c.Type, Name: c.Name, Category: c.Properties.Category, AuthType: c.Properties.AuthType,
			Error: c.Properties.Error, Target: target,
		})
	}
	return out, nil
}

func extractStrings(m map[string]any, keys ...string) []string {
	if len(m) == 0 {
		return nil
	}
	var out []string
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		switch vv := v.(type) {
		case string:
			if strings.TrimSpace(vv) != "" {
				out = append(out, vv)
			}
		case []any:
			for _, x := range vv {
				if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, s)
				}
			}
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
				Capacity int `json:"capacity"`
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
			return nil, fmt.Errorf("azure: decode deployment: %w", err)
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
			ID: d.ID, Type: d.Type, Name: d.Name, DeploymentState: d.Properties.DeploymentState, ProvisioningState: d.Properties.ProvisioningState,
			DynamicThrottling: d.Properties.DynamicThrottling, CurrentCapacity: capacity, ProvisionedRateLimit: limit,
		})
	}
	return out, nil
}

// RuntimeRBAC ---------------------------------------------------------------

func (a *Adapter) ListRoleAssignments(ctx context.Context, scope, principalID string) ([]RoleAssignment, error) {
	if strings.TrimSpace(scope) == "" || !subIDRe.MatchString(principalID) {
		return nil, fmt.Errorf("%w: role assignment query", ErrInvalidInput)
	}
	q := url.Values{"$filter": {"principalId eq " + principalID}}
	items, _, err := a.c.list(ctx, strings.TrimRight(scope, "/")+"/providers/Microsoft.Authorization/roleAssignments", q, 0)
	if err != nil {
		return nil, err
	}
	out := make([]RoleAssignment, 0, len(items))
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
			return nil, fmt.Errorf("azure: decode role assignment: %w", err)
		}
		out = append(out, RoleAssignment{
			RoleDefinitionID: r.Properties.RoleDefinitionID,
			PrincipalID:      r.Properties.PrincipalID,
			Scope:            r.Properties.Scope,
			Condition:        r.Properties.Condition,
		})
	}
	return out, nil
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
		var r struct {
			Properties struct {
				RoleDefinitionID string `json:"roleDefinitionId"`
				PrincipalID      string `json:"principalId"`
				Scope            string `json:"scope"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("azure: decode cosmos sql role assignment: %w", err)
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

// RuntimeDNS ----------------------------------------------------------------

func (a *Adapter) ListPrivateDNSVNetLinks(ctx context.Context, zoneID string) ([]PrivateDNSVNetLink, error) {
	if !strings.Contains(strings.ToLower(zoneID), "/providers/microsoft.network/privatednszones/") {
		return nil, fmt.Errorf("%w: private dns zone id", ErrInvalidInput)
	}
	items, _, err := a.c.list(ctx, strings.TrimRight(zoneID, "/")+"/virtualNetworkLinks", nil, 0)
	if err != nil {
		return nil, err
	}
	out := make([]PrivateDNSVNetLink, 0, len(items))
	for _, raw := range items {
		var l struct {
			Name       string `json:"name"`
			Properties struct {
				State string `json:"virtualNetworkLinkState"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, fmt.Errorf("azure: decode private dns link: %w", err)
		}
		out = append(out, PrivateDNSVNetLink{Name: l.Name, State: l.Properties.State})
	}
	return out, nil
}

// RuntimeMonitor ------------------------------------------------------------

func (a *Adapter) QueryAccountMetrics(ctx context.Context, accountID, metricName, filterDimension, filterValue, seriesDimension string) ([]MetricTotal, error) {
	if !strings.Contains(strings.ToLower(accountID), "/providers/microsoft.cognitiveservices/accounts/") {
		return nil, fmt.Errorf("%w: account id", ErrInvalidInput)
	}
	q := url.Values{
		"metricnames": {metricName},
		"aggregation": {"Total"},
		"timespan":    {"PT1H"},
		"interval":    {"PT1H"},
	}
	if filterDimension != "" && filterValue != "" {
		q.Set("$filter", filterDimension+" eq '"+filterValue+"'")
	}
	var resp struct {
		Value []struct {
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
			Timeseries []struct {
				Metadatavalues []struct {
					Name struct {
						Value string `json:"value"`
					} `json:"name"`
					Value string `json:"value"`
				} `json:"metadatavalues"`
				Data []struct {
					Total *float64 `json:"total"`
				} `json:"data"`
			} `json:"timeseries"`
		} `json:"value"`
	}
	if err := a.c.get(ctx, strings.TrimRight(accountID, "/")+"/providers/Microsoft.Insights/metrics", q, &resp); err != nil {
		return nil, err
	}
	out := []MetricTotal{}
	for _, m := range resp.Value {
		for _, ts := range m.Timeseries {
			label := metricName
			for _, md := range ts.Metadatavalues {
				if seriesDimension == "" || strings.EqualFold(md.Name.Value, seriesDimension) {
					label = md.Value
				}
			}
			var total float64
			for _, d := range ts.Data {
				if d.Total != nil {
					total += *d.Total
				}
			}
			out = append(out, MetricTotal{Series: label, Total: total})
		}
	}
	return out, nil
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
		var s struct {
			Name       string `json:"name"`
			SystemData struct {
				CreatedAt      string `json:"createdAt"`
				LastModifiedAt string `json:"lastModifiedAt"`
			} `json:"systemData"`
			Properties struct {
				WorkspaceID string `json:"workspaceId"`
				Logs        []struct {
					Category string `json:"category"`
					Enabled  bool   `json:"enabled"`
				} `json:"logs"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("azure: decode diagnostic setting: %w", err)
		}
		cats := []string{}
		for _, l := range s.Properties.Logs {
			if l.Enabled {
				cats = append(cats, l.Category)
			}
		}
		out = append(out, RuntimeDiagnosticSetting{Name: s.Name, WorkspaceID: s.Properties.WorkspaceID, Categories: sortedUnique(cats), AgeHours: ageHours(s.SystemData)})
	}
	return out, nil
}

func ageHours(systemData struct {
	CreatedAt      string `json:"createdAt"`
	LastModifiedAt string `json:"lastModifiedAt"`
}) int {
	for _, raw := range []string{systemData.LastModifiedAt, systemData.CreatedAt} {
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return int(time.Since(ts).Hours())
		}
	}
	return 0
}

var safeAccountName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func (a *Adapter) QueryDiagnosticCounts(ctx context.Context, workspaceID, accountName string, hours int) (RuntimeLogsResult, error) {
	if !safeAccountName.MatchString(accountName) || strings.TrimSpace(workspaceID) == "" || hours <= 0 {
		return RuntimeLogsResult{}, fmt.Errorf("%w: workspace query", ErrInvalidInput)
	}
	type queryReq struct {
		Query string `json:"query"`
	}
	body := queryReq{Query: "AzureDiagnostics | where ResourceProvider == \"MICROSOFT.COGNITIVESERVICES\" | where ResourceId has \"" + accountName + "\" | where TimeGenerated > ago(" + fmt.Sprintf("%dh", hours) + ") | summarize count() by Category"}
	var resp struct {
		Tables []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"tables"`
	}
	if err := a.logQuery(ctx, workspaceID, body, &resp); err != nil {
		return RuntimeLogsResult{}, err
	}
	out := RuntimeLogsResult{Counts: map[string]int64{}}
	if len(resp.Tables) == 0 {
		return out, nil
	}
	out.Table = resp.Tables[0].Name
	for _, row := range resp.Tables[0].Rows {
		if len(row) >= 2 {
			cat, _ := row[0].(string)
			switch n := row[1].(type) {
			case float64:
				out.Counts[cat] = int64(n)
			case int64:
				out.Counts[cat] = n
			}
		}
	}
	return out, nil
}

func (a *Adapter) logQuery(ctx context.Context, workspaceID string, body any, out any) error {
	scope := "https://api.loganalytics.io/.default"
	tok, err := a.c.cred.Token(ctx, scope)
	if err != nil {
		return &UnavailableError{Capability: "Log Analytics workspace query", Reason: "no usable Azure credential (run `azd auth login` or `az login`)", Err: err}
	}
	endpoint := "https://api.loganalytics.io/v1/workspaces/" + url.PathEscape(workspaceID) + "/query"
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return &UnavailableError{Capability: "Log Analytics workspace query", Reason: "workspace query permission unavailable"}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return &UnavailableError{Capability: "Log Analytics workspace query", Reason: "credential rejected by workspace"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
