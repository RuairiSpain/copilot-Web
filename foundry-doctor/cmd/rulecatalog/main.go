// Command rulecatalog validates the rule catalogue and generates documents from it.
//
//	rulecatalog validate         [--dir rules/catalog] [--phase0]
//	rulecatalog generate-docs    [--dir rules/catalog] [--out docs/rule-catalog.md]     [--check]
//	rulecatalog generate-overlap [--dir rules/catalog] [--out docs/overlap-analysis.md] [--check]
//
// Exit codes: 0 ok; 1 the catalogue is invalid or a generated file is stale;
// 2 usage error or an I/O failure (cannot read the catalogue, cannot write the output).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

const usage = "usage: rulecatalog validate|generate-docs|generate-overlap [flags]"

func main() { os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cmd := args[0]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "validate", "generate-docs", "generate-overlap":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", cmd, usage)
		return 2
	}

	fl := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fl.SetOutput(stderr)
	dir := fl.String("dir", "rules/catalog", "catalogue directory")
	phase0 := fl.Bool("phase0", false, "validate only: fail rules still in the proposed state")
	defaultOut := map[string]string{"generate-docs": "docs/rule-catalog.md", "generate-overlap": "docs/overlap-analysis.md"}[cmd]
	out := fl.String("out", defaultOut, "generate-* only: output file")
	check := fl.Bool("check", false, "generate-* only: fail if the output file is not up to date, without writing")
	if err := fl.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fl.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fl.Args())
		return 2
	}
	set := map[string]bool{}
	fl.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if cmd == "validate" && (set["out"] || set["check"]) || cmd != "validate" && set["phase0"] {
		fmt.Fprintf(stderr, "flag not valid for %s\n", cmd)
		return 2
	}

	rules, err := catalog.Load(ctx, os.DirFS(*dir), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		var inv *catalog.InvalidError
		if errors.As(err, &inv) {
			return 1
		}
		return 2
	}
	opt := catalog.Options{Phase0Gate: cmd == "validate" && *phase0}
	if err := catalog.Validate(rules, opt); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if cmd == "validate" {
		fmt.Fprintf(stdout, "catalogue valid: %d rules\n", len(rules))
		return 0
	}

	doc := catalog.Markdown(rules)
	if cmd == "generate-overlap" {
		doc = catalog.OverlapMatrix(rules)
	}
	if *check {
		cur, err := os.ReadFile(*out)
		switch {
		case errors.Is(err, fs.ErrNotExist) || err == nil && string(cur) != doc:
			fmt.Fprintf(stderr, "%s is stale; run: go run ./cmd/rulecatalog %s\n", *out, cmd)
			return 1
		case err != nil:
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	if err := writeFileAtomic(*out, []byte(doc)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "wrote %s (%d rules)\n", *out, len(rules))
	return 0
}

// writeFileAtomic writes via a temporary file in the same directory, so a failed write never leaves a truncated output.
func writeFileAtomic(name string, data []byte) error {
	d := filepath.Dir(name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d, ".rulecatalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}
