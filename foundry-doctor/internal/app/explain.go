package app

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/bicep"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/markdown"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

func redactLine(s string) string {
	return strings.Join(strings.Fields(findings.Redact(s)), " ")
}

// doc is the format-neutral content of an explanation: ordered label/value pairs.
type doc struct {
	title  string
	fields [][2]string
}

func (d *doc) add(label, value string) {
	if strings.TrimSpace(value) != "" {
		d.fields = append(d.fields, [2]string{label, strings.TrimSpace(value)})
	}
}

// Explain prints the catalogue entry of one rule (PRD section 4: explain). Exit 2 for an unknown rule or format.
func Explain(ctx context.Context, opts ExplainOptions, stdout, stderr io.Writer) error {
	ee := explain(ctx, opts, stdout)
	if ee == nil {
		return nil
	}
	fmt.Fprintf(stderr, "error: %s\n", ee.Err)
	return ee
}

func explain(ctx context.Context, opts ExplainOptions, stdout io.Writer) *ExitError {
	format := opts.Format
	if format == "" {
		format = "console"
	}
	if format != "console" && format != "markdown" {
		return exitErr(sdk.ExitCannotRun, "--format %q is not supported by explain: use console or markdown", format)
	}
	cat := opts.Catalogue
	if cat == nil {
		var err error
		if cat, err = engine.DefaultCatalogue(ctx); err != nil {
			return exitErr(sdk.ExitInternal, "load the rule catalogue: %w", err)
		}
	}
	id := strings.TrimSpace(opts.RuleID)
	var d doc
	switch {
	case engine.IsSystemID(strings.ToUpper(id)):
		sd, ok := systemDoc(strings.ToUpper(id))
		if !ok {
			return exitErr(sdk.ExitCannotRun, "unknown rule %q: it is not a reserved engine ID", id)
		}
		d = sd
	case engine.IsBicepID(id):
		d = bicepDoc(id)
	default:
		e, ok := cat.Registry.Get(strings.ToUpper(id))
		if !ok {
			return exitErr(sdk.ExitCannotRun, "unknown rule %q: run `doctor` to see the IDs in a report, or read docs/rule-catalog.md", id)
		}
		d = ruleDoc(e)
	}
	var sb strings.Builder
	if format == "markdown" {
		renderMarkdown(&sb, d)
	} else {
		renderConsole(&sb, d)
	}
	if _, err := io.WriteString(stdout, sb.String()); err != nil {
		return exitErr(sdk.ExitCannotRun, "write to stdout: %w", err)
	}
	return nil
}

func systemDoc(id string) (doc, bool) {
	for _, s := range engine.SystemDiagnostics() {
		if s.ID == id {
			d := doc{title: id + "  engine diagnostic"}
			d.add("Meaning", s.Meaning)
			d.add("Severity", string(s.Severity)+" in every profile")
			d.add("Owner", "engine (ADR-009)")
			d.add("Baseline", "never baselined; fix the cause")
			return d, true
		}
	}
	return doc{}, false
}

func bicepDoc(id string) doc {
	code := strings.TrimPrefix(id, "bicep/")
	d := doc{title: id + "  Bicep compiler diagnostic"}
	d.add("Meaning", "The Bicep compiler or linter reported "+code+". The doctor passes the diagnostic through with the severity the compiler gave it.")
	d.add("Docs", bicep.DocsURL(code))
	d.add("Owner", "Bicep CLI (adapter)")
	return d
}

func ruleDoc(e rules.Entry) doc {
	m := e.Meta
	d := doc{title: m.ID + "  " + m.Title}
	d.add("Version", fmt.Sprint(m.Version))
	d.add("Status", string(m.Status))
	d.add("Group", m.Group)
	d.add("Category", m.Category)
	d.add("Pillar", m.Pillar)
	d.add("Basis", strings.Join(m.Basis, ", "))
	var sev []string
	for _, p := range config.ProfileNames() {
		s, err := rules.SeverityFor(m, p)
		if err != nil {
			sev = append(sev, p+"=unknown")
			continue
		}
		sev = append(sev, p+"="+string(s))
	}
	d.add("Severity", strings.Join(sev, ", "))
	d.add("Rationale", m.Description)
	d.add("Inputs", strings.Join(m.Inputs, ", "))
	d.add("Evidence", m.Evidence.Description)
	d.add("Evidence confidence", m.Evidence.Confidence)
	d.add("Remediation", m.Recommendation)
	d.add("Fix", m.Fix)
	if len(m.Sources) > 0 {
		d.add("Docs", m.Sources[0].URL)
	}
	last := ""
	for _, s := range m.Sources {
		last = max(last, s.LastVerified)
	}
	d.add("Last verified", last)
	owner := m.Implementation.Owner
	if m.Implementation.Package != "" {
		owner += " (" + m.Implementation.Package + ")"
	}
	d.add("Owner", owner)
	if !e.Executable() {
		d.add("Build", "this build has no Go implementation of the rule; it is reported as skipped")
	}
	d.add("Decision", m.Overlap.Decision)
	d.add("Overlap", m.Overlap.Rationale)
	c := m.Compatibility
	d.add("Compatible azd", c.Azd)
	keys := make([]string, 0, len(c.Extensions))
	for k := range c.Extensions {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		d.add("Compatible extension "+k, c.Extensions[k])
	}
	d.add("API versions", strings.Join(c.APIVersions, ", "))
	if c.Preview {
		d.add("Preview", "verified against prerelease tooling")
	}
	d.add("Compatibility notes", c.Notes)
	for i, s := range m.Sources {
		line := s.URL
		if s.LastVerified != "" {
			line += " (verified " + s.LastVerified + ")"
		}
		if s.VerifiedVia != "" {
			line += " via " + s.VerifiedVia
		}
		if s.Note != "" {
			line += ": " + s.Note
		}
		d.add(fmt.Sprintf("Source %d", i+1), line)
	}
	d.add("Notes", m.Notes)
	switch m.Status {
	case catalog.StatusProposed:
		d.add("Unverified", "the rule is proposed and not yet researched against a primary source")
	case catalog.StatusProductOpinion:
		d.add("Unverified", "product opinion: no platform source states this requirement")
	}
	return d
}

func renderConsole(sb *strings.Builder, d doc) {
	sb.WriteString(d.title + "\n")
	for _, f := range d.fields {
		if strings.Contains(f[1], "\n") {
			sb.WriteString(f[0] + ":\n")
			for _, l := range strings.Split(f[1], "\n") {
				sb.WriteString("    " + l + "\n")
			}
			continue
		}
		fmt.Fprintf(sb, "%s: %s\n", f[0], f[1])
	}
}

func renderMarkdown(sb *strings.Builder, d doc) {
	sb.WriteString("# " + markdown.Escape(d.title) + "\n\n")
	for _, f := range d.fields {
		switch f[0] {
		case "Fix":
			sb.WriteString("\n**Fix**\n\n```yaml\n" + f[1] + "\n```\n\n")
		case "Rationale", "Evidence", "Remediation", "Notes", "Overlap", "Compatibility notes":
			sb.WriteString("\n**" + f[0] + "**\n\n" + f[1] + "\n\n")
		default:
			sb.WriteString("- **" + f[0] + "**: " + strings.Join(strings.Fields(f[1]), " ") + "\n")
		}
	}
}
