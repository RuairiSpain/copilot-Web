// Package foundry contains the metadata-only Foundry runtime probes and data
// plane client used by the runtime command.
package foundry

import (
	"context"
	"errors"
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
	ListAgents(context.Context, string) ([]Agent, error)
	GetAgentVersion(context.Context, string, string, string) (AgentVersion, error)
	ListConnections(context.Context, string) ([]Connection, error)
}

// Agent is a metadata-only agent summary.
type Agent struct {
	Name          string
	LatestVersion string
}

// AgentVersion is the metadata-only version status.
type AgentVersion struct {
	Name    string
	Version string
	Status  string
}

// Connection is the metadata-only data-plane connection summary.
type Connection struct {
	Name     string
	Category string
}

// HTTPClient implements ProjectReader against a Foundry project endpoint.
type HTTPClient struct {
	HTTP       *http.Client
	Credential azure.TokenCredential
}

const scope = "https://ai.azure.com/.default"

func (c HTTPClient) Ping(ctx context.Context, endpoint string) error {
	u := strings.TrimRight(endpoint, "/") + "/agents?api-version=v1&limit=1"
	var resp struct {
		Data []any `json:"data"`
	}
	return runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp)
}

func (c HTTPClient) ListAgents(ctx context.Context, endpoint string) ([]Agent, error) {
	u := strings.TrimRight(endpoint, "/") + "/agents?api-version=v1&limit=200"
	var resp struct {
		Data []struct {
			Name          string `json:"name"`
			LatestVersion string `json:"latestVersion"`
		} `json:"data"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, Agent{Name: a.Name, LatestVersion: a.LatestVersion})
	}
	return out, nil
}

func (c HTTPClient) GetAgentVersion(ctx context.Context, endpoint, name, version string) (AgentVersion, error) {
	v := version
	if v == "" {
		return AgentVersion{}, errors.New("agent version is required")
	}
	u := strings.TrimRight(endpoint, "/") + "/agents/" + url.PathEscape(name) + "/versions/" + url.PathEscape(v) + "?api-version=v1"
	var resp struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Status  string `json:"status"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return AgentVersion{}, err
	}
	return AgentVersion{Name: resp.Name, Version: resp.Version, Status: resp.Status}, nil
}

func (c HTTPClient) ListConnections(ctx context.Context, endpoint string) ([]Connection, error) {
	u := strings.TrimRight(endpoint, "/") + "/connections?api-version=v1"
	var resp struct {
		Data []struct {
			Name     string `json:"name"`
			Category string `json:"category"`
		} `json:"data"`
	}
	if err := runtime.DoJSON(ctx, c.HTTP, c.Credential, scope, http.MethodGet, u, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Connection, 0, len(resp.Data))
	for _, c := range resp.Data {
		out = append(out, Connection{Name: c.Name, Category: c.Category})
	}
	return out, nil
}

// Endpoint derives the standard public-cloud project endpoint.
func Endpoint(account, project string) string {
	return fmt.Sprintf("https://%s.services.ai.azure.com/api/projects/%s", account, project)
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
