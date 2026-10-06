package monitor

import (
	"context"
	"net/http"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// WorkspaceClient is a small data-plane helper for recorded tests. The
// runtime rules use azure.RuntimeMonitor in production; this client exists so
// the package can exercise the POST-read query path directly.
type WorkspaceClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
}

// Query runs a fixed aggregate-only query against a workspace endpoint.
func (c WorkspaceClient) Query(ctx context.Context, endpoint string, body any, out any) error {
	return runtime.DoJSON(ctx, c.HTTP, c.Credential, "https://api.loganalytics.io/.default", http.MethodPost, endpoint, ".loganalytics.io", body, out)
}
