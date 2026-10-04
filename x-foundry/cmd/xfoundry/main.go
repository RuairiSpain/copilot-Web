// Command xfoundry validates and plans the x-foundry section of an azure.yaml.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/diag"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	"github.com/RuairiSpain/copilot-Web/x-foundry/schemas"
)

func fprintf(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
func fprintln(w io.Writer, args ...any)               { _, _ = fmt.Fprintln(w, args...) }
func fprint(w io.Writer, args ...any)                 { _, _ = fmt.Fprint(w, args...) }

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage: xfoundry <command> [flags] [file]

commands:
  validate <file> [--json]   validate x-foundry in an azure.yaml (exit 1 when invalid)
  plan <file> [--json]       print the ordered deployment plan
  schema                     print the x-foundry JSON Schema
`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "schema":
		var v any
		_ = json.Unmarshal(schemas.XFoundry, &v)
		out, _ := json.MarshalIndent(v, "", "  ")
		fprintln(stdout, string(out))
		return 0
	case "validate", "plan":
		return runFile(args[0], args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fprint(stdout, usage)
		return 0
	}
	fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
	return 2
}

func runFile(command string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	// Allow flags before or after the file name.
	var files []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		files = append(files, fs.Args()[:min(1, fs.NArg())]...)
		args = fs.Args()[min(1, fs.NArg()):]
	}
	if len(files) != 1 {
		fprintf(stderr, "%s needs exactly one file\n", command)
		return 2
	}
	analysis, err := plan.AnalyseFile(files[0])
	if err != nil {
		fprintln(stderr, err)
		return 1
	}
	if !analysis.OK() {
		printDiagnostics(analysis.Diagnostics, *asJSON, stdout, stderr)
		return 1
	}
	p := analysis.Plan
	if command == "validate" {
		printDiagnostics(p.Warnings, *asJSON, stdout, stderr)
		if !*asJSON {
			fprintf(stdout, "%s: valid (%d resources, %d warning(s))\n", files[0], len(p.Nodes), len(p.Warnings))
		}
		return 0
	}
	if *asJSON {
		out, err := p.JSON("  ")
		if err != nil {
			fprintln(stderr, err)
			return 1
		}
		fprintln(stdout, string(out))
		return 0
	}
	for i, layer := range p.Layers {
		fprintf(stdout, "step %d: %s\n", i+1, strings.Join(layer, ", "))
	}
	return 0
}

func printDiagnostics(ds []diag.Diagnostic, asJSON bool, stdout, stderr io.Writer) {
	if asJSON {
		if ds == nil {
			ds = []diag.Diagnostic{}
		}
		out, _ := json.MarshalIndent(ds, "", "  ")
		fprintln(stdout, string(out))
		return
	}
	for _, d := range ds {
		fprintln(stderr, d)
	}
}
