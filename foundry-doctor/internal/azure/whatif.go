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

type whatIfBody struct {
	Status     string `json:"status"`
	Properties struct {
		Changes []struct {
			ResourceID string `json:"resourceId"`
			ChangeType string `json:"changeType"`
		} `json:"changes"`
		Error *armErrorBody `json:"error"`
	} `json:"properties"`
	Error *armErrorBody `json:"error"`
}

// Run implements WhatIf. It is the only ARM deployment-scoped call and never
// creates, updates or deletes a resource. Request bodies carry the template and
// parameters; response before/after payloads are not requested
// (resultFormat=ResourceIdOnly) and are never retained.
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
	res := WhatIfResult{DiagnosticCodes: sortedUnique(codes), Changes: mergeChanges(wb)}
	return res, nil
}

func mergeChanges(wb whatIfBody) []WhatIfChange {
	types := map[string]map[ChangeType]bool{}
	for _, c := range wb.Properties.Changes {
		ct := normaliseChange(c.ChangeType)
		if types[c.ResourceID] == nil {
			types[c.ResourceID] = map[ChangeType]bool{}
		}
		types[c.ResourceID][ct] = true
	}
	out := []WhatIfChange{}
	for id, set := range types {
		if set[ChangeDelete] && set[ChangeCreate] {
			delete(set, ChangeDelete)
			delete(set, ChangeCreate)
			out = append(out, WhatIfChange{ResourceID: id, ChangeType: ChangeReplace})
		}
		for ct := range set {
			out = append(out, WhatIfChange{ResourceID: id, ChangeType: ct})
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
	for _, ct := range []ChangeType{ChangeCreate, ChangeModify, ChangeDelete, ChangeDeploy, ChangeIgnore, ChangeNoChange} {
		if strings.EqualFold(string(ct), s) {
			return ct
		}
	}
	return ChangeUnknown
}
