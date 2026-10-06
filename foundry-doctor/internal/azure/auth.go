package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// AccessToken is a bearer token. It is held in memory only and never printed.
type AccessToken struct {
	Token     string
	ExpiresOn time.Time
}

// String redacts the token.
func (AccessToken) String() string { return "AccessToken{redacted}" }

// GoString redacts the token for %#v.
func (AccessToken) GoString() string { return "AccessToken{redacted}" }

// TokenCredential supplies bearer tokens for an OAuth scope such as
// "https://management.azure.com/.default".
type TokenCredential interface {
	Token(ctx context.Context, scope string) (AccessToken, error)
}

// ErrNoCredential is returned when no credential source yields a token.
var ErrNoCredential = errors.New("azure: no credential available")

// CommandRunner runs an external command and returns stdout only.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner is the production CommandRunner. Stderr is discarded so that it
// can never leak into errors or reports.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...).Output()
}

type cliSource struct {
	name string
	args func(scope string) []string
	// parse returns the token and expiry from stdout.
	parse func(out []byte) (AccessToken, error)
}

type chainCredential struct {
	run     CommandRunner
	now     func() time.Time
	sources []cliSource
	mu      sync.Mutex
	cache   map[string]AccessToken
}

// NewDefaultCredential checks AZURE_ACCESS_TOKEN first, then chains the Azure
// Developer CLI (`azd auth token`) and the Azure CLI
// (`az account get-access-token`), the credentials a developer is already
// signed in with. No secret is stored or written; tokens live in memory until
// shortly before expiry. A nil run uses ExecRunner.
func NewDefaultCredential(run CommandRunner) TokenCredential {
	return newChain(run, time.Now)
}

func newChain(run CommandRunner, now func() time.Time) *chainCredential {
	if run == nil {
		run = ExecRunner
	}
	return &chainCredential{run: run, now: now, cache: map[string]AccessToken{}, sources: []cliSource{
		{
			name: "env",
			args: func(string) []string { return nil },
			parse: func([]byte) (AccessToken, error) {
				if tok := strings.TrimSpace(os.Getenv("AZURE_ACCESS_TOKEN")); tok != "" {
					return AccessToken{Token: tok}, nil
				}
				return AccessToken{}, errors.New("AZURE_ACCESS_TOKEN not set")
			},
		},
		{
			name: "azd",
			args: func(scope string) []string {
				return []string{"auth", "token", "--output", "json", "--scope", scope}
			},
			parse: func(out []byte) (AccessToken, error) {
				var v struct {
					Token     string `json:"token"`
					ExpiresOn string `json:"expiresOn"`
				}
				if err := json.Unmarshal(out, &v); err != nil || v.Token == "" {
					return AccessToken{}, errors.New("unrecognised azd output")
				}
				return AccessToken{Token: v.Token, ExpiresOn: parseExpiry(v.ExpiresOn, 0)}, nil
			},
		},
		{
			name: "az",
			args: func(scope string) []string {
				return []string{"account", "get-access-token", "--output", "json", "--resource", strings.TrimSuffix(scope, "/.default")}
			},
			parse: func(out []byte) (AccessToken, error) {
				var v struct {
					AccessToken string `json:"accessToken"`
					ExpiresOn   string `json:"expiresOn"`
					ExpiresUnix int64  `json:"expires_on"`
				}
				if err := json.Unmarshal(out, &v); err != nil || v.AccessToken == "" {
					return AccessToken{}, errors.New("unrecognised az output")
				}
				return AccessToken{Token: v.AccessToken, ExpiresOn: parseExpiry(v.ExpiresOn, v.ExpiresUnix)}, nil
			},
		},
	}}
}

func parseExpiry(text string, unix int64) time.Time {
	if unix > 0 {
		return time.Unix(unix, 0)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.000000", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t
		}
	}
	return time.Time{} // unknown expiry: treated as not cacheable
}

func (c *chainCredential) Token(ctx context.Context, scope string) (AccessToken, error) {
	c.mu.Lock()
	if t, ok := c.cache[scope]; ok && t.ExpiresOn.After(c.now().Add(2*time.Minute)) {
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()
	var failures []string
	for _, s := range c.sources {
		var (
			out []byte
			err error
		)
		if s.name != "env" {
			out, err = c.run(ctx, s.name, s.args(scope)...)
			if ctx.Err() != nil {
				return AccessToken{}, ctx.Err()
			}
			if err != nil {
				failures = append(failures, s.name+": unavailable or not signed in")
				continue
			}
		}
		tok, perr := s.parse(out)
		if perr != nil {
			failures = append(failures, s.name+": "+perr.Error())
			continue
		}
		c.mu.Lock()
		c.cache[scope] = tok
		c.mu.Unlock()
		return tok, nil
	}
	return AccessToken{}, fmt.Errorf("%w (%s)", ErrNoCredential, strings.Join(failures, "; "))
}

// Claims are the non-secret identity claims read (not verified) from a token.
type Claims struct {
	ObjectID string
	TenantID string
	IDType   string
}

// ParseClaims decodes the JWT payload locally. It does not validate the
// signature; the claims are used only to identify the caller in reports.
func ParseClaims(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("azure: token is not a JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return Claims{}, errors.New("azure: token payload is not base64url")
	}
	var v struct {
		OID   string `json:"oid"`
		TID   string `json:"tid"`
		IDTyp string `json:"idtyp"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Claims{}, errors.New("azure: token payload is not JSON")
	}
	return Claims{ObjectID: v.OID, TenantID: v.TID, IDType: v.IDTyp}, nil
}

func (c Claims) principalType() string {
	switch {
	case c.IDType == "app":
		return "ServicePrincipal"
	case c.ObjectID != "":
		return "User"
	}
	return ""
}
