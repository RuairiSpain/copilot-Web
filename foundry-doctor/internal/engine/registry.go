package engine

import (
	"context"
	"fmt"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cfg"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/cost"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/env"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/idn"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/net"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/ops"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/rel"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/rules/sec"
	rulesdata "github.com/ruairispain/copilot-web/foundry-doctor/rules"
)

// Catalogue is the loaded catalogue, its packs and the registry joined to the implementations.
type Catalogue struct {
	Registry *rules.Registry
	Packs    []rules.Pack
}

// AllImplementations returns the Go rules of every group package, group by group.
func AllImplementations() []rules.Rule {
	var all []rules.Rule
	for _, g := range [][]rules.Rule{cfg.Rules(), sec.Rules(), net.Rules(), idn.Rules(), env.Rules(), ops.Rules(), rel.Rules(), cost.Rules()} {
		all = append(all, g...)
	}
	return all
}

// DefaultCatalogue loads the embedded catalogue and packs and joins them to AllImplementations.
// It fails on a malformed catalogue or pack, a version mismatch, or an implementation of a rule that
// is not a phase 1 rule. It does not require every phase 1 rule to be implemented; see Registry.CheckComplete.
func DefaultCatalogue(ctx context.Context) (*Catalogue, error) {
	return NewCatalogue(ctx, AllImplementations())
}

// NewCatalogue is DefaultCatalogue with the implementations supplied by the caller.
func NewCatalogue(ctx context.Context, impls []rules.Rule) (*Catalogue, error) {
	cat, err := rules.LoadCatalog(ctx, rulesdata.FS, rulesdata.CatalogRoot)
	if err != nil {
		return nil, err
	}
	packs, err := rules.LoadPacks(ctx, rulesdata.FS, rulesdata.PacksRoot)
	if err != nil {
		return nil, err
	}
	if err := rules.ValidatePacks(packs, cat); err != nil {
		return nil, fmt.Errorf("validate packs: %w", err)
	}
	reg, err := rules.NewRegistry(cat, impls, rules.Phase1)
	if err != nil {
		return nil, err
	}
	return &Catalogue{Registry: reg, Packs: packs}, nil
}
