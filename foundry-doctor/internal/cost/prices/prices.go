package prices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DefaultBaseURL = "https://prices.azure.com/api/retail/prices"

// Query identifies one public retail lookup.
type Query struct {
	ServiceName   string `json:"serviceName"`
	ProductName   string `json:"productName,omitempty"`
	Region        string `json:"region"`
	CurrencyCode  string `json:"currencyCode,omitempty"`
	MeterName     string `json:"meterName"`
	RequireHourly bool   `json:"requireHourly,omitempty"`
}

func (q Query) key() string {
	return strings.Join([]string{
		q.ServiceName,
		q.ProductName,
		strings.ToLower(q.Region),
		strings.ToUpper(q.CurrencyCode),
		q.MeterName,
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
	UnitOfMeasure      string    `json:"unitOfMeasure"`
	UnitPrice          float64   `json:"unitPrice"`
	EffectiveStartDate string    `json:"effectiveStartDate"`
	RetrievedAt        time.Time `json:"retrievedAt"`
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
	CurrencyCode       string  `json:"currencyCode"`
	UnitPrice          float64 `json:"unitPrice"`
	ArmRegionName      string  `json:"armRegionName"`
	ProductName        string  `json:"productName"`
	SKUName            string  `json:"skuName"`
	ArmSKUName         string  `json:"armSkuName"`
	MeterName          string  `json:"meterName"`
	ServiceName        string  `json:"serviceName"`
	UnitOfMeasure      string  `json:"unitOfMeasure"`
	EffectiveStartDate string  `json:"effectiveStartDate"`
	Type               string  `json:"type"`
}

func (c Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Lookup returns the cheapest matching hourly retail price for q.
func (c Client) Lookup(ctx context.Context, q Query) (Record, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return Record{}, err
	}
	filters := []string{
		fmt.Sprintf("serviceName eq '%s'", escapeFilter(q.ServiceName)),
		fmt.Sprintf("armRegionName eq '%s'", escapeFilter(q.Region)),
		fmt.Sprintf("meterName eq '%s'", escapeFilter(q.MeterName)),
	}
	if q.ProductName != "" {
		filters = append(filters, fmt.Sprintf("productName eq '%s'", escapeFilter(q.ProductName)))
	}
	values := u.Query()
	values.Set("$filter", strings.Join(filters, " and "))
	if q.CurrencyCode != "" {
		values.Set("currencyCode", strings.ToUpper(q.CurrencyCode))
	}
	u.RawQuery = values.Encode()

	var (
		best  *retailItem
		next  = u.String()
		httpc = c.httpClient()
	)
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return Record{}, err
		}
		resp, err := httpc.Do(req)
		if err != nil {
			return Record{}, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if err != nil {
			return Record{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return Record{}, fmt.Errorf("retail prices API: %s", resp.Status)
		}
		var page retailPage
		if err := json.Unmarshal(body, &page); err != nil {
			return Record{}, err
		}
		for _, item := range page.Items {
			if !strings.EqualFold(item.ServiceName, q.ServiceName) || !strings.EqualFold(item.ArmRegionName, q.Region) || item.MeterName != q.MeterName {
				continue
			}
			if q.ProductName != "" && !strings.EqualFold(item.ProductName, q.ProductName) {
				continue
			}
			if q.RequireHourly && !strings.Contains(strings.ToLower(item.UnitOfMeasure), "hour") {
				continue
			}
			if best == nil || item.UnitPrice < best.UnitPrice {
				cpy := item
				best = &cpy
			}
		}
		next = page.NextPageLink
	}
	if best == nil {
		return Record{}, fsErrNotExist(q)
	}
	return Record{
		Query:              q,
		CurrencyCode:       best.CurrencyCode,
		Region:             best.ArmRegionName,
		ProductName:        best.ProductName,
		SKUName:            best.SKUName,
		ArmSKUName:         best.ArmSKUName,
		MeterName:          best.MeterName,
		UnitOfMeasure:      best.UnitOfMeasure,
		UnitPrice:          best.UnitPrice,
		EffectiveStartDate: best.EffectiveStartDate,
		RetrievedAt:        c.now(),
	}, nil
}

func escapeFilter(v string) string { return strings.ReplaceAll(v, "'", "''") }

func fsErrNotExist(q Query) error {
	return fmt.Errorf("%w: no retail price for %s in %s (%s)", os.ErrNotExist, q.MeterName, q.Region, q.ServiceName)
}

// LoadCache reads a cache file. Missing files return (Cache{}, os.ErrNotExist).
func LoadCache(path string) (Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Cache{}, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, err
	}
	if c.Version == 0 {
		c.Version = 1
	}
	return c, nil
}

// SaveCache writes c atomically enough for CLI use.
func SaveCache(path string, c Cache) error {
	c.Version = 1
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Lookup returns the cached record for q.
func (c Cache) Lookup(q Query) (Record, bool) {
	key := q.key()
	for _, r := range c.Records {
		if r.Query.key() == key {
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

// Stale reports whether r is older than maxAge.
func Stale(r Record, now time.Time, maxAge time.Duration) bool {
	if r.RetrievedAt.IsZero() {
		return true
	}
	return now.Sub(r.RetrievedAt) > maxAge
}

// Resolve returns a cached price, optionally refreshing it live first.
func Resolve(ctx context.Context, c Client, cachePath string, offline bool, q Query) (Record, bool, error) {
	cache, err := LoadCache(cachePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Record{}, false, err
	}
	if !offline {
		rec, err := c.Lookup(ctx, q)
		if err == nil {
			cache.Upsert(rec)
			if cachePath != "" {
				if saveErr := SaveCache(cachePath, cache); saveErr != nil {
					return rec, false, saveErr
				}
			}
			return rec, false, nil
		}
		if !errors.Is(err, os.ErrNotExist) && cachePath == "" {
			return Record{}, false, err
		}
	}
	if rec, ok := cache.Lookup(q); ok {
		return rec, true, nil
	}
	if offline {
		return Record{}, false, fmt.Errorf("%w: cached retail price missing", os.ErrNotExist)
	}
	return Record{}, false, fmt.Errorf("%w: retail price unavailable and no cached fallback", os.ErrNotExist)
}
