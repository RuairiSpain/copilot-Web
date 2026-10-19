package monitor

import (
	"context"
	"testing"
	"time"
)

func TestFixedClientForwardsTheWindow(t *testing.T) {
	for _, w := range []time.Duration{0, 15 * time.Minute, 6 * time.Hour} {
		f := &fakeInner{}
		c := FixedClient{Inner: f, Window: w}
		ctx := context.Background()
		if _, err := c.QueryRequestsByStatus(ctx, "id"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.QueryProvisionedUtilization(ctx, "id"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.QueryTraffic(ctx, "id"); err != nil {
			t.Fatal(err)
		}
		if len(f.queries) != 3 {
			t.Fatalf("queries = %d", len(f.queries))
		}
		for _, q := range f.queries {
			if q.Window != w {
				t.Errorf("window = %v, want %v", q.Window, w)
			}
		}
	}
}
