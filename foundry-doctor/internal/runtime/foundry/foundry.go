// Package foundry contains the metadata-only Foundry runtime probes and data
// plane client used by the runtime command.
//
// Shapes and paging follow the Foundry v1 data-plane specification
// (Azure/azure-rest-api-specs specification/ai-foundry/data-plane/Foundry/openapi3/v1,
// microsoft-foundry-openapi3.json, info.version v1):
//   - GET /agents returns {data, first_id, last_id, has_more}; limit ranges 1 to
//     100 and the next page is requested with after={last_id}.
//   - An agent object carries name, state (enabled|disabled) and
//     versions.latest.{version,status}; status is creating|active|failed|deleting|deleted
//     and "defaults to 'active' for non-hosted agents".
//   - GET /connections returns PagedConnection {value, nextLink}; a connection
//     has name, type (CognitiveSearch, CosmosDB, AzureStorageAccount, ...),
//     target, isDefault, credentials and metadata. There is no "category".
package foundry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/runtime"
)

// ProjectReader is the metadata-only interface to the project endpoint.
// It intentionally exposes no document, prompt, completion or tool-invocation
// operations.
type ProjectReader interface {
	Ping(context.Context, string) error
	ListAgents(context.Context, string) (AgentList, error)
	ListConnections(context.Context, string) (ConnectionList, error)
}

// Agent is a metadata-only agent summary. Instructions and definitions are
// never decoded.
type Agent struct {
	Name string
	// State is the operational state (enabled or disabled); empty when absent.
	State string
	// LatestVersion and LatestStatus come from versions.latest. LatestStatus is
	// empty when the service does not report it (the spec default is active).
	LatestVersion string
	LatestStatus  string
}

// AgentList is one complete (or capped) agent listing.
type AgentList struct {
	Agents []Agent
	// Truncated is true when paging stopped at the page cap with has_more still set.
	Truncated bool
}

// Connection is the metadata-only data-plane connection summary.
type Connection struct {
	Name string
	// Type is the spec's connection type (not "category").
	Type string
}

// ConnectionList is one complete (or capped) connection listing.
type ConnectionList struct {
	Connections []Connection
	Truncated   bool
}

// HTTPClient implements ProjectReader against a Foundry project endpoint.
type HTTPClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
}

const (
	scope      = "https://ai.azure.com/.default"
	hostSuffix = ".services.ai.azure.com"
	// agentPageSize is the maximum the spec allows for limit.
	agentPageSize = 100
	// maxPages bounds both listings (1000 agents at most).
	maxPages = 10
)

func (c HTTPClient) Ping(ctx context.Context, endpoint string) error {
	u := strings.TrimRight(endpoint, "/") + "/agents?api-version=v1&limit=1"
	var resp struct {
		Data []any `json:"data"`
	}
	return runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp)
}

func (c HTTPClient) ListAgents(ctx context.Context, endpoint string) (AgentList, error) {
	base := strings.TrimRight(endpoint, "/") + "/agents?api-version=v1&limit=" + fmt.Sprint(agentPageSize)
	var out AgentList
	after := ""
	for page := 0; page < maxPages; page++ {
		u := base
		if after != "" {
			u += "&after=" + url.QueryEscape(after)
		}
		var resp struct {
			Data []struct {
				Name     string `json:"name"`
				State    string `json:"state"`
				Versions struct {
					Latest struct {
						Version string `json:"version"`
						Status  string `json:"status"`
					} `json:"latest"`
				} `json:"versions"`
			} `json:"data"`
			LastID  string `json:"last_id"`
			HasMore bool   `json:"has_more"`
		}
		if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp); err != nil {
			return AgentList{}, err
		}
		for _, a := range resp.Data {
			out.Agents = append(out.Agents, Agent{Name: a.Name, State: a.State, LatestVersion: a.Versions.Latest.Version, LatestStatus: a.Versions.Latest.Status})
		}
		if !resp.HasMore {
			return out, nil
		}
		if resp.LastID == "" || resp.LastID == after {
			out.Truncated = true
			return out, nil
		}
		after = resp.LastID
	}
	out.Truncated = true
	return out, nil
}

func (c HTTPClient) ListConnections(ctx context.Context, endpoint string) (ConnectionList, error) {
	first := strings.TrimRight(endpoint, "/") + "/connections?api-version=v1"
	var out ConnectionList
	u := first
	for page := 0; page < maxPages; page++ {
		var resp struct {
			Value []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"value"`
			NextLink string `json:"nextLink"`
		}
		if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, hostSuffix, nil, &resp); err != nil {
			return ConnectionList{}, err
		}
		for _, v := range resp.Value {
			out.Connections = append(out.Connections, Connection{Name: v.Name, Type: v.Type})
		}
		if resp.NextLink == "" {
			return out, nil
		}
		// The bearer token only follows a nextLink that stays on the endpoint host.
		if !runtime.SameOrigin(first, resp.NextLink) {
			return ConnectionList{}, fmt.Errorf("%w: connections nextLink leaves the project endpoint", azure.ErrNotAllowed)
		}
		u = resp.NextLink
	}
	out.Truncated = true
	return out, nil
}

// Endpoint derives the standard public-cloud project endpoint.
func Endpoint(account, project string) (string, error) {
	if err := runtime.ValidateDataPlaneName("foundry account", account); err != nil {
		return "", err
	}
	if err := runtime.ValidateDataPlaneName("foundry project", project); err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.services.ai.azure.com/api/projects/%s", account, project), nil
}

// Probe is a scheduler probe wrapper.
type Probe struct {
	ProbeID    string
	ProbeClass runtime.Class
	ProbeTime  time.Duration
	RunFunc    func(context.Context) (runtime.Result, error)
}

func (p Probe) ID() string                                      { return p.ProbeID }
func (p Probe) Class() runtime.Class                            { return p.ProbeClass }
func (p Probe) Timeout() time.Duration                          { return p.ProbeTime }
func (p Probe) Run(ctx context.Context) (runtime.Result, error) { return p.RunFunc(ctx) }
