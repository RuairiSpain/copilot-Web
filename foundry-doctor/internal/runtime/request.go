package runtime

import "time"

// Options are the shared runtime execution settings.
type Options struct {
	Vantage     Vantage
	Timeout     time.Duration
	Parallelism int
}
