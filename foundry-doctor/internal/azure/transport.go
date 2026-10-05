package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the Azure public cloud ARM endpoint.
const DefaultEndpoint = "https://management.azure.com"

const (
	maxBodyBytes     = 32 << 20
	maxPages         = 100
	defaultResources = 1000
	maxRetryAfter    = 30 * time.Second
)

// ErrNotAllowed is returned (before any network I/O) when a request is not on
// the ADR-006 read-only allow-list.
var ErrNotAllowed = errors.New("azure: request is not on the read-only allow-list")

// ErrInvalidInput is returned for malformed caller input (for example a
// hostile resource name) before any network I/O.
var ErrInvalidInput = errors.New("azure: invalid input")

const (
	guidRe  = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	segRe   = `[A-Za-z0-9._()~-]{1,90}`
	scopeRe = `/subscriptions/` + guidRe + `(?:/resourceGroups/` + segRe + `)?`
)

// AllowedOperation documents one allow-listed request shape.
type AllowedOperation struct {
	Name       string
	Method     string
	Template   string
	APIVersion string
	// Capability is the RBAC action the call exercises.
	Capability string
}

type operation struct {
	AllowedOperation
	re *regexp.Regexp
}

func newOp(name, method, template, version, capability string) operation {
	pat := regexp.QuoteMeta(template)
	pat = strings.ReplaceAll(pat, `\{scope\}`, scopeRe)
	pat = strings.ReplaceAll(pat, `\{sub\}`, guidRe)
	pat = strings.ReplaceAll(pat, `\{seg\}`, segRe)
	pat = strings.ReplaceAll(pat, `\{roledef\}`, `(?:/subscriptions/`+guidRe+`)?/providers/Microsoft\.Authorization/roleDefinitions/`+guidRe)
	return operation{
		AllowedOperation: AllowedOperation{Name: name, Method: method, Template: template, APIVersion: version, Capability: capability},
		re:               regexp.MustCompile(`(?i)^` + pat + `$`),
	}
}

// allowList is the complete set of requests the adapters may send. Versions
// marked "catalogue" come from rules/catalog/dep/*.yaml apiVersions; others are
// recorded in docs/permissions-matrix.md as awaiting catalogue verification.
func allowList() []operation {
	const g, p = http.MethodGet, http.MethodPost
	return []operation{
		// catalogue: FND-DEP-001
		newOp("subscription.get", g, "/subscriptions/{sub}", "2022-12-01", "Microsoft.Resources/subscriptions/read"),
		// catalogue: FND-DEP-002
		newOp("provider.get", g, "/subscriptions/{sub}/providers/{seg}", "2025-04-01", "Microsoft.Resources/subscriptions/providers/read"),
		// unverified: resource groups and resource listing use the Microsoft.Resources 2022-12-01 spec
		newOp("resourcegroup.get", g, "/subscriptions/{sub}/resourcegroups/{seg}", "2022-12-01", "Microsoft.Resources/subscriptions/resourceGroups/read"),
		newOp("resources.list", g, "{scope}/resources", "2022-12-01", "Microsoft.Resources/subscriptions/resources/read"),
		// catalogue: locks 2020-05-01
		newOp("locks.list", g, "{scope}/providers/Microsoft.Authorization/locks", "2020-05-01", "Microsoft.Authorization/locks/read"),
		// catalogue: virtualNetworks/subnets 2025-05-01
		newOp("subnet.get", g, "/subscriptions/{sub}/resourceGroups/{seg}/providers/Microsoft.Network/virtualNetworks/{seg}/subnets/{seg}", "2025-05-01", "Microsoft.Network/virtualNetworks/subnets/read"),
		// catalogue: roleAssignments 2022-04-01
		// catalogue FND-DEP-012 (subscriptions 2022-12-01)
		newOp("locations.list", g, "/subscriptions/{sub}/locations", "2022-12-01", "Microsoft.Resources/subscriptions/locations/read"),
		// catalogue FND-DEP-010 (deployments 2026-06-01); only the count is used
		newOp("deployments.list", g, "/subscriptions/{sub}/resourcegroups/{seg}/providers/Microsoft.Resources/deployments", "2026-06-01", "Microsoft.Resources/deployments/read"),
		// catalogue FND-DEP-011 (2025-05-01); exact RBAC action for link reads UNVERIFIED
		newOp("subnet.associationlinks", g, "/subscriptions/{sub}/resourceGroups/{seg}/providers/Microsoft.Network/virtualNetworks/{seg}/subnets/{seg}/ServiceAssociationLinks", "2025-05-01", "Microsoft.Network/virtualNetworks/subnets/read"),
		newOp("subnet.navigationlinks", g, "/subscriptions/{sub}/resourceGroups/{seg}/providers/Microsoft.Network/virtualNetworks/{seg}/subnets/{seg}/ResourceNavigationLinks", "2025-05-01", "Microsoft.Network/virtualNetworks/subnets/read"),
		// catalogue FND-DEP-003 (permissions 2022-04-01; RG and resource scope only)
		newOp("permissions.list", g, "{scope}/providers/Microsoft.Authorization/permissions", "2022-04-01", "Microsoft.Authorization/permissions/read"),
		newOp("roleassignments.list", g, "{scope}/providers/Microsoft.Authorization/roleAssignments", "2022-04-01", "Microsoft.Authorization/roleAssignments/read"),
		newOp("roledefinition.get", g, "{roledef}", "2022-04-01", "Microsoft.Authorization/roleDefinitions/read"),
		// Learn "List Azure deny assignments": 2018-07-01-preview or later
		newOp("denyassignments.list", g, "{scope}/providers/Microsoft.Authorization/denyAssignments", "2022-04-01", "Microsoft.Authorization/denyAssignments/read"),
		// catalogue: FND-DEP-004, FND-DEP-005, FND-DEP-006
		newOp("models.list", g, "/subscriptions/{sub}/providers/Microsoft.CognitiveServices/locations/{seg}/models", "2026-09-01", "Microsoft.CognitiveServices/locations/models/read"),
		newOp("usages.list", g, "/subscriptions/{sub}/providers/Microsoft.CognitiveServices/locations/{seg}/usages", "2026-09-01", "Microsoft.CognitiveServices/locations/usages/read"),
		newOp("deletedaccounts.list", g, "/subscriptions/{sub}/providers/Microsoft.CognitiveServices/deletedAccounts", "2026-09-01", "Microsoft.CognitiveServices/deletedAccounts/read"),
		newOp("deletedvaults.list", g, "/subscriptions/{sub}/providers/Microsoft.KeyVault/deletedVaults", "2026-05-15", "Microsoft.KeyVault/deletedVaults/read"),
		newOp("deletedservices.list", g, "/subscriptions/{sub}/providers/Microsoft.ApiManagement/deletedservices", "2024-05-01", "Microsoft.ApiManagement/deletedservices/read"),
		// catalogue: policyAssignments 2026-07-01; exemptions/restrictions unverified
		newOp("policyassignments.list", g, "{scope}/providers/Microsoft.Authorization/policyAssignments", "2026-07-01", "Microsoft.Authorization/policyAssignments/read"),
		newOp("policyexemptions.list", g, "{scope}/providers/Microsoft.Authorization/policyExemptions", "2022-07-01-preview", "Microsoft.Authorization/policyExemptions/read"),
		newOp("policy.checkrestrictions", p, "{scope}/providers/Microsoft.PolicyInsights/checkPolicyRestrictions", "2024-10-01", "Microsoft.PolicyInsights/checkPolicyRestrictions/read"),
		// catalogue: deployments whatIf 2026-06-01 (explicit opt-in only)
		newOp("whatif.run", p, "{scope}/providers/Microsoft.Resources/deployments/{seg}/whatIf", "2026-06-01", "Microsoft.Resources/deployments/whatIf/action"),
		newOp("whatif.poll", g, "{scope}/providers/Microsoft.Resources/deployments/{seg}/operationStatuses/{seg}", "2026-06-01", "Microsoft.Resources/deployments/read"),
		// unverified name-availability versions (ADR-006 allows these POSTs)
		newOp("name.keyvault", p, "/subscriptions/{sub}/providers/Microsoft.KeyVault/checkNameAvailability", "2026-05-15", "Microsoft.KeyVault/checkNameAvailability/read"),
		newOp("name.storage", p, "/subscriptions/{sub}/providers/Microsoft.Storage/checkNameAvailability", "2026-09-01", "Microsoft.Storage/checknameavailability/read"),
		newOp("name.apim", p, "/subscriptions/{sub}/providers/Microsoft.ApiManagement/checkNameAvailability", "2024-05-01", "Microsoft.ApiManagement/checkNameAvailability/read"),
		newOp("name.search", p, "/subscriptions/{sub}/providers/Microsoft.Search/checkNameAvailability", "2025-05-01", "Microsoft.Search/checkNameAvailability/action"),
		newOp("name.foundry", p, "/subscriptions/{sub}/providers/Microsoft.CognitiveServices/checkDomainAvailability", "2026-09-01", "Microsoft.CognitiveServices/checkDomainAvailability/action"),
	}
}

// AllowedOperations returns the allow-list for documentation and tests.
func AllowedOperations() []AllowedOperation {
	ops := allowList()
	out := make([]AllowedOperation, len(ops))
	for i, o := range ops {
		out[i] = o.AllowedOperation
	}
	return out
}

// IsAllowed reports whether method+path matches an allow-listed template.
func IsAllowed(method, path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == ".." || seg == "." {
			return false
		}
	}
	for _, o := range allowList() {
		if o.Method == method && o.re.MatchString(path) {
			return true
		}
	}
	return false
}

func allowedQueryKey(k string) bool {
	switch strings.ToLower(k) {
	case "$filter", "$skiptoken", "skiptoken", "$top", "$expand", "$skip":
		return true
	}
	return false
}

// APIError is a non-success ARM response reduced to status and error code.
// Response bodies are never retained.
type APIError struct {
	Operation string
	Status    int
	Code      string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("azure: %s: HTTP %d", e.Operation, e.Status)
	}
	return fmt.Sprintf("azure: %s: HTTP %d (%s)", e.Operation, e.Status, e.Code)
}

func isNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

var safeCode = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func errorCode(body []byte) string {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && safeCode.MatchString(e.Error.Code) {
		return e.Error.Code
	}
	return ""
}

type response struct {
	body   []byte
	header http.Header
	status int
}

type armClient struct {
	cred       TokenCredential
	http       *http.Client
	origin     string
	base       string
	scope      string
	sem        chan struct{}
	maxRetries int
	sleep      func(context.Context, time.Duration) error
	ops        []operation
}

func (c *armClient) match(method, path string) (operation, bool) {
	for _, o := range c.ops {
		if o.Method == method && o.re.MatchString(path) {
			return o, true
		}
	}
	return operation{}, false
}

func retryDelay(h http.Header, attempt int) time.Duration {
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if s, err := strconv.Atoi(ra); err == nil && s >= 0 {
			return min(time.Duration(s)*time.Second, maxRetryAfter)
		}
		if t, err := http.ParseTime(ra); err == nil {
			return min(max(time.Until(t), 0), maxRetryAfter)
		}
	}
	return min(500*time.Millisecond<<uint(attempt), 8*time.Second)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// do performs one allow-listed request with bounded concurrency and retry.
func (c *armClient) do(ctx context.Context, method, path string, query url.Values, body any) (response, error) {
	o, ok := c.match(method, path)
	if !ok {
		return response{}, fmt.Errorf("%w: %s %s", ErrNotAllowed, method, path)
	}
	vals := url.Values{}
	for k, vs := range query {
		if !allowedQueryKey(k) {
			return response{}, fmt.Errorf("%w: query parameter %q", ErrNotAllowed, k)
		}
		vals[k] = vs
	}
	vals.Set("api-version", o.APIVersion)
	full := c.base + path + "?" + strings.ReplaceAll(vals.Encode(), "+", "%20")
	var payload []byte
	if body != nil {
		if method != http.MethodPost {
			return response{}, fmt.Errorf("%w: body on %s", ErrNotAllowed, method)
		}
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return response{}, fmt.Errorf("azure: %s: encode body: %w", o.Name, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return response{}, err
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
	defer func() { <-c.sem }()

	for attempt := 0; ; attempt++ {
		tok, err := c.cred.Token(ctx, c.scope)
		if err != nil {
			if ctx.Err() != nil {
				return response{}, ctx.Err()
			}
			return response{}, &UnavailableError{Capability: o.Capability + " at " + path, Reason: "no usable Azure credential (run `azd auth login` or `az login`)", Err: err}
		}
		req, err := http.NewRequestWithContext(ctx, method, full, bytes.NewReader(payload))
		if err != nil {
			return response{}, fmt.Errorf("azure: %s: build request: %w", o.Name, err)
		}
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "foundry-doctor")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return response{}, ctx.Err()
			}
			if attempt < c.maxRetries {
				if serr := c.sleep(ctx, retryDelay(http.Header{}, attempt)); serr != nil {
					return response{}, serr
				}
				continue
			}
			return response{}, fmt.Errorf("azure: %s: transport failure: %w", o.Name, err)
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		_ = resp.Body.Close()
		if rerr != nil {
			return response{}, fmt.Errorf("azure: %s: read response: %w", o.Name, rerr)
		}
		if len(data) > maxBodyBytes {
			return response{}, fmt.Errorf("azure: %s: response exceeds %d bytes", o.Name, maxBodyBytes)
		}
		st := resp.StatusCode
		switch {
		case st >= 200 && st < 300:
			return response{body: data, header: resp.Header, status: st}, nil
		case st == http.StatusTooManyRequests || st == 500 || st == 502 || st == 503 || st == 504:
			if attempt < c.maxRetries {
				if serr := c.sleep(ctx, retryDelay(resp.Header, attempt)); serr != nil {
					return response{}, serr
				}
				continue
			}
			return response{}, &APIError{Operation: o.Name, Status: st, Code: errorCode(data)}
		case st == http.StatusUnauthorized:
			return response{}, &UnavailableError{Capability: o.Capability + " at " + path, Reason: "credential rejected by Azure (HTTP 401)", Err: &APIError{Operation: o.Name, Status: st, Code: errorCode(data)}}
		case st == http.StatusForbidden:
			return response{}, &UnavailableError{Capability: o.Capability + " at " + path, Permission: o.Capability, Err: &APIError{Operation: o.Name, Status: st, Code: errorCode(data)}}
		default:
			return response{}, &APIError{Operation: o.Name, Status: st, Code: errorCode(data)}
		}
	}
}

func (c *armClient) get(ctx context.Context, path string, q url.Values, out any) error {
	r, err := c.do(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(r.body, out); err != nil {
		return fmt.Errorf("azure: decode response for %s: %w", path, err)
	}
	return nil
}

// list follows nextLink (same origin, allow-listed path only) up to max items
// (<=0 means unbounded within maxPages). truncated is true when cut short.
func (c *armClient) list(ctx context.Context, path string, q url.Values, limit int) ([]json.RawMessage, bool, error) {
	curPath, curQ := path, q
	var items []json.RawMessage
	for page := 0; page < maxPages; page++ {
		var pg struct {
			Value    []json.RawMessage `json:"value"`
			NextLink string            `json:"nextLink"`
		}
		if err := c.get(ctx, curPath, curQ, &pg); err != nil {
			return nil, false, err
		}
		items = append(items, pg.Value...)
		if limit > 0 && len(items) > limit {
			return items[:limit], true, nil
		}
		if pg.NextLink == "" {
			return items, false, nil
		}
		u, err := url.Parse(pg.NextLink)
		if err != nil || !strings.EqualFold(u.Scheme+"://"+u.Host, c.origin) {
			return nil, false, fmt.Errorf("%w: nextLink leaves the configured endpoint", ErrNotAllowed)
		}
		curPath, curQ = u.Path, u.Query()
		curQ.Del("api-version")
	}
	return items, true, nil
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	cp := append([]string(nil), in...)
	sort.Strings(cp)
	out := cp[:1]
	for _, s := range cp[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
