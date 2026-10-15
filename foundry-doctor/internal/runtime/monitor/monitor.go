// Package monitor contains the bounded Azure Monitor and Log Analytics runtime
// probes.
package monitor

import (
	"context"
	"regexp"
	"strconv"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// MetricPoint is the count-only projection used by runtime rules.
type MetricPoint struct {
	Series string
	Total  float64
}

// DiagnosticSetting is the metadata-only shape used by RUN-007.
type DiagnosticSetting struct {
	Name        string
	WorkspaceID string
	Categories  []string
	AgeHours    int
}

// LogsResult is the aggregate-only projection of a workspace query.
type LogsResult struct {
	Table  string
	Counts map[string]int64
}

// Client exposes the fixed monitor operations used by the rules.
type Client interface {
	QueryTraffic(context.Context, string) ([]MetricPoint, error)
	Query429(context.Context, string) ([]MetricPoint, error)
	QueryProvisionedUtilization(context.Context, string) ([]MetricPoint, error)
	ListDiagnosticSettings(context.Context, string) ([]DiagnosticSetting, error)
	QueryDiagnosticCounts(context.Context, string, string, int) (LogsResult, error)
}

// FixedClient adapts azure.RuntimeMonitor to the monitor Client contract.
type FixedClient struct{ Inner azure.RuntimeMonitor }

func (c FixedClient) QueryTraffic(ctx context.Context, accountID string) ([]MetricPoint, error) {
	got, err := c.Inner.QueryAccountMetrics(ctx, accountID, "AzureOpenAIRequests", "", "", "")
	if err != nil {
		return nil, err
	}
	return metricPoints(got), nil
}

func (c FixedClient) Query429(ctx context.Context, accountID string) ([]MetricPoint, error) {
	got, err := c.Inner.QueryAccountMetrics(ctx, accountID, "AzureOpenAIRequests", "StatusCode", "429", "ModelDeploymentName")
	if err != nil {
		return nil, err
	}
	return metricPoints(got), nil
}

func (c FixedClient) QueryProvisionedUtilization(ctx context.Context, accountID string) ([]MetricPoint, error) {
	got, err := c.Inner.QueryAccountMetrics(ctx, accountID, "AzureOpenAIProvisionedManagedUtilizationV2", "", "", "ModelDeploymentName")
	if err != nil {
		return nil, err
	}
	return metricPoints(got), nil
}

func (c FixedClient) ListDiagnosticSettings(ctx context.Context, resourceID string) ([]DiagnosticSetting, error) {
	got, err := c.Inner.ListDiagnosticSettings(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	out := make([]DiagnosticSetting, 0, len(got))
	for _, s := range got {
		out = append(out, DiagnosticSetting{
			Name:        s.Name,
			WorkspaceID: s.WorkspaceID,
			Categories:  append([]string(nil), s.Categories...),
			AgeHours:    s.AgeHours,
		})
	}
	return out, nil
}

func (c FixedClient) QueryDiagnosticCounts(ctx context.Context, workspaceID, accountName string, hours int) (LogsResult, error) {
	got, err := c.Inner.QueryDiagnosticCounts(ctx, workspaceID, accountName, hours)
	if err != nil {
		return LogsResult{}, err
	}
	out := LogsResult{Table: got.Table, Counts: map[string]int64{}}
	for k, v := range got.Counts {
		out.Counts[k] = v
	}
	return out, nil
}

func metricPoints(in []azure.MetricTotal) []MetricPoint {
	out := make([]MetricPoint, 0, len(in))
	for _, p := range in {
		out = append(out, MetricPoint{Series: p.Series, Total: p.Total})
	}
	return out
}

var accountNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// BuildQuery returns the fixed aggregate-only query template for RUN-007.
func BuildQuery(accountName string, hours int) (string, bool) {
	if !accountNameRE.MatchString(accountName) || hours <= 0 {
		return "", false
	}
	return "AzureDiagnostics | where ResourceProvider == \"MICROSOFT.COGNITIVESERVICES\" | where ResourceId has \"" + accountName + "\" | where TimeGenerated > ago(" + strconv.Itoa(hours) + "h) | summarize count() by Category", true
}
