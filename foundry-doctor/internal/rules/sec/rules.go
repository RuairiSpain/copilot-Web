// Package sec holds the native FND-SEC-* rules for security.
//
// The rules read the compiled ARM template only (Input.ARM). The live (control-plane) half of each
// rule is not read offline and is reported once as skipped with control-plane-unavailable. FND-SEC-014
// is a wrap rule owned by the Bicep adapter and has no Go rule here.
package sec

import (
	"context"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
)

// Rules returns the rules of this group in ID order.
func Rules() []rules.Rule {
	return []rules.Rule{rule001{}, rule002{}, rule003{}, rule004{}, rule006{}}
}

// armRule is the shape shared by the five rules: guard the input, evaluate the template, append the
// by-design live skip.
type armRule interface {
	rules.Rule
	evaluateARM(in *model.Input) []rules.Result
}

func run(ctx context.Context, r armRule, in *model.Input) ([]rules.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if out, ok := armGuard(r, in); !ok {
		return out, nil
	}
	out := r.evaluateARM(in)
	return append(out, liveSkip(r)), nil
}
