package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/foundry"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/plan"
	"github.com/RuairiSpain/copilot-Web/x-foundry/internal/provision"
)

type deployOptions struct {
	file         string
	account      string
	endpointBase string
	statePath    string
	dryRun       bool
	allowDestroy bool
}

// projectEndpointBase is where project endpoints live: <base>/<project>.
func (o deployOptions) projectEndpointBase() string {
	if o.endpointBase != "" {
		return strings.TrimRight(o.endpointBase, "/")
	}
	return strings.TrimSuffix(foundry.ProjectURL(o.account, ""), "/")
}

func (o deployOptions) stateFile(environment string) string {
	if o.statePath != "" {
		return o.statePath
	}
	return filepath.Join(filepath.Dir(o.file), ".xfoundry", environment+".state.json")
}

func runDeploy(p *plan.Plan, opts deployOptions, stdout, stderr io.Writer) int {
	printDiagnostics(p.Warnings, false, stdout, stderr)
	items, warnings := provision.Desired(p)
	printDiagnostics(warnings, false, stdout, stderr)

	environment := p.Config.Environment
	statePath := opts.stateFile(environment)
	state, err := provision.LoadState(statePath, environment)
	if err != nil {
		fprintln(stderr, err)
		return 1
	}
	actions := provision.Diff(items, state)
	for _, a := range actions {
		fprintf(stdout, "%s %s\n", symbolOf(a.Op), a.Key)
	}
	fprintf(stdout, "%s [environment: %s]\n", provision.Summary(actions), environment)
	if blocked := provision.CheckDestroy(actions, opts.allowDestroy); len(blocked) > 0 {
		printDiagnostics(blocked, false, stdout, stderr)
		return 1
	}
	if opts.dryRun {
		return 0
	}
	if opts.account == "" && opts.endpointBase == "" {
		fprintln(stderr, "deploy needs the Foundry resource name: pass --account or set AZURE_AI_ACCOUNT_NAME (an output of the generated infrastructure)")
		return 2
	}
	token, err := tokenSource()
	if err != nil {
		fprintln(stderr, err)
		return 1
	}
	base := opts.projectEndpointBase()
	engine := &provision.Engine{
		State: state,
		API: func(project string) provision.API {
			return &foundry.Client{BaseURL: base + "/" + project, Token: token, Retries: 3}
		},
		Persist: func(s *provision.State) error { return s.Save(statePath) },
		Log:     func(line string) { fprintln(stdout, line) },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := engine.Apply(ctx, actions); err != nil {
		fprintln(stderr, err)
		return 1
	}
	fprintf(stdout, "deployed; state saved to %s\n", statePath)
	return 0
}

func symbolOf(op provision.Op) string {
	switch op {
	case provision.OpCreate:
		return "+"
	case provision.OpUpdate:
		return "~"
	case provision.OpDelete:
		return "-"
	}
	return "="
}
