package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/azure"
)

const (
	maxResponseBytes = 8 << 20
	maxRetryAfter    = 30 * time.Second
)

// DoJSON performs a bounded read-only HTTP request with OAuth auth and 429/5xx
// retry semantics. It never returns response bodies in errors.
func DoJSON(ctx context.Context, hc *http.Client, cred azure.TokenCredential, scope, method, rawURL string, body any, out any) error {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
	}
	for attempt := 0; ; attempt++ {
		tok, err := cred.Token(ctx, scope)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "foundry-doctor")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := hc.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < 3 {
				if err := sleepRetry(ctx, retryAfter(http.Header{}, attempt)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("transport failure: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}
		if len(data) > maxResponseBytes {
			return fmt.Errorf("response exceeds %d bytes", maxResponseBytes)
		}
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out == nil || len(data) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode response: %w", err)
			}
			return nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 500 || resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504:
			if attempt < 3 {
				if err := sleepRetry(ctx, retryAfter(resp.Header, attempt)); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("http %d", resp.StatusCode)
		case resp.StatusCode == http.StatusForbidden:
			return &azure.UnavailableError{Capability: method + " " + safeURL(rawURL), Reason: "data-plane permission or network access unavailable"}
		case resp.StatusCode == http.StatusUnauthorized:
			return &azure.UnavailableError{Capability: method + " " + safeURL(rawURL), Reason: "credential rejected by service"}
		case resp.StatusCode == http.StatusNotFound:
			return fmt.Errorf("http 404")
		default:
			return fmt.Errorf("http %d", resp.StatusCode)
		}
	}
}

func retryAfter(h http.Header, attempt int) time.Duration {
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			if d := time.Duration(secs) * time.Second; d < maxRetryAfter {
				return d
			}
			return maxRetryAfter
		}
		if ts, err := http.ParseTime(v); err == nil {
			if d := time.Until(ts); d > 0 && d < maxRetryAfter {
				return d
			}
			return maxRetryAfter
		}
	}
	d := 500 * time.Millisecond << attempt
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}

func sleepRetry(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery = ""
	u.User = nil
	return u.String()
}
