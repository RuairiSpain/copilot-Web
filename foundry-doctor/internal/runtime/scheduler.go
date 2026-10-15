package runtime

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Probe is one runtime probe executed by the scheduler.
type Probe interface {
	ID() string
	Class() Class
	Timeout() time.Duration
	Run(context.Context) (Result, error)
}

// Scheduler executes probes with bounded parallelism and per-probe timeouts.
type Scheduler struct {
	Parallelism int
}

// Run executes probes and returns results sorted by probe id.
func (s Scheduler) Run(ctx context.Context, probes []Probe) (map[string]Result, error) {
	if len(probes) == 0 {
		return map[string]Result{}, nil
	}
	parallel := s.Parallelism
	if parallel <= 0 {
		parallel = 4
	}
	type item struct {
		id  string
		res Result
		err error
	}
	sem := make(chan struct{}, parallel)
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out = make([]item, 0, len(probes))
	)
	for _, p := range probes {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				out = append(out, item{id: p.ID(), err: ctx.Err()})
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, Timeout(p.Timeout(), 30*time.Second))
			defer cancel()
			res, err := p.Run(pctx)
			if err == nil && res.Class == "" {
				res.Class = p.Class()
			}
			mu.Lock()
			out = append(out, item{id: p.ID(), res: res, err: err})
			mu.Unlock()
		}()
	}
	wg.Wait()
	slices.SortFunc(out, func(a, b item) int { return compareStrings(a.id, b.id) })
	res := make(map[string]Result, len(out))
	var errs []error
	for _, it := range out {
		if it.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", it.id, it.err))
			continue
		}
		res[it.id] = it.res
	}
	return res, errorsJoin(errs...)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func errorsJoin(errs ...error) error {
	var out error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if out == nil {
			out = err
			continue
		}
		out = fmt.Errorf("%v; %w", out, err)
	}
	return out
}
