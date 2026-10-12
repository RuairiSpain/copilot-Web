package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/config"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/engine"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/console"
	jsonreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/report/json"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/markdown"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/sarif"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// extension returns the file extension used when a format is written into the outputs directory.
func extension(f config.Format) string {
	switch f {
	case config.FormatJSON:
		return "json"
	case config.FormatMarkdown:
		return "md"
	case config.FormatSARIF:
		return "sarif"
	}
	return "txt"
}

// planFormats returns the de-duplicated formats to write and checks --out.
func planFormats(eff config.Effective, opts Options) ([]config.Format, *ExitError) {
	var fs []config.Format
	for _, f := range eff.Outputs.Formats {
		if !slices.Contains(fs, f) {
			fs = append(fs, f)
		}
	}
	if len(fs) == 0 {
		fs = []config.Format{config.FormatConsole}
	}
	if opts.Out != "" && len(fs) != 1 {
		return nil, exitErr(sdk.ExitCannotRun, "--out writes one report: give exactly one --format (configured formats: %d)", len(fs))
	}
	return fs, nil
}

func reporterFor(f config.Format, opts Options, ruleInfo map[string]sarif.RuleInfo) (sdk.Reporter, error) {
	switch f {
	case config.FormatConsole:
		return console.New(console.Options{Color: opts.Color}), nil
	case config.FormatJSON:
		return jsonreport.New(), nil
	case config.FormatMarkdown:
		return markdown.New(), nil
	case config.FormatSARIF:
		return sarif.New(sarif.Options{Rules: ruleInfo}), nil
	}
	return nil, fmt.Errorf("unknown format %q", f)
}

// sarifRules gives the SARIF reporter the catalogue text of every rule.
func sarifRules(cat *engine.Catalogue) map[string]sarif.RuleInfo {
	m := map[string]sarif.RuleInfo{}
	for _, r := range cat.Registry.Catalog() {
		ri := sarif.RuleInfo{Title: r.Title, Description: strings.TrimSpace(r.Description), Help: strings.TrimSpace(r.Recommendation)}
		if len(r.Sources) > 0 {
			ri.HelpURI = r.Sources[0].URL
		}
		m[r.ID] = ri
	}
	return m
}

// repoPrefix returns the slash path from the repository root (the nearest parent holding .git) to the
// project root, or "" when the project is the root or no repository is found. SARIF code scanning wants
// URIs relative to the repository root (docs/development/ci.md).
func repoPrefix(projectRoot string) string {
	dir := projectRoot
	for range 64 {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			rel, err := filepath.Rel(dir, projectRoot)
			if err != nil || rel == "." {
				return ""
			}
			return filepath.ToSlash(rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func withPrefix(r sdk.Report, prefix string) sdk.Report {
	if prefix == "" {
		return r
	}
	r.Findings = slices.Clone(r.Findings)
	for i, f := range r.Findings {
		if f.Location.File != "" {
			r.Findings[i].Location.File = prefix + "/" + f.Location.File
		}
	}
	return r
}

func writeReports(ctx context.Context, root, base string, formats []config.Format, opts Options, eff config.Effective, cat *engine.Catalogue, rep sdk.Report, stdout io.Writer) error {
	info := sarifRules(cat)
	for _, f := range formats {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := reporterFor(f, opts, info)
		if err != nil {
			return err
		}
		doc := rep
		if f == config.FormatSARIF {
			doc = withPrefix(rep, repoPrefix(root))
		}
		var buf bytes.Buffer
		if err := r.Write(&buf, &doc); err != nil {
			return err
		}
		switch {
		case opts.Out != "":
			if err := writeOutput(root, base, opts.Out, buf.Bytes()); err != nil {
				return err
			}
		case len(formats) == 1 || f == config.FormatConsole:
			if _, err := stdout.Write(buf.Bytes()); err != nil {
				return fmt.Errorf("write to stdout: %w", err)
			}
		default:
			name := filepath.Join(eff.Outputs.Directory, "foundry-doctor."+extension(f))
			if err := writeOutput(root, root, name, buf.Bytes()); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeDebug prints what --debug promises: the effective policy, tool versions and the redacted adapter
// command. It never prints environment values, file contents, or absolute tool paths.
func writeDebug(w io.Writer, eff config.Effective, iac iacResult, rep sdk.Report, notes []string) {
	fmt.Fprintf(w, "debug: profile %s (source %s)\n", eff.Profile.Name, eff.ProfileSource)
	if eff.Environment != "" {
		fmt.Fprintf(w, "debug: environment %s\n", eff.Environment)
	}
	for _, e := range eff.PolicyEntries {
		fmt.Fprintf(w, "debug: policy %s = %s (%s)\n", e.Key, redactLine(e.Value), e.Source)
	}
	for _, t := range rep.Tools {
		fmt.Fprintf(w, "debug: tool %s state=%s version=%s required=%t path=%s\n", t.Name, t.State, t.Version, t.Required, t.Path)
	}
	if len(iac.tools) > 0 {
		fmt.Fprintf(w, "debug: adapter command: %s\n", bicepCommand)
	}
	for _, n := range notes {
		if n != "" {
			fmt.Fprintf(w, "debug: %s\n", redactLine(n))
		}
	}
}
