package azure

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Options configures the read-only Azure adapters. All fields are optional.
type Options struct {
	// Credential defaults to NewDefaultCredential(nil) (azd, then az CLI).
	Credential TokenCredential
	// Endpoint is the ARM endpoint; defaults to DefaultEndpoint.
	Endpoint string
	// HTTPClient defaults to a client with a 60s timeout and no redirects.
	HTTPClient *http.Client
	// MaxConcurrency bounds in-flight requests (default 4).
	MaxConcurrency int
	// MaxRetries bounds retries for 429/5xx/transport errors (default 3).
	MaxRetries int
	// Sleep replaces the backoff timer (tests).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Adapter implements every capability interface with strictly read-only ARM
// calls restricted to the allow-list in transport.go.
type Adapter struct {
	c *armClient
}

var (
	_ ContextProvider = (*Adapter)(nil)
	_ Inventory       = (*Adapter)(nil)
	_ Permissions     = (*Adapter)(nil)
	_ Models          = (*Adapter)(nil)
	_ Policy          = (*Adapter)(nil)
	_ WhatIf          = (*Adapter)(nil)
	_ Names           = (*Adapter)(nil)
)

// New builds an Adapter. It performs no network I/O.
func New(o Options) (*Adapter, error) {
	endpoint := strings.TrimRight(o.Endpoint, "/")
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" {
		return nil, fmt.Errorf("%w: endpoint must be an https origin", ErrInvalidInput)
	}
	cred := o.Credential
	if cred == nil {
		cred = NewDefaultCredential(nil)
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	cp := *hc
	// Never follow redirects: they could carry the bearer token elsewhere.
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	conc := o.MaxConcurrency
	if conc <= 0 {
		conc = 4
	}
	retries := o.MaxRetries
	if retries <= 0 {
		retries = 3
	}
	sleep := o.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	return &Adapter{c: &armClient{
		cred: cred, http: &cp, origin: u.Scheme + "://" + u.Host, base: endpoint,
		scope: endpoint + "/.default", sem: make(chan struct{}, conc),
		maxRetries: retries, sleep: sleep, ops: allowList(),
	}}, nil
}

// Client returns the aggregate wired to this adapter.
func (a *Adapter) Client() Client {
	return Client{Context: a, Inventory: a, Permissions: a, Models: a, Policy: a, WhatIf: a, Names: a,
		Regions: a, Deployments: a, SubnetLinks: a, Evidence: a}
}

var (
	subIDRe   = regexp.MustCompile(`^` + guidRe + `$`)
	segOnlyRe = regexp.MustCompile(`^` + segRe + `$`)
	locationR = regexp.MustCompile(`^[A-Za-z0-9]{2,40}$`)
)

func requireSub(id string) error {
	if !subIDRe.MatchString(id) {
		return fmt.Errorf("%w: subscription id", ErrInvalidInput)
	}
	return nil
}

func requireLocation(l string) error {
	if !locationR.MatchString(l) {
		return fmt.Errorf("%w: location", ErrInvalidInput)
	}
	return nil
}

func subPath(sub, rest string) string { return "/subscriptions/" + sub + rest }

// resourceGroupOf extracts the resource group from an ARM id.
func resourceGroupOf(id string) string {
	parts := strings.Split(id, "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "resourceGroups") {
			return parts[i+1]
		}
	}
	return ""
}

func (c *armClient) principal(ctx context.Context) (Claims, error) {
	tok, err := c.cred.Token(ctx, c.scope)
	if err != nil {
		if ctx.Err() != nil {
			return Claims{}, ctx.Err()
		}
		return Claims{}, &UnavailableError{Capability: "caller identity", Reason: "no usable Azure credential (run `azd auth login` or `az login`)", Err: err}
	}
	cl, perr := ParseClaims(tok.Token)
	if perr != nil {
		return Claims{}, &UnavailableError{Capability: "caller identity", Reason: "token has no readable object id claim", Err: perr}
	}
	return cl, nil
}
