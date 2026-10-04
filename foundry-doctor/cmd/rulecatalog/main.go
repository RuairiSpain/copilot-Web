// Command rulecatalog validates the rule catalogue and generates docs/rule-catalog.md.
//
//	rulecatalog validate [--phase0] [--dir rules/catalog]
//	rulecatalog generate-docs [--dir rules/catalog] [--out docs/rule-catalog.md] [--check]
//
// Exit codes: 0 ok, 1 catalogue invalid or generated file stale, 2 usage or I/O error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/catalog"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: rulecatalog validate|generate-docs [flags]")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "rules/catalog", "catalogue directory")
	phase0 := fs.Bool("phase0", false, "validate: fail rules still in the proposed state")
	out := fs.String("out", "docs/rule-catalog.md", "generate-docs: output file")
	check := fs.Bool("check", false, "generate-docs: fail if the output file is not up to date, without writing")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	rules, err := catalog.Load(os.DirFS("."), *dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	switch args[0] {
	case "validate":
		if err := catalog.Validate(rules, catalog.Options{Phase0Gate: *phase0}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "catalogue valid: %d rules\n", len(rules))
		return 0
	case "generate-docs":
		if err := catalog.Validate(rules, catalog.Options{}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		md := catalog.Markdown(rules)
		if *check {
			cur, err := os.ReadFile(*out)
			if err != nil || string(cur) != md {
				fmt.Fprintf(stderr, "%s is stale; run: go run ./cmd/rulecatalog generate-docs\n", *out)
				return 1
			}
			return 0
		}
		if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stdout, "wrote %s (%d rules)\n", *out, len(rules))
		return 0
	}
	fmt.Fprintf(stderr, "unknown command %q\n", args[0])
	return 2
}
