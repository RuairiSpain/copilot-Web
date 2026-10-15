package app

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/cost"
	costreport "github.com/ruairispain/copilot-web/foundry-doctor/internal/cost/report"
)

// CostRequest mirrors the cost flags.
type CostRequest struct {
	Dir           string
	Profile       string
	Environment   string
	Compare       []string
	Currency      string
	HoursPerMonth float64
	Offline       bool
	PriceCache    string
	Format        string
	Out           string
}

// Validate checks cost request values.
func (r *CostRequest) Validate() error {
	if r.Format == "" {
		r.Format = "console"
	}
	switch r.Format {
	case "console", "json", "markdown":
	default:
		return Usagef("invalid --format %q (want console, json or markdown)", r.Format)
	}
	if r.HoursPerMonth < 0 {
		return Usagef("--hours-per-month must be >= 0")
	}
	return nil
}

// Cost renders an advisory fixed-capacity estimate.
func Cost(ctx context.Context, svc Services, req CostRequest, stdout io.Writer) (int, error) {
	if err := req.Validate(); err != nil {
		return ExitCodeForError(err), err
	}
	if svc.Cost == nil {
		return ExitUnavailable, fmt.Errorf("%w: cost estimation is not available in this build", ErrUnavailable)
	}
	doc, err := costRun(ctx, svc, req)
	if err != nil {
		return ExitCodeForError(err), err
	}
	var buf bytes.Buffer
	if err := costreport.Render(&buf, req.Format, doc); err != nil {
		return ExitCodeForError(Usagef("%s", err)), Usagef("%s", err)
	}
	if err := svc.emit(req.Out, buf.Bytes(), stdout); err != nil {
		return ExitCodeForError(err), err
	}
	return ExitOK, nil
}

func costRun(ctx context.Context, svc Services, req CostRequest) (costreport.Document, error) {
	envs := []cost.Environment{}
	primary, err := loadCostEnvironment(ctx, svc, req.Dir, req.Profile, req.Environment)
	if err != nil {
		return costreport.Document{}, err
	}
	envs = append(envs, primary)
	for _, name := range req.Compare {
		other, err := loadCostEnvironment(ctx, svc, req.Dir, req.Profile, name)
		if err != nil {
			return costreport.Document{}, err
		}
		envs = append(envs, other)
	}
	return svc.Cost.Estimate(ctx, CostInput{
		Dir:           req.Dir,
		Profile:       req.Profile,
		Environment:   req.Environment,
		Compare:       req.Compare,
		Currency:      req.Currency,
		HoursPerMonth: req.HoursPerMonth,
		Offline:       req.Offline,
		PriceCache:    req.PriceCache,
		loaded:        envs,
	})
}

func loadCostEnvironment(ctx context.Context, svc Services, dir, profile, environment string) (cost.Environment, error) {
	set, err := svc.Config.Resolve(ctx, ConfigRequest{Dir: dir, Profile: profile, Environment: environment})
	if err != nil {
		return cost.Environment{}, fmt.Errorf("resolve config: %w", err)
	}
	src, err := svc.Project.Load(ctx, dir, set.Inputs)
	if err != nil {
		return cost.Environment{}, fmt.Errorf("discover project: %w", err)
	}
	if svc.ARM == nil {
		return cost.Environment{}, fmt.Errorf("%w: ARM loading is unavailable", ErrUnavailable)
	}
	arm, err := svc.ARM.Load(ctx, src)
	if err != nil {
		return cost.Environment{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	name := set.Environment
	if name == "" {
		name = environment
	}
	if name == "" {
		name = "default"
	}
	return cost.Environment{Name: name, Profile: set.Profile, ARM: arm}, nil
}

type costEngine struct{}

func (costEngine) Estimate(ctx context.Context, in CostInput) (costreport.Document, error) {
	return cost.Engine{}.Estimate(ctx, cost.Request{
		Environments:   in.loaded,
		Currency:       in.Currency,
		HoursPerMonth:  in.HoursPerMonth,
		Offline:        in.Offline,
		PriceCachePath: in.PriceCache,
	})
}
