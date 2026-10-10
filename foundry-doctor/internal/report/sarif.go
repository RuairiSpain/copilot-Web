package report

import (
	"encoding/json"
	"io"
	"net/url"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// SARIF constants.
const (
	SARIFVersion = "2.1.0"
	SARIFSchema  = "https://json.schemastore.org/sarif-2.1.0.json"
	// FingerprintKey is the partialFingerprints key for finding fingerprints.
	FingerprintKey = "foundryDoctorFingerprint/v1"
)

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool         `json:"tool"`
	Invocations []sarifInvocation `json:"invocations"`
	Results     []sarifResult     `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version,omitempty"`
	Rules   []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID         string      `json:"id"`
	HelpURI    string      `json:"helpUri,omitempty"`
	Properties *sarifRuleP `json:"properties,omitempty"`
}

type sarifRuleP struct {
	Pillar string   `json:"pillar,omitempty"`
	Basis  []string `json:"basis,omitempty"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifInvocation struct {
	ExecutionSuccessful        bool                `json:"executionSuccessful"`
	ExitCode                   int                 `json:"exitCode"`
	StartTimeUTC               string              `json:"startTimeUtc,omitempty"`
	ToolExecutionNotifications []sarifNotification `json:"toolExecutionNotifications"`
	Properties                 map[string]any      `json:"properties,omitempty"`
}

type sarifNotification struct {
	Level      string         `json:"level"`
	Message    sarifText      `json:"message"`
	Descriptor sarifDescRef   `json:"descriptor"`
	Properties map[string]any `json:"properties,omitempty"`
}

type sarifDescRef struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	BaselineState       string            `json:"baselineState"`
	Suppressions        []sarifSuppr      `json:"suppressions,omitempty"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

type sarifSuppr struct {
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Justification string `json:"justification,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

func sarifLevel(s sdk.Severity) string {
	switch s {
	case sdk.SeverityError:
		return "error"
	case sdk.SeverityWarning:
		return "warning"
	}
	return "note"
}

// artifactURI percent-encodes each path segment of a sanitised path.
func artifactURI(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// SARIF writes a SARIF 2.1.0 log. Findings become results (suppressed ones
// carry suppressions, baselined ones baselineState "unchanged"); skipped
// checks become tool execution notifications.
func SARIF(w io.Writer, fs []sdk.Finding, run Run) error {
	p := prepared(fs)
	skips := preparedSkips(run.Skipped)

	ruleMeta := map[string]sarifRule{}
	for _, f := range p {
		id := SafeRuleID(f.RuleID)
		r, ok := ruleMeta[id]
		if !ok {
			r = sarifRule{ID: id}
		}
		if f.DocsURL != "" && r.HelpURI == "" {
			if u, err := url.Parse(f.DocsURL); err == nil && u.Scheme == "https" && u.Host != "" {
				r.HelpURI = u.String()
			}
		}
		if r.Properties == nil && (f.Pillar != "" || len(f.Basis) > 0) {
			r.Properties = &sarifRuleP{Pillar: cleanText(f.Pillar), Basis: f.Basis}
		}
		ruleMeta[id] = r
	}
	for _, s := range skips {
		id := SafeRuleID(s.RuleID)
		if _, ok := ruleMeta[id]; !ok {
			ruleMeta[id] = sarifRule{ID: id}
		}
	}
	ids := make([]string, 0, len(ruleMeta))
	for id := range ruleMeta {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	index := map[string]int{}
	rules := make([]sarifRule, len(ids))
	for i, id := range ids {
		index[id] = i
		rules[i] = ruleMeta[id]
	}

	results := make([]sarifResult, 0, len(p))
	for _, f := range p {
		id := SafeRuleID(f.RuleID)
		msg := cleanText(f.Evidence)
		if msg == "" {
			msg = cleanText(f.Recommendation)
		}
		if msg == "" {
			msg = "Finding " + id
		}
		res := sarifResult{
			RuleID:              id,
			RuleIndex:           index[id],
			Level:               sarifLevel(f.Severity),
			Message:             sarifText{Text: msg},
			PartialFingerprints: map[string]string{FingerprintKey: cleanText(f.Fingerprint)},
			BaselineState:       "new",
		}
		if f.Baselined {
			res.BaselineState = "unchanged"
		}
		if f.Suppressed != nil {
			res.Suppressions = []sarifSuppr{{Kind: "external", Status: "accepted", Justification: cleanText(f.Suppressed.Reason)}}
		}
		if f.Location.File != "" {
			pl := sarifPhysical{ArtifactLocation: sarifArtifact{URI: artifactURI(f.Location.File), URIBaseID: "%SRCROOT%"}}
			if f.Location.Line > 0 {
				pl.Region = &sarifRegion{StartLine: f.Location.Line}
				if f.Location.Column > 0 {
					pl.Region.StartColumn = f.Location.Column
				}
			}
			res.Locations = []sarifLocation{{PhysicalLocation: pl}}
		}
		props := map[string]any{}
		if f.Recommendation != "" {
			props["recommendation"] = cleanText(f.Recommendation)
		}
		if f.Fix != "" {
			props["fix"] = cleanText(f.Fix)
		}
		if f.Confidence != "" {
			props["confidence"] = string(f.Confidence)
		}
		if f.Resource.Type != "" || f.Resource.Name != "" {
			props["resource"] = cleanText(f.Resource.Type + " " + f.Resource.Name)
		}
		if f.Adapter != "" {
			props["adapter"] = cleanText(f.Adapter)
		}
		if len(props) > 0 {
			res.Properties = props
		}
		results = append(results, res)
	}

	notes := make([]sarifNotification, 0, len(skips))
	for _, s := range skips {
		lvl := "note"
		if s.Required {
			lvl = "warning"
		}
		notes = append(notes, sarifNotification{
			Level:      lvl,
			Message:    sarifText{Text: "Check skipped (not passed): " + cleanText(s.Reason)},
			Descriptor: sarifDescRef{ID: SafeRuleID(s.RuleID)},
			Properties: map[string]any{"skipped": true, "required": s.Required},
		})
	}

	log := sarifLog{
		Schema:  SARIFSchema,
		Version: SARIFVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{Name: ToolName, Version: findings.Redact(cleanText(run.ToolVersion)), Rules: rules}},
			Invocations: []sarifInvocation{{
				ExecutionSuccessful:        run.ExitCode == ExitOK || run.ExitCode == ExitFindings || run.ExitCode == ExitStrictSkipped,
				ExitCode:                   run.ExitCode,
				StartTimeUTC:               run.GeneratedAt,
				ToolExecutionNotifications: notes,
				Properties:                 readinessProps(run.Readiness),
			}},
			Results: results,
		}},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
