package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// BicepAdapterName is Finding.Adapter for compiler and linter diagnostics.
const BicepAdapterName = "bicep"

// bicepRulePrefix prefixes the compiler's own code to form the finding rule ID
// (ADR-009 decision 1), for example bicep/BCP035.
const bicepRulePrefix = "bicep/"

// bicepRuleVersion versions the diagnostic-to-finding mapping, not the compiler.
const bicepRuleVersion = 1

// bicepFindings converts compiler diagnostics into adapter findings (ADR-009).
// They carry no catalogue entry: no category, basis, pillar or profile. The
// severity is the compiler's. Evidence is the redacted compiler message.
//
// Finding.Key is "<code>:<message hash>" plus an occurrence counter, so two
// diagnostics with the same code in one file differ, while an edit that only
// moves a diagnostic to another line keeps its fingerprint (and baseline entry).
// entry is the repo-relative compiled file, used when the compiler gives no path.
func bicepFindings(ds []bicep.Diagnostic, entry string) []sdk.Finding {
	out := make([]sdk.Finding, 0, len(ds))
	occurrence := map[string]int{}
	// Diagnostics arrive sorted by file, line, column, so the occurrence number
	// is stable for an unchanged file.
	for _, d := range ds {
		file := d.File
		if file == "" {
			file = entry
		}
		msg := findings.Redact(d.Message)
		sum := sha256.Sum256([]byte(msg))
		key := d.Code + ":" + hex.EncodeToString(sum[:])[:12]
		occurrence[file+"\x00"+key]++
		if n := occurrence[file+"\x00"+key]; n > 1 {
			key = fmt.Sprintf("%s:%d", key, n)
		}
		f := sdk.Finding{
			RuleID:      bicepRulePrefix + d.Code,
			RuleVersion: bicepRuleVersion,
			Severity:    bicepSeverity(d.Severity),
			Resource:    sdk.ResourceRef{Type: "bicep-source", Name: file},
			Key:         key,
			Location:    sdk.Location{File: file, Line: d.Line, Column: d.Column},
			Evidence:    msg,
			DocsURL:     d.DocsURL,
			Confidence:  sdk.ConfidenceHigh,
			Adapter:     BicepAdapterName,
		}
		f.Recommendation = "Resolve the Bicep " + string(d.Severity) + " " + d.Code + " reported by the Bicep compiler."
		f.Fingerprint = findings.Fingerprint(f)
		out = append(out, f)
	}
	return out
}

func bicepSeverity(s bicep.Severity) sdk.Severity {
	switch s {
	case bicep.SeverityError:
		return sdk.SeverityError
	case bicep.SeverityInfo:
		return sdk.SeverityInfo
	}
	return sdk.SeverityWarning
}

// adapterFindingsSelected reports whether adapter findings belong in a run
// with these selectors and exclusions. They have no category, group or pillar,
// so a selector that names a rule, group or pillar leaves them out; the default
// category selectors (must-have, nice-to-have) and no selector keep them.
// An exclusion removes a finding whose rule ID equals or globs the entry.
func adapterFindingsSelected(selectors []string) bool {
	for _, s := range selectors {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(categories, s) {
			return false
		}
	}
	return true
}

func filterAdapterFindings(fs []sdk.Finding, selectors, exclude []string) []sdk.Finding {
	if !adapterFindingsSelected(selectors) {
		return nil
	}
	var out []sdk.Finding
next:
	for _, f := range fs {
		for _, x := range exclude {
			if ok, _ := path.Match(x, f.RuleID); ok || strings.EqualFold(x, f.RuleID) {
				continue next
			}
		}
		out = append(out, f)
	}
	return out
}

// adapterStore carries adapter findings from the ARM loader to the engine
// without changing the loader or engine interfaces. The loader stores the
// findings of the entry compile under the project directory; the engine takes
// them once. A zero store is ready to use.
type adapterStore struct {
	mu sync.Mutex
	m  map[string][]sdk.Finding
}

func (s *adapterStore) put(dir string, fs []sdk.Finding) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]sdk.Finding{}
	}
	if len(fs) == 0 {
		delete(s.m, dir)
		return
	}
	s.m[dir] = fs
}

func (s *adapterStore) take(dir string) []sdk.Finding {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fs := s.m[dir]
	delete(s.m, dir)
	return fs
}
