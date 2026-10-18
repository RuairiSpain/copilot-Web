// Package monitor contains the bounded Azure Monitor and Log Analytics runtime
// probes. All reads go through the allow-listed azure.RuntimeMonitor adapter;
// this package has no HTTP client of its own.
package monitor

import (
	"context"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

// Metric and dimension names verified in MicrosoftDocs/azure-monitor-docs
// reference/supported-metrics/microsoft-cognitiveservices-accounts-metrics.md:
//   - AzureOpenAIRequests: aggregation Total; dimensions include ModelDeploymentName and StatusCode.
//   - AzureOpenAIProvisionedManagedUtilizationV2: aggregations Minimum, Maximum, Average;
//     dimension ModelDeploymentName.
//
// The StatusCode dimension VALUES are not enumerated by the docs (the
// description speaks of categories: successful, clienterrors, server errors),
// so callers must match values client-side and never filter on a literal.
const (
	MetricRequests      = "AzureOpenAIRequests"
	MetricUtilizationV2 = "AzureOpenAIProvisionedManagedUtilizationV2"
	DimDeployment       = "ModelDeploymentName"
	DimStatusCode       = "StatusCode"
)

// Series is one attributed time series. Dimensions are keyed by lower-cased
// dimension name.
type Series struct {
	Dimensions map[string]string
	Value      float64
	Points     int
}

// Result is one metrics read.
type Result struct {
	Series    []Series
	Truncated bool
	ErrorCode string
}

// DiagnosticSetting is the metadata-only shape used by RUN-007.
type DiagnosticSetting struct {
	Name                        string
	WorkspaceID                 string
	Categories                  []string
	CategoryGroups              []string
	LogAnalyticsDestinationType string
	AgeKnown                    bool
	AgeMinutes                  int
}

// LogsResult is the aggregate-only projection of a workspace query.
type LogsResult struct {
	Counts map[string]int64
}

// Client exposes the fixed monitor operations used by the rules.
type Client interface {
	// QueryTraffic returns the request total of the account (no split).
	QueryTraffic(context.Context, string) (Result, error)
	// QueryRequestsByStatus returns request totals split by deployment and StatusCode.
	QueryRequestsByStatus(context.Context, string) (Result, error)
	// QueryProvisionedUtilization returns the maximum utilisation split by deployment.
	QueryProvisionedUtilization(context.Context, string) (Result, error)
	ListDiagnosticSettings(context.Context, string) ([]DiagnosticSetting, error)
	QueryDiagnosticCounts(context.Context, string, string, int) (LogsResult, error)
}

// FixedClient adapts azure.RuntimeMonitor to the monitor Client contract.
type FixedClient struct{ Inner azure.RuntimeMonitor }

func (c FixedClient) query(ctx context.Context, accountID string, q azure.MetricQuery) (Result, error) {
	got, err := c.Inner.QueryAccountMetrics(ctx, accountID, q)
	if err != nil {
		return Result{}, err
	}
	out := Result{Truncated: got.Truncated, ErrorCode: got.ErrorCode}
	for _, s := range got.Series {
		dims := make(map[string]string, len(s.Dimensions))
		for k, v := range s.Dimensions {
			dims[strings.ToLower(k)] = v
		}
		out.Series = append(out.Series, Series{Dimensions: dims, Value: s.Value, Points: s.Points})
	}
	return out, nil
}

func (c FixedClient) QueryTraffic(ctx context.Context, accountID string) (Result, error) {
	return c.query(ctx, accountID, azure.MetricQuery{MetricName: MetricRequests, Aggregation: "Total"})
}

func (c FixedClient) QueryRequestsByStatus(ctx context.Context, accountID string) (Result, error) {
	return c.query(ctx, accountID, azure.MetricQuery{MetricName: MetricRequests, Aggregation: "Total", SplitDimensions: []string{DimDeployment, DimStatusCode}})
}

func (c FixedClient) QueryProvisionedUtilization(ctx context.Context, accountID string) (Result, error) {
	return c.query(ctx, accountID, azure.MetricQuery{MetricName: MetricUtilizationV2, Aggregation: "Maximum", SplitDimensions: []string{DimDeployment}})
}

func (c FixedClient) ListDiagnosticSettings(ctx context.Context, resourceID string) ([]DiagnosticSetting, error) {
	got, err := c.Inner.ListDiagnosticSettings(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	out := make([]DiagnosticSetting, 0, len(got))
	for _, s := range got {
		out = append(out, DiagnosticSetting{
			Name:                        s.Name,
			WorkspaceID:                 s.WorkspaceID,
			Categories:                  append([]string(nil), s.Categories...),
			CategoryGroups:              append([]string(nil), s.CategoryGroups...),
			LogAnalyticsDestinationType: s.LogAnalyticsDestinationType,
			AgeKnown:                    s.AgeKnown,
			AgeMinutes:                  s.AgeMinutes,
		})
	}
	return out, nil
}

func (c FixedClient) QueryDiagnosticCounts(ctx context.Context, workspaceID, accountName string, hours int) (LogsResult, error) {
	got, err := c.Inner.QueryDiagnosticCounts(ctx, workspaceID, accountName, hours)
	if err != nil {
		return LogsResult{}, err
	}
	out := LogsResult{Counts: map[string]int64{}}
	for k, v := range got.Counts {
		out.Counts[k] = v
	}
	return out, nil
}

// BuildQuery returns the fixed aggregate-only query template for RUN-007. It
// is the same template the allow-listed adapter sends (azure.DiagnosticCountsKQL).
func BuildQuery(accountName string, hours int) (string, bool) {
	return azure.DiagnosticCountsKQL(accountName, hours)
}

// DedicatedTables reports whether the diagnostic setting writes to
// resource-specific tables (logAnalyticsDestinationType "Dedicated") instead of
// AzureDiagnostics.
func (s DiagnosticSetting) DedicatedTables() bool {
	return strings.EqualFold(s.LogAnalyticsDestinationType, "Dedicated")
}

// SendsAuditOrRequestResponse reports whether the setting enables a log
// category or category group that carries Audit or RequestResponse rows:
// the categories themselves or the allLogs group.
func (s DiagnosticSetting) SendsAuditOrRequestResponse() bool {
	for _, c := range s.Categories {
		if c == "Audit" || c == "RequestResponse" {
			return true
		}
	}
	for _, g := range s.CategoryGroups {
		if strings.EqualFold(g, "allLogs") {
			return true
		}
	}
	return false
}
