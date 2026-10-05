// Package console renders a report as plain text for a terminal or a CI log. The output does not
// depend on terminal width, the clock or the environment. Colour is off unless Options.Color is set.
package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/norm"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Options configure the console reporter.
type Options struct {
	// Color wraps severity labels in ANSI colour. Off by default; the caller decides, never the reporter.
	Color bool
}

// Reporter is the console sdk.Reporter.
type Reporter struct{ opts Options }

var _ sdk.Reporter = Reporter{}

// New returns a console reporter.
func New(opts Options) Reporter { return Reporter{opts: opts} }

// Format returns "console".
func (Reporter) Format() string { return "console" }

// Write renders r to w.
func (c Reporter) Write(w io.Writer, r *sdk.Report) error {
	rep := norm.Normalised(r)
	var b strings.Builder
	c.header(&b, &rep)
	c.findings(&b, &rep)
	c.skipped(&b, &rep)
	c.summary(&b, &rep)
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return fmt.Errorf("write console report: %w", err)
	}
	return nil
}

func (c Reporter) sev(s sdk.Severity) string {
	label := string(s)
	if label == "" {
		label = "unknown"
	}
	label = norm.Text(label, true)
	if !c.opts.Color {
		return label
	}
	code := map[sdk.Severity]string{sdk.SeverityError: "31", sdk.SeverityWarning: "33", sdk.SeverityInfo: "36"}[s]
	if code == "" {
		return label
	}
	return "\x1b[" + code + "m" + label + "\x1b[0m"
}

func (c Reporter) header(b *strings.Builder, r *sdk.Report) {
	name := norm.Text(r.Tool.Name, true)
	if name == "" {
		name = "foundry-doctor"
	}
	fmt.Fprintf(b, "%s %s\n", name, norm.Text(r.Tool.Version, true))
	fmt.Fprintf(b, "Profile: %s\n", norm.Text(r.Profile, true))
	b.WriteString("Effective policy:\n")
	lines := norm.PolicyLines(r.EffectivePolicy)
	if len(lines) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, kv := range lines {
		fmt.Fprintf(b, "  %s = %s\n", kv.Key, kv.Value)
	}
	if len(r.Tools) > 0 {
		b.WriteString("Tools:\n")
		for _, t := range r.Tools {
			req := "optional"
			if t.Required {
				req = "required"
			}
			fmt.Fprintf(b, "  %s %s: %s (%s)", norm.Text(t.Name, true), norm.Text(t.Version, true), norm.Text(string(t.State), true), req)
			if t.Detail != "" {
				fmt.Fprintf(b, " - %s", norm.Text(t.Detail, true))
			}
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
}

func (c Reporter) findings(b *strings.Builder, r *sdk.Report) {
	fmt.Fprintf(b, "Findings (%d)\n", len(r.Findings))
	if len(r.Findings) == 0 {
		b.WriteString("  none\n")
	}
	for _, g := range norm.GroupByFile(r.Findings) {
		fmt.Fprintf(b, "  %s\n", norm.Text(g.File, true))
		last := sdk.Severity("\x00")
		for _, f := range g.Findings {
			if f.Severity != last {
				fmt.Fprintf(b, "    %s\n", c.sev(f.Severity))
				last = f.Severity
			}
			c.finding(b, f)
		}
	}
	b.WriteByte('\n')
}

func (c Reporter) finding(b *strings.Builder, f sdk.Finding) {
	head := "      " + norm.Text(f.RuleID, true)
	if p := norm.Position(f.Location); p != "" {
		head += " @" + p
	}
	if f.Confidence != "" {
		head += " [" + norm.Text(string(f.Confidence), true) + "]"
	}
	if f.Suppressed != nil {
		head += " [suppressed until " + norm.Text(f.Suppressed.Expires, true) + "]"
	}
	if f.Baselined {
		head += " [baselined]"
	}
	b.WriteString(head + "\n")
	if rl := norm.ResourceLabel(f.Resource); rl != "" {
		fmt.Fprintf(b, "        resource: %s\n", rl)
	}
	indented(b, "evidence", f.Evidence)
	indented(b, "recommendation", f.Recommendation)
	indented(b, "fix", f.Fix)
	if f.Suppressed != nil {
		indented(b, "suppression reason", f.Suppressed.Reason)
	}
	if u := norm.Text(f.DocsURL, true); u != "" {
		fmt.Fprintf(b, "        docs: %s\n", u)
	}
	if f.Fingerprint != "" {
		fmt.Fprintf(b, "        fingerprint: %s\n", norm.Text(f.Fingerprint, true))
	}
}

func indented(b *strings.Builder, label, text string) {
	t := norm.Text(text, false)
	if t == "" {
		return
	}
	lines := strings.Split(t, "\n")
	fmt.Fprintf(b, "        %s: %s\n", label, lines[0])
	for _, l := range lines[1:] {
		fmt.Fprintf(b, "          %s\n", l)
	}
}

func (c Reporter) skipped(b *strings.Builder, r *sdk.Report) {
	fmt.Fprintf(b, "Skipped checks (%d)\n", len(r.Skipped))
	if len(r.Skipped) == 0 {
		b.WriteString("  none\n")
	}
	for _, s := range r.Skipped {
		fmt.Fprintf(b, "  %s: %s\n", norm.Text(s.RuleID, true), norm.Text(s.Reason, true))
		if s.MissingCapability != "" {
			fmt.Fprintf(b, "    missing capability: %s\n", norm.Text(s.MissingCapability, true))
		}
		if s.Detail != "" {
			fmt.Fprintf(b, "    detail: %s\n", norm.Text(s.Detail, true))
		}
		if rl := norm.ResourceLabel(s.Resource); rl != "" {
			fmt.Fprintf(b, "    resource: %s\n", rl)
		}
	}
	b.WriteByte('\n')
}

func (c Reporter) summary(b *strings.Builder, r *sdk.Report) {
	b.WriteString("Owner summary\n")
	s := r.Summary
	fmt.Fprintf(b, "  Errors: %d  Warnings: %d  Info: %d\n", s.Error, s.Warning, s.Info)
	fmt.Fprintf(b, "  Suppressed: %d  Baselined: %d\n", s.Suppressed, s.Baselined)
	fmt.Fprintf(b, "  Skipped: %d  Hidden by --min-severity: %d\n", norm.SkippedCount(r), s.Hidden)
	b.WriteString("  Top rules:\n")
	top := norm.TopRules(r.Findings, 5)
	if len(top) == 0 {
		b.WriteString("    none\n")
	}
	for _, t := range top {
		fmt.Fprintf(b, "    %s: %d (%s)\n", norm.Text(t.RuleID, true), t.Count, norm.Text(string(t.Severity), true))
	}
	if mk := norm.MissingProfileKeys(r.Skipped); len(mk) > 0 {
		b.WriteString("  Rules skipped because a profile key is missing:\n")
		for _, m := range mk {
			fmt.Fprintf(b, "    %s%s: %s\n", norm.ProfileKeyMissingPrefix, m.Key, strings.Join(m.Rules, ", "))
		}
	}
	fmt.Fprintf(b, "  Exit code: %d\n", r.ExitCode)
	b.WriteString("  " + norm.SkippedStatement + "\n")
}
