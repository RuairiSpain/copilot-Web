// Package prices reads public retail prices from the Azure Retail Prices API
// (https://learn.microsoft.com/rest/api/cost-management/retail-prices/azure-retail-prices,
// verified 2026-10-18) and caches them locally.
//
// The client never guesses: only pay-as-you-go ("Consumption") rows in the
// requested currency are candidates, primary meter regions are preferred,
// tiered rows are dropped, and more than one remaining price is reported as
// ambiguous instead of choosing one.
package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the documented unauthenticated endpoint.
	DefaultBaseURL = "https://prices.azure.com/api/retail/prices"
	// APIVersion is pinned so filter values are case sensitive and stable
	// ("in the 2023-01-01-preview version and later, the value is case
	// sensitive").
	APIVersion = "2023-01-01-preview"
	// PriceType restricts results to pay-as-you-go rows; DevTestConsumption
	// and Reservation rows (a Reservation unitPrice is the term price even
	// when unitOfMeasure is "1 Hour") are never candidates.
	PriceType = "Consumption"
	// MaxPages caps NextPageLink following (the API returns at most 1,000
	// records per page).
	MaxPages = 20
	// RetailHost is the only host a NextPageLink may point to in production.
	RetailHost = "prices.azure.com"

	cacheVersion = 2
	maxBodyBytes = 4 << 20
)

var (
	// ErrAmbiguous means more than one distinct pay-as-you-go price matched.
	ErrAmbiguous = errors.New("ambiguous retail meter")
	// ErrCurrencyMismatch means the API answered in a currency other than
	// the requested one.
	ErrCurrencyMismatch = errors.New("retail price currency mismatch")
	// ErrPaging means a NextPageLink was refused or the page cap was hit.
	ErrPaging = errors.New("retail price paging refused")
)

// Query identifies one public retail lookup.
type Query struct {
	ServiceName   string `json:"serviceName"`
	ProductName   string `json:"productName,omitempty"`
	Region        string `json:"region"`
	CurrencyCode  string `json:"currencyCode,omitempty"`
	MeterName     string `json:"meterName"`
	RequireHourly bool   `json:"requireHourly,omitempty"`
}

func (q Query) currency() string {
	c := strings.ToUpper(strings.TrimSpace(q.CurrencyCode))
	if c == "" {
		return "USD"
	}
	return c
}

func (q Query) key() string {
	return strings.Join([]string{
		APIVersion,
		PriceType,
		q.ServiceName,
		q.ProductName,
		strings.ToLower(q.Region),
		q.currency(),
		q.MeterName,
		strconv.FormatBool(q.RequireHourly),
	}, "|")
}

// Record is one cached or live retail price.
type Record struct {
	Query              Query     `json:"query"`
	CurrencyCode       string    `json:"currencyCode"`
	Region             string    `json:"region"`
	ProductName        string    `json:"productName"`
	SKUName            string    `json:"skuName"`
	ArmSKUName         string    `json:"armSkuName,omitempty"`
	MeterName          string    `json:"meterName"`
	MeterID            string    `json:"meterId,omitempty"`
	UnitOfMeasure      string    `json:"unitOfMeasure"`
	UnitPrice          float64   `json:"unitPrice"`
	Type               string    `json:"type"`
	PrimaryMeterRegion bool      `json:"isPrimaryMeterRegion"`
	EffectiveStartDate string    `json:"effectiveStartDate"`
	RetrievedAt        time.Time `json:"retrievedAt"`
	// Notice explains a non-fatal condition of this resolution (cache not
	// written, cached fallback after a live failure). It is not cached.
	Notice string `json:"-"`
}

// Cache is the on-disk price cache schema.
type Cache struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

// Client queries the public Azure Retail Prices API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Now     func() time.Time
}

type retailPage struct {
	Items        []retailItem `json:"Items"`
	NextPageLink string       `json:"NextPageLink"`
}

type retailItem struct {
	CurrencyCode         string  `json:"currencyCode"`
	TierMinimumUnits     float64 `json:"tierMinimumUnits"`
	UnitPrice            float64 `json:"unitPrice"`
	ArmRegionName        string  `json:"armRegionName"`
	ProductName          string  `json:"productName"`
	SKUName              string  `json:"skuName"`
	ArmSKUName           string  `json:"armSkuName"`
	MeterID              string  `json:"meterId"`
	MeterName            string  `json:"meterName"`
	ServiceName          string  `json:"serviceName"`
	UnitOfMeasure        string  `json:"unitOfMeasure"`
	EffectiveStartDate   string  `json:"effectiveStartDate"`
	EffectiveEndDate     string  `json:"effectiveEndDate"`
	Type                 string  `json:"type"`
	IsPrimaryMeterRegion bool    `json:"isPrimaryMeterRegion"`
}

func (c Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

// httpClient returns a copy of the configured client that never follows
// redirects: a 3xx answer is an error, not a hop to another origin.
func (c Client) httpClient() *http.Client {
	var hc http.Client
	if c.HTTP != nil {
		hc = *c.HTTP
	} else {
		hc = http.Client{Timeout: 20 * time.Second}
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &hc
}

// RequestURL builds the first-page URL for q against base.
func RequestURL(base string, q Query) (*url.URL, error) {
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	filters := []string{
		fmt.Sprintf("serviceName eq '%s'", escapeFilter(q.ServiceName)),
		fmt.Sprintf("armRegionName eq '%s'", escapeFilter(q.Region)),
		fmt.Sprintf("meterName eq '%s'", escapeFilter(q.MeterName)),
		fmt.Sprintf("priceType eq '%s'", PriceType),
	}
	if q.ProductName != "" {
		filters = append(filters, fmt.Sprintf("productName eq '%s'", escapeFilter(q.ProductName)))
	}
	values := u.Query()
	values.Set("api-version", APIVersion)
	values.Set("$filter", strings.Join(filters, " and "))
	// Documented request form: currencyCode='EUR' (quoted).
	values.Set("currencyCode", "'"+q.currency()+"'")
	u.RawQuery = values.Encode()
	return u, nil
}

// nextAllowed accepts a NextPageLink only on the origin of the first request
// (https://prices.azure.com in production; port 443 may be explicit).
func nextAllowed(first *url.URL, next string) (string, error) {
	n, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("%w: invalid NextPageLink: %v", ErrPaging, err)
	}
	if !strings.EqualFold(n.Scheme, first.Scheme) || !strings.EqualFold(n.Hostname(), first.Hostname()) {
		return "", fmt.Errorf("%w: NextPageLink origin %s://%s is not %s://%s", ErrPaging, n.Scheme, n.Hostname(), first.Scheme, first.Hostname())
	}
	if !samePort(first, n) {
		return "", fmt.Errorf("%w: NextPageLink port %q differs from the request", ErrPaging, n.Port())
	}
	if first.Port() == "" && strings.EqualFold(first.Scheme, "https") && !strings.EqualFold(first.Hostname(), RetailHost) {
		return "", fmt.Errorf("%w: unexpected retail host %s", ErrPaging, first.Hostname())
	}
	return n.String(), nil
}

func samePort(a, b *url.URL) bool {
	pa, pb := a.Port(), b.Port()
	if pa == pb {
		return true
	}
	def := map[string]string{"https": "443", "http": "80"}[strings.ToLower(a.Scheme)]
	return (pa == "" && pb == def) || (pb == "" && pa == def)
}

// Lookup returns the single pay-as-you-go retail price for q.
func (c Client) Lookup(ctx context.Context, q Query) (Record, error) {
	first, err := RequestURL(c.BaseURL, q)
	if err != nil {
		return Record{}, err
	}
	now := c.now()
	var (
		cands      []retailItem
		mismatched string
		future     int
		next       = first.String()
		httpc      = c.httpClient()
	)
	for pages := 0; next != ""; pages++ {
		if pages >= MaxPages {
			return Record{}, fmt.Errorf("%w: more than %d pages", ErrPaging, MaxPages)
		}
		page, err := fetch(ctx, httpc, next)
		if err != nil {
			return Record{}, err
		}
		for _, item := range page.Items {
			if !strings.EqualFold(item.ServiceName, q.ServiceName) || !strings.EqualFold(item.ArmRegionName, q.Region) || item.MeterName != q.MeterName {
				continue
			}
			if q.ProductName != "" && !strings.EqualFold(item.ProductName, q.ProductName) {
				continue
			}
			if !strings.EqualFold(item.Type, PriceType) {
				continue
			}
			if q.RequireHourly && !hourlyUnit(item.UnitOfMeasure) {
				continue
			}
			if item.TierMinimumUnits != 0 {
				continue
			}
			if ended(item.EffectiveEndDate, now) {
				continue
			}
			if notYet(item.EffectiveStartDate, now) {
				future++
				continue
			}
			if !strings.EqualFold(item.CurrencyCode, q.currency()) {
				mismatched = item.CurrencyCode
				continue
			}
			cands = append(cands, item)
		}
		if page.NextPageLink == "" {
			break
		}
		if next, err = nextAllowed(first, page.NextPageLink); err != nil {
			return Record{}, err
		}
	}
	if len(cands) == 0 {
		if mismatched != "" {
			return Record{}, fmt.Errorf("%w: requested %s, API returned %s for %s", ErrCurrencyMismatch, q.currency(), mismatched, q.MeterName)
		}
		if future > 0 {
			return Record{}, fmt.Errorf("%w: retail price for %s in %s is not effective yet", os.ErrNotExist, q.MeterName, q.Region)
		}
		return Record{}, fsErrNotExist(q)
	}
	var primary []retailItem
	for _, it := range cands {
		if it.IsPrimaryMeterRegion {
			primary = append(primary, it)
		}
	}
	if len(primary) > 0 {
		cands = primary
	}
	distinct := map[float64]bool{}
	for _, it := range cands {
		distinct[it.UnitPrice] = true
	}
	if len(distinct) > 1 {
		var ps []string
		for p := range distinct {
			ps = append(ps, strconv.FormatFloat(p, 'f', -1, 64))
		}
		sort.Strings(ps)
		return Record{}, fmt.Errorf("%w: %d distinct Consumption prices for %s in %s (%s)", ErrAmbiguous, len(distinct), q.MeterName, q.Region, strings.Join(ps, ", "))
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].MeterID < cands[j].MeterID })
	best := cands[0]
	return Record{
		Query:              q,
		CurrencyCode:       best.CurrencyCode,
		Region:             best.ArmRegionName,
		ProductName:        best.ProductName,
		SKUName:            best.SKUName,
		ArmSKUName:         best.ArmSKUName,
		MeterName:          best.MeterName,
		MeterID:            best.MeterID,
		UnitOfMeasure:      best.UnitOfMeasure,
		UnitPrice:          best.UnitPrice,
		Type:               best.Type,
		PrimaryMeterRegion: best.IsPrimaryMeterRegion,
		EffectiveStartDate: best.EffectiveStartDate,
		RetrievedAt:        now,
	}, nil
}

func fetch(ctx context.Context, httpc *http.Client, target string) (retailPage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return retailPage{}, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return retailPage{}, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	_ = resp.Body.Close()
	if err != nil {
		return retailPage{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return retailPage{}, fmt.Errorf("retail prices API: %s", resp.Status)
	}
	var page retailPage
	if err := json.Unmarshal(body, &page); err != nil {
		return retailPage{}, err
	}
	return page, nil
}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

func ended(end string, now time.Time) bool {
	t, ok := parseTime(end)
	return ok && t.Before(now)
}

func notYet(start string, now time.Time) bool {
	t, ok := parseTime(start)
	return ok && t.After(now)
}

func escapeFilter(v string) string { return strings.ReplaceAll(v, "'", "''") }

func fsErrNotExist(q Query) error {
	return fmt.Errorf("%w: no Consumption retail price for %s in %s (%s)", os.ErrNotExist, q.MeterName, q.Region, q.ServiceName)
}

// LoadCache reads a cache file. Missing files return (Cache{}, os.ErrNotExist).
// A cache written with an older schema is ignored (its keys did not include
// the price type and API version).
func LoadCache(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, err
	}
	if c.Version != cacheVersion {
		return Cache{Version: cacheVersion}, nil
	}
	return c, nil
}

// SaveCache writes c atomically enough for CLI use.
func SaveCache(path string, c Cache) error {
	c.Version = cacheVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Lookup returns the cached record for q. A record in another currency is
// never returned.
func (c Cache) Lookup(q Query) (Record, bool) {
	key := q.key()
	for _, r := range c.Records {
		if r.Query.key() == key && strings.EqualFold(r.CurrencyCode, q.currency()) {
			return r, true
		}
	}
	return Record{}, false
}

// Upsert replaces or appends a record.
func (c *Cache) Upsert(r Record) {
	key := r.Query.key()
	for i := range c.Records {
		if c.Records[i].Query.key() == key {
			c.Records[i] = r
			return
		}
	}
	c.Records = append(c.Records, r)
}

// hourlyUnit reports whether unit is exactly one hour ("1 Hour" or "1/Hour").
// A unit that merely contains "hour" ("100 Hours", "1 Hour per 100") is not
// priced as one hour: the quantity would be scaled wrongly without a sign.
func hourlyUnit(unit string) bool {
	u := strings.ToLower(strings.Join(strings.Fields(unit), ""))
	return u == "1hour" || u == "1/hour"
}

// clockSkew is how far a cached retrieval date may lie in the future.
const clockSkew = 24 * time.Hour

// Valid checks a record before it is trusted. The cache is a plain file in the
// project directory, so a record in it is untrusted input: its price must be
// finite and not negative, its currency and unit must match the query, and it
// must not claim to be retrieved in the future (which would never go stale).
func (r Record) Valid(q Query, now time.Time) error {
	switch {
	case math.IsNaN(r.UnitPrice) || math.IsInf(r.UnitPrice, 0) || r.UnitPrice < 0:
		return fmt.Errorf("unit price %v is not a finite non-negative number", r.UnitPrice)
	case !strings.EqualFold(r.CurrencyCode, q.currency()):
		return fmt.Errorf("currency %q does not match the requested %q", r.CurrencyCode, q.currency())
	case q.RequireHourly && !hourlyUnit(r.UnitOfMeasure):
		return fmt.Errorf("unit of measure %q is not one hour", r.UnitOfMeasure)
	case r.Type != "" && !strings.EqualFold(r.Type, PriceType):
		return fmt.Errorf("price type %q is not %s", r.Type, PriceType)
	case r.RetrievedAt.After(now.Add(clockSkew)):
		return fmt.Errorf("retrieval date %s is in the future", r.RetrievedAt.Format(time.RFC3339))
	}
	return nil
}

// Stale reports whether r is older than maxAge.
func Stale(r Record, now time.Time, maxAge time.Duration) bool {
	if r.RetrievedAt.IsZero() {
		return true
	}
	return now.Sub(r.RetrievedAt) > maxAge
}

// Resolve returns a live price (refreshing the cache) or, offline or after a
// live failure, the cached price. usedCache reports the cached path. An
// ambiguous or wrong-currency live answer is returned as an error, never
// replaced by a cached value.
func Resolve(ctx context.Context, c Client, cachePath string, offline bool, q Query) (rec Record, usedCache bool, err error) {
	cache := Cache{Version: cacheVersion}
	var cacheErr error
	if cachePath != "" {
		loaded, err := LoadCache(cachePath)
		switch {
		case err == nil:
			cache = loaded
		case errors.Is(err, os.ErrNotExist):
		default:
			cacheErr = err
		}
	}
	var liveErr error
	if !offline {
		rec, liveErr = c.Lookup(ctx, q)
		if liveErr == nil {
			if cachePath != "" {
				cache.Upsert(rec)
				if saveErr := SaveCache(cachePath, cache); saveErr != nil {
					rec.Notice = "price cache not written: " + saveErr.Error()
				}
			}
			return rec, false, nil
		}
		if errors.Is(liveErr, ErrAmbiguous) || errors.Is(liveErr, ErrCurrencyMismatch) || errors.Is(liveErr, ErrPaging) {
			return Record{}, false, liveErr
		}
	}
	if cacheErr != nil {
		if liveErr != nil {
			return Record{}, false, fmt.Errorf("%w; price cache unreadable: %v", liveErr, cacheErr)
		}
		return Record{}, false, fmt.Errorf("price cache unreadable: %w", cacheErr)
	}
	if cached, ok := cache.Lookup(q); ok {
		if verr := cached.Valid(q, c.now()); verr != nil {
			return Record{}, false, fmt.Errorf("cached retail price rejected (no price guessed): %w", verr)
		}
		switch {
		case liveErr == nil:
		case errors.Is(liveErr, os.ErrNotExist):
			cached.Notice = "live API no longer lists this meter; cached price shown"
		default:
			cached.Notice = "live lookup failed (" + liveErr.Error() + "); cached price shown"
		}
		return cached, true, nil
	}
	if offline {
		return Record{}, false, fmt.Errorf("%w: cached retail price missing", os.ErrNotExist)
	}
	return Record{}, false, fmt.Errorf("%w (no cached fallback)", liveErr)
}
