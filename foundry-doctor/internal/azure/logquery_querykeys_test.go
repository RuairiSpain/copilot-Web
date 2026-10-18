package azure

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// The Log Analytics query takes its query in the body and its version in the
// path. A query-string key on it (even one the default set allows elsewhere)
// is refused before anything is sent.
func TestLogAnalyticsQueryRejectsEveryQueryStringKey(t *testing.T) {
	h := newHostRT(t)
	a, _ := newRuntimeAdapter(t, h, &scopeCred{}, time.Now())
	const path = "/v1/workspaces/44444444-4444-4444-4444-444444444444/query"
	for _, k := range []string{"$filter", "$top", "$skiptoken", "timespan", "query", "api-version", "metricnames"} {
		t.Run(k, func(t *testing.T) {
			_, err := a.c.do(context.Background(), http.MethodPost, path, url.Values{k: {"x"}}, logQueryBody{})
			if !errors.Is(err, ErrNotAllowed) {
				t.Fatalf("query key %q: err = %v, want ErrNotAllowed", k, err)
			}
		})
	}
	if h.count() != 0 {
		t.Fatalf("a refused query reached the network %d time(s)", h.count())
	}
	for _, o := range AllowedOperations() {
		if o.Name == "runtime.loganalytics.query" && (o.QueryKeys == nil || len(o.QueryKeys) != 0) {
			t.Errorf("runtime.loganalytics.query must declare an empty, non-nil key set: %#v", o.QueryKeys)
		}
	}
}
