package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// ErrWhatIfNotOptedIn is returned, without any Azure call, when WhatIfRequest.OptIn is false.
var ErrWhatIfNotOptedIn = errors.New("azure: what-if requires explicit opt-in")

const (
	whatIfDeploymentName = "foundry-doctor-whatif"
	maxWhatIfPolls       = 120
)

type armErrorBody struct {
	Code    string         `json:"code"`
	Details []armErrorBody `json:"details"`
}

func (e *armErrorBody) collect(into *[]string) {
	if e == nil {
		return
	}
	if safeCode.MatchString(e.Code) {
		*into = append(*into, e.Code)
	}
	for i := range e.Details {
		e.Details[i].collect(into)
	}
}

type whatIfChangeBody struct {
	ResourceID string `json:"resourceId"`
	ChangeType string `json:"changeType"`
}

// whatIfBody decodes WhatIfOperationResult (Microsoft.Resources deployments
// spec 2026-06-01): status, properties.changes[], properties.potentialChanges[]
// and error. The before, after and delta members are deliberately not decoded,
// so no resource payload can be retained or logged (ADR-006 point 8).
type whatIfBody struct {
	Status     string `json:"status"`
	Properties struct {
		Changes          []whatIfChangeBody `json:"changes"`
		PotentialChanges []whatIfChangeBody `json:"potentialChanges"`
		Error            *armErrorBody      `json:"error"`
	} `json:"properties"`
	Error *armErrorBody `json:"error"`
}

// whatIfCapability is the RBAC action what-if needs (permissions table).
const whatIfCapability = "Microsoft.Resources/deployments/whatIf/action"

// Run implements WhatIf. It is the only ARM deployment-scoped call and never
// creates, updates or deletes a resource. Request bodies carry the template and
// parameters; response before/after payloads are not requested
// (resultFormat=ResourceIdOnly) and are never retained.
//
// Note for ADR-006 point 8, which says the doctor requests FullResourcePayloads
// and discards the bodies: the code uses ResourceIdOnly, so no payload leaves
// Azure at all. The cost, documented in Learn deploy-what-if.md ("Deploy ... You
// only see this condition when ResultFormat is set to ResourceIdOnly"), is that
// an existing resource is reported as Deploy instead of Modify or NoChange;
// Create and Delete are still reported. The request also uses mode Incremental,
// in which Delete essentially never appears, and ARM has no change type for a
// recreation caused by an immutable-property change. A caller must therefore
// never treat an empty destructive list as proof that nothing is destroyed.
func (a *Adapter) Run(ctx context.Context, req WhatIfRequest) (WhatIfResult, error) {
	if !req.OptIn {
		return WhatIfResult{}, ErrWhatIfNotOptedIn
	}
	if req.Scope == "" || !json.Valid(req.Template) {
		return WhatIfResult{}, fmt.Errorf("%w: what-if needs a scope and a valid ARM JSON template", ErrInvalidInput)
	}
	props := map[string]any{
		"mode":     "Incremental",
		"template": json.RawMessage(req.Template),
	}
	if len(req.Parameters) > 0 {
		params := map[string]any{}
		for k, v := range req.Parameters {
			params[k] = map[string]any{"value": v}
		}
		props["parameters"] = params
	}
	body := map[string]any{"properties": props}
	if !strings.Contains(strings.ToLower(req.Scope), "/resourcegroups/") {
		if err := requireLocation(req.Location); err != nil {
			return WhatIfResult{}, fmt.Errorf("%w: subscription-scope what-if needs a location", ErrInvalidInput)
		}
		body["location"] = req.Location
	}
	body["properties"].(map[string]any)["whatIfSettings"] = map[string]any{"resultFormat": "ResourceIdOnly"}

	scope := strings.TrimRight(req.Scope, "/")
	r, err := a.c.do(ctx, http.MethodPost, scope+"/providers/Microsoft.Resources/deployments/"+whatIfDeploymentName+"/whatIf", nil, body)
	if err != nil {
		return WhatIfResult{}, err
	}
	for polls := 0; r.status == http.StatusAccepted; polls++ {
		if polls >= maxWhatIfPolls {
			return WhatIfResult{}, errors.New("azure: what-if did not finish within the polling bound")
		}
		loc, perr := url.Parse(r.header.Get("Location"))
		if perr != nil || !strings.EqualFold(loc.Scheme+"://"+loc.Host, a.c.origin) {
			return WhatIfResult{}, fmt.Errorf("%w: what-if status location", ErrNotAllowed)
		}
		if serr := a.c.sleep(ctx, retryDelay(http.Header{"Retry-After": r.header.Values("Retry-After")}, 0)); serr != nil {
			return WhatIfResult{}, serr
		}
		q := loc.Query()
		q.Del("api-version")
		if r, err = a.c.do(ctx, http.MethodGet, loc.Path, q, nil); err != nil {
			return WhatIfResult{}, err
		}
	}
	var wb whatIfBody
	if err := json.Unmarshal(r.body, &wb); err != nil {
		return WhatIfResult{}, fmt.Errorf("azure: decode what-if result: %w", err)
	}
	var codes []string
	wb.Error.collect(&codes)
	wb.Properties.Error.collect(&codes)
	codes = sortedUnique(codes)
	// Only a final status of "Succeeded" proves the change list is complete. A
	// Failed or Canceled operation, or a body without a status, would otherwise
	// look like "no changes" and read as a vacuous pass.
	if !strings.EqualFold(wb.Status, "Succeeded") {
		reason := "what-if finished with status " + statusLabel(wb.Status) + ", so no change list is available"
		if len(codes) > 0 {
			reason += " (error codes: " + strings.Join(codes, ",") + ")"
		}
		u := &UnavailableError{Capability: whatIfCapability + " (what-if result)", Reason: reason}
		for _, c := range codes {
			if strings.Contains(strings.ToLower(c), "authorizationfailed") {
				u.Permission = whatIfCapability
			}
		}
		return WhatIfResult{}, u
	}
	return WhatIfResult{
		Status: "Succeeded", DiagnosticCodes: codes,
		Changes: mapChanges(wb.Properties.Changes), PotentialChanges: mapChanges(wb.Properties.PotentialChanges),
	}, nil
}

// statusLabel renders a status for a reason text: only a short token is kept.
func statusLabel(s string) string {
	if safeCode.MatchString(s) {
		return s
	}
	if s == "" {
		return "(none)"
	}
	return "(unrecognised)"
}

// mapChanges maps the response entries one to one. A Delete and a Create of the
// same resource ID are both reported as ARM returned them: ARM has no Replace
// change type, so a recreation is not inferred (Phase 2 gate finding F7).
func mapChanges(in []whatIfChangeBody) []WhatIfChange {
	seen := map[WhatIfChange]bool{}
	out := []WhatIfChange{}
	for _, c := range in {
		w := WhatIfChange{ResourceID: c.ResourceID, ChangeType: normaliseChange(c.ChangeType)}
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ResourceID != out[j].ResourceID {
			return out[i].ResourceID < out[j].ResourceID
		}
		return out[i].ChangeType < out[j].ChangeType
	})
	return out
}

func normaliseChange(s string) ChangeType {
	for _, ct := range []ChangeType{ChangeCreate, ChangeModify, ChangeDelete, ChangeDeploy, ChangeIgnore, ChangeNoChange, ChangeUnsupported} {
		if strings.EqualFold(string(ct), s) {
			return ct
		}
	}
	return ChangeUnknown
}
