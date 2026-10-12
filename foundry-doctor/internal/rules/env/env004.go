package env

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// differenceSummary is FND-ENV-004. It is informational: every profile maps it to info.
type differenceSummary struct{ src Source }

func (differenceSummary) ID() string   { return "FND-ENV-004" }
func (differenceSummary) Version() int { return 1 }

const (
	env004Rec = "Review the summary before promoting a release; use azd env get-values per environment for the details you are entitled to see."
	env004Fix = "foundry-doctor compare --env dev --env prod\n"
	listLimit = 20
)

// Evaluate summarises, for every pair of visible environments, the keys present on one side only,
// the keys whose values differ and which of those are environment-bound. It is built on
// compare.Environments, the function behind the compare command, so it shares its guarantee: key
// names and equal/different outcomes only, never a value or a hash of a value. The default
// environment is marked but never treated as a baseline.
func (r differenceSummary) Evaluate(ctx context.Context, in *model.Input) ([]rules.Result, error) {
	envs := visible(r.src, in)
	if len(envs) < 2 {
		return []rules.Result{skipSingle(r, in, len(envs))}, nil
	}
	var out []rules.Result
	for i := range envs {
		for j := i + 1; j < len(envs); j++ {
			d, err := compare.Environments(ctx, envs[i], envs[j])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", r.ID(), err)
			}
			if d.Identical() {
				out = append(out, rules.Pass())
				continue
			}
			f := rules.NewFinding(r, sdk.ResourceRef{Kind: "project", Name: in.Project.Name},
				summary(d, in.Project.CurrentEnvironment), env004Rec)
			f.Key = "diff:" + d.Left + "+" + d.Right
			f.Location = envLocation(d.Left)
			f.Fix = env004Fix
			out = append(out, rules.Fail(f))
		}
	}
	return out, nil
}

func summary(d compare.Diff, current string) string {
	var onlyL, onlyR, differs, bound []string
	for _, k := range d.Keys {
		switch k.Kind {
		case compare.OnlyLeft:
			onlyL = append(onlyL, k.Key)
		case compare.OnlyRight:
			onlyR = append(onlyR, k.Key)
		case compare.Different:
			differs = append(differs, k.Key)
			if k.Bound {
				bound = append(bound, k.Key)
			}
		}
	}
	label := func(n string) string {
		if n == current && n != "" {
			return n + " (default)"
		}
		return n
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Environments %s and %s: ", label(d.Left), label(d.Right))
	fmt.Fprintf(&b, "%d key(s) only in %s [%s]; %d only in %s [%s]; %d differ [%s], of which %d environment-bound [%s]; %d same. ",
		len(onlyL), d.Left, nameList(onlyL, listLimit), len(onlyR), d.Right, nameList(onlyR, listLimit),
		len(differs), nameList(differs, listLimit), len(bound), nameList(bound, listLimit), d.Count(compare.Same))
	if d.Subscription == compare.Different {
		b.WriteString("The subscription differs. ")
	}
	if d.ResourceGroup == compare.Different {
		b.WriteString("The resource group differs. ")
	}
	b.WriteString("Values are not shown.")
	return b.String()
}
