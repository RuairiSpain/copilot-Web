package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/project"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/report/markdown"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// Comparer compares two environments. internal/compare.Environments is the default.
type Comparer func(ctx context.Context, left, right model.Environment) (compare.Diff, error)

// keyLine is what compare prints about one key: the name and the kind of difference, never a value and never
// a value fingerprint (a hash of a short value can be guessed).
type keyLine struct {
	Key  string           `json:"key"`
	Kind compare.DiffKind `json:"kind"`
}

type compareDoc struct {
	Left  string    `json:"left"`
	Right string    `json:"right"`
	Keys  []keyLine `json:"keys"`
}

// Compare lists the environment keys that differ between two azd environments of the project
// (.azure/<name>/.env). Values are never printed. The exit code is 0 whenever the comparison ran: a
// difference between environments is information, not a failure.
func Compare(ctx context.Context, opts CompareOptions, stdout, stderr io.Writer) error {
	ee := doCompare(ctx, opts, stdout)
	if ee == nil {
		return nil
	}
	fmt.Fprintf(stderr, "error: %s\n", ee.Err)
	return ee
}

func doCompare(ctx context.Context, opts CompareOptions, stdout io.Writer) *ExitError {
	format := opts.Format
	if format == "" {
		format = "console"
	}
	switch format {
	case "console", "json", "markdown":
	default:
		return exitErr(sdk.ExitCannotRun, "--format %q is not supported by compare: use console, json or markdown", format)
	}
	cmp := opts.Comparer
	if cmp == nil {
		cmp = compare.Environments
	}
	start := opts.Dir
	if start == "" {
		start = "."
	}
	disc, err := project.Discover(ctx, start)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "find the project: %w", err)
	}
	azd, err := project.NewStandaloneAzdContext(disc.Project.Root)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "open the project: %w", err)
	}
	defer azd.Close()

	l, err := azd.EnvValues(ctx, opts.Left)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "left environment: %w", err)
	}
	r, err := azd.EnvValues(ctx, opts.Right)
	if err != nil {
		return exitErr(sdk.ExitCannotRun, "right environment: %w", err)
	}
	diff, err := cmp(ctx, l, r)
	if err != nil {
		return exitErr(sdk.ExitInternal, "compare environments: %w", err)
	}
	out := compareDoc{Left: opts.Left, Right: opts.Right, Keys: []keyLine{}}
	for _, k := range diff.Keys {
		out.Keys = append(out.Keys, keyLine{Key: k.Key, Kind: k.Kind})
	}
	var sb strings.Builder
	switch format {
	case "json":
		b, merr := json.MarshalIndent(out, "", "  ")
		if merr != nil {
			return exitErr(sdk.ExitInternal, "encode: %w", merr)
		}
		sb.Write(b)
		sb.WriteString("\n")
	case "markdown":
		fmt.Fprintf(&sb, "# Environment comparison: %s and %s\n\n", markdown.Escape(out.Left), markdown.Escape(out.Right))
		sb.WriteString("Values are never shown.\n\n| Key | Difference |\n| --- | --- |\n")
		for _, k := range out.Keys {
			fmt.Fprintf(&sb, "| %s | %s |\n", markdown.Escape(k.Key), k.Kind)
		}
	default:
		fmt.Fprintf(&sb, "Comparing %s with %s (values are never shown)\n", out.Left, out.Right)
		for _, k := range out.Keys {
			fmt.Fprintf(&sb, "  %-12s %s\n", k.Kind, k.Key)
		}
		if len(out.Keys) == 0 {
			sb.WriteString("  no keys\n")
		}
	}
	if _, err := io.WriteString(stdout, sb.String()); err != nil {
		return exitErr(sdk.ExitCannotRun, "write to stdout: %w", err)
	}
	return nil
}
