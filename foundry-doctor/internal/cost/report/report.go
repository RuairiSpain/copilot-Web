package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// LineItem is one priced resource line.
type LineItem struct {
	ResourceName string   `json:"resourceName"`
	ResourceType string   `json:"resourceType"`
	Kind         string   `json:"kind"`
	Region       string   `json:"region"`
	MeterName    string   `json:"meterName"`
	SKUName      string   `json:"skuName,omitempty"`
	Quantity     float64  `json:"quantity"`
	Unit         string   `json:"unit"`
	UnitPrice    float64  `json:"unitPrice"`
	MonthlyLow   float64  `json:"monthlyLow"`
	MonthlyHigh  float64  `json:"monthlyHigh"`
	PriceDate    string   `json:"priceDate"`
	Notes        []string `json:"notes,omitempty"`
}

// NoteItem is one excluded or unsupported resource.
type NoteItem struct {
	ResourceName string `json:"resourceName"`
	ResourceType string `json:"resourceType"`
	Reason       string `json:"reason"`
}

// EnvironmentReport is one environment's advisory estimate.
type EnvironmentReport struct {
	Environment    string     `json:"environment"`
	Profile        string     `json:"profile"`
	Currency       string     `json:"currency"`
	HoursPerMonth  float64    `json:"hoursPerMonth"`
	UsedCache      bool       `json:"usedCache"`
	StalePricing   bool       `json:"stalePricing"`
	PriceRetrieved string     `json:"priceRetrieved,omitempty"`
	Estimated      []LineItem `json:"estimated,omitempty"`
	Excluded       []NoteItem `json:"excluded,omitempty"`
	Unsupported    []NoteItem `json:"unsupported,omitempty"`
	Notes          []string   `json:"notes,omitempty"`
	TotalLow       float64    `json:"totalLow"`
	TotalHigh      float64    `json:"totalHigh"`
	Incomplete     bool       `json:"incomplete"`
}

// Comparison compares two environments.
type Comparison struct {
	Left       string  `json:"left"`
	Right      string  `json:"right"`
	DeltaLow   float64 `json:"deltaLow"`
	DeltaHigh  float64 `json:"deltaHigh"`
	Comparable bool    `json:"comparable"`
	Reason     string  `json:"reason,omitempty"`
}

// Document is the rendered cost output.
type Document struct {
	Advisory     string              `json:"advisory"`
	Environments []EnvironmentReport `json:"environments"`
	Comparisons  []Comparison        `json:"comparisons,omitempty"`
}

// Render prints doc in console, json, or markdown form.
func Render(w io.Writer, format string, doc Document) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "console":
		return console(w, doc)
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	case "markdown":
		return markdown(w, doc)
	default:
		return fmt.Errorf("invalid --format %q (want console, json or markdown)", format)
	}
}

func console(w io.Writer, doc Document) error {
	if _, err := fmt.Fprintln(w, "Foundry Doctor cost estimate (advisory only; not a bill)"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, doc.Advisory); err != nil {
		return err
	}
	for _, env := range doc.Environments {
		if _, err := fmt.Fprintf(w, "\n[%s] profile=%s currency=%s hours/month=%.0f\n", env.Environment, env.Profile, env.Currency, env.HoursPerMonth); err != nil {
			return err
		}
		if env.PriceRetrieved != "" {
			source := "live"
			if env.UsedCache {
				source = "cache"
			}
			if _, err := fmt.Fprintf(w, "pricing: %s retrieved %s\n", source, env.PriceRetrieved); err != nil {
				return err
			}
		}
		for _, n := range env.Notes {
			if _, err := fmt.Fprintf(w, "note: %s\n", n); err != nil {
				return err
			}
		}
		if len(env.Estimated) == 0 {
			if _, err := fmt.Fprintln(w, "estimated: none"); err != nil {
				return err
			}
		}
		for _, line := range env.Estimated {
			if _, err := fmt.Fprintf(w, "- %s (%s): %.2f %s x %.5f => %.2f/month [%s]\n",
				line.ResourceName, line.MeterName, line.Quantity, line.Unit, line.UnitPrice, line.MonthlyHigh, line.Region); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "total: %.2f to %.2f %s/month\n", env.TotalLow, env.TotalHigh, env.Currency); err != nil {
			return err
		}
		if len(env.Excluded) > 0 {
			if _, err := fmt.Fprintln(w, "excluded consumption-based resources:"); err != nil {
				return err
			}
			for _, x := range env.Excluded {
				if _, err := fmt.Fprintf(w, "  - %s (%s): %s\n", x.ResourceName, x.ResourceType, x.Reason); err != nil {
					return err
				}
			}
		}
		if len(env.Unsupported) > 0 {
			if _, err := fmt.Fprintln(w, "unsupported or unverified resources:"); err != nil {
				return err
			}
			for _, x := range env.Unsupported {
				if _, err := fmt.Fprintf(w, "  - %s (%s): %s\n", x.ResourceName, x.ResourceType, x.Reason); err != nil {
					return err
				}
			}
		}
	}
	for _, cmp := range doc.Comparisons {
		if cmp.Comparable {
			if _, err := fmt.Fprintf(w, "\ncompare %s -> %s: delta %.2f to %.2f\n", cmp.Left, cmp.Right, cmp.DeltaLow, cmp.DeltaHigh); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(w, "\ncompare %s -> %s: %s\n", cmp.Left, cmp.Right, cmp.Reason); err != nil {
			return err
		}
	}
	return nil
}

func markdown(w io.Writer, doc Document) error {
	if _, err := fmt.Fprintln(w, "# Foundry Doctor cost estimate"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s\n", doc.Advisory); err != nil {
		return err
	}
	for _, env := range doc.Environments {
		if _, err := fmt.Fprintf(w, "\n## %s\n\n", env.Environment); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "- Profile: `%s`\n- Currency: `%s`\n- Hours/month: `%.0f`\n- Total: `%.2f` to `%.2f` %s/month\n",
			env.Profile, env.Currency, env.HoursPerMonth, env.TotalLow, env.TotalHigh, env.Currency); err != nil {
			return err
		}
		for _, n := range env.Notes {
			if _, err := fmt.Fprintf(w, "- Note: %s\n", n); err != nil {
				return err
			}
		}
		if len(env.Estimated) > 0 {
			if _, err := fmt.Fprintln(w, "\n| Resource | Meter | Quantity | Unit price | Monthly |"); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(w, "|---|---|---:|---:|---:|"); err != nil {
				return err
			}
			for _, line := range env.Estimated {
				if _, err := fmt.Fprintf(w, "| %s | %s | %.2f %s | %.5f | %.2f |\n",
					line.ResourceName, line.MeterName, line.Quantity, line.Unit, line.UnitPrice, line.MonthlyHigh); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
