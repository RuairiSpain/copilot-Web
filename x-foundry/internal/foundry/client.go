// Package foundry is a small client for the Microsoft Foundry project data plane (API version
// "v1"): agents and toolboxes. Paths and payload shapes follow the azure-ai-projects SDK.
package foundry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Scope is the token scope for the Foundry data plane.
const Scope = "https://ai.azure.com/.default"

// APIVersion is the data-plane API version the client speaks.
const APIVersion = "v1"

// ProjectURL is the endpoint of a project: https://<account>.services.ai.azure.com/api/projects/<project>.
func ProjectURL(account, project string) string {
	return "https://" + account + ".services.ai.azure.com/api/projects/" + project
}

// TokenFunc returns a bearer token for Scope.
type TokenFunc func(ctx context.Context) (string, error)

// Client talks to one project. BaseURL is the project endpoint.
type Client struct {
	BaseURL string
	Token   TokenFunc
	HTTP    *http.Client
	// Retries is the number of retries after a throttled (429) or unavailable (503) response.
	Retries int
	// Sleep waits between retries; tests replace it.
	Sleep func(time.Duration)
}

// Resource is the part of an agent or toolbox the engine needs: its name and latest version.
type Resource struct {
	Name    string
	Version string
}

// APIError is an error response from the service.
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("foundry: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("foundry: %d %s", e.Status, e.Message)
}

// IsNotFound reports whether err is a 404 from the service.
func IsNotFound(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.Status == http.StatusNotFound
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}
	target := strings.TrimRight(c.BaseURL, "/") + path
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("api-version", APIVersion)
	u.RawQuery = q.Encode()
	for attempt := 0; ; attempt++ {
		token, err := c.Token(ctx)
		if err != nil {
			return fmt.Errorf("foundry: getting a token: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.client().Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable
		if retryable && attempt < c.Retries {
			c.sleep(retryAfter(resp.Header.Get("Retry-After"), attempt))
			continue
		}
		if resp.StatusCode >= 300 {
			return parseError(resp.StatusCode, data)
		}
		if out != nil && len(data) > 0 {
			return json.Unmarshal(data, out)
		}
		return nil
	}
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

// retryAfter honours Retry-After seconds, else backs off exponentially from one second.
func retryAfter(header string, attempt int) time.Duration {
	if s, err := strconv.Atoi(header); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}

func parseError(status int, data []byte) error {
	e := &APIError{Status: status, Message: http.StatusText(status)}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && (body.Error.Code != "" || body.Error.Message != "") {
		e.Code, e.Message = body.Error.Code, body.Error.Message
	}
	return e
}

// ------------------------------------------------------------------------------- agents

type versionEnvelope struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Versions struct {
		Latest struct {
			Version string `json:"version"`
		} `json:"latest"`
	} `json:"versions"`
	DefaultVersion string `json:"default_version"`
}

func (v versionEnvelope) latest() string {
	switch {
	case v.Versions.Latest.Version != "":
		return v.Versions.Latest.Version
	case v.DefaultVersion != "":
		return v.DefaultVersion
	}
	return v.Version
}

func (c *Client) get(ctx context.Context, path string) (*Resource, error) {
	var env versionEnvelope
	if err := c.do(ctx, http.MethodGet, path, nil, &env); err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &Resource{Name: env.Name, Version: env.latest()}, nil
}

func (c *Client) createVersion(ctx context.Context, path string, body any) (*Resource, error) {
	var env versionEnvelope
	if err := c.do(ctx, http.MethodPost, path, body, &env); err != nil {
		return nil, err
	}
	return &Resource{Name: env.Name, Version: env.latest()}, nil
}

func (c *Client) delete(ctx context.Context, path string) error {
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

// GetAgent returns the agent, or nil when it does not exist.
func (c *Client) GetAgent(ctx context.Context, name string) (*Resource, error) {
	return c.get(ctx, "/agents/"+url.PathEscape(name))
}

// CreateAgentVersion creates a new, immutable version of an agent (creating the agent if needed).
func (c *Client) CreateAgentVersion(ctx context.Context, name string, body map[string]any) (*Resource, error) {
	return c.createVersion(ctx, "/agents/"+url.PathEscape(name)+"/versions", body)
}

// DeleteAgent deletes an agent and all its versions; a missing agent is not an error.
func (c *Client) DeleteAgent(ctx context.Context, name string) error {
	return c.delete(ctx, "/agents/"+url.PathEscape(name))
}

// ----------------------------------------------------------------------------- toolboxes

// GetToolbox returns the toolbox, or nil when it does not exist.
func (c *Client) GetToolbox(ctx context.Context, name string) (*Resource, error) {
	return c.get(ctx, "/toolboxes/"+url.PathEscape(name))
}

// CreateToolboxVersion creates a new, immutable version of a toolbox (creating the toolbox if needed).
func (c *Client) CreateToolboxVersion(ctx context.Context, name string, body map[string]any) (*Resource, error) {
	r, err := c.createVersion(ctx, "/toolboxes/"+url.PathEscape(name)+"/versions", body)
	if r != nil && r.Name == "" {
		r.Name = name
	}
	return r, err
}

// DeleteToolbox deletes a toolbox and all its versions; a missing toolbox is not an error.
func (c *Client) DeleteToolbox(ctx context.Context, name string) error {
	return c.delete(ctx, "/toolboxes/"+url.PathEscape(name))
}

// ToolboxMCPURL is the MCP endpoint of one toolbox version, which agents use to call its tools.
func (c *Client) ToolboxMCPURL(name, version string) string {
	return strings.TrimRight(c.BaseURL, "/") + "/toolboxes/" + url.PathEscape(name) + "/versions/" + url.PathEscape(version) + "/mcp?api-version=" + APIVersion
}
