package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/findings"
	"github.com/ruairispain/copilot-web/foundry-doctor/pkg/sdk"
)

// mapPolicy adapts a flattened dotted-key map to sdk.Policy.
type mapPolicy map[string]any

// MapPolicy returns a read-only sdk.Policy over a flattened policy map. A nil
// map yields a policy with no keys.
func MapPolicy(m map[string]any) sdk.Policy { return mapPolicy(m) }

// Get implements sdk.Policy.
func (p mapPolicy) Get(key string) (any, bool) {
	v, ok := p[key]
	if !ok {
		if alt, okAlt := strings.CutPrefix(key, "policy."); okAlt {
			v, ok = p[alt]
		} else {
			v, ok = p["policy."+key]
		}
	}
	return v, ok
}

// Difference is one policy key that differs between two environments.
type Difference struct {
	Key   string `json:"key"`
	Left  any    `json:"left,omitempty"`
	Right any    `json:"right,omitempty"`
	// Presence is "both", "left-only" or "right-only".
	Presence string `json:"presence"`
}

// Comparison is the result of an offline environment compare.
type Comparison struct {
	Left         string       `json:"left"`
	Right        string       `json:"right"`
	LeftProfile  string       `json:"leftProfile"`
	RightProfile string       `json:"rightProfile"`
	Differences  []Difference `json:"differences"`
}

// CompareRequest mirrors the compare flags.
type CompareRequest struct {
	Dir        string
	Left       string
	Right      string
	Format     string
	Out        string
	FailOnDiff bool
}

// CompareSettings diffs two flattened policy maps deterministically.
func CompareSettings(left, right map[string]any) []Difference {
	keys := map[string]struct{}{}
	for k := range left {
		keys[k] = struct{}{}
	}
	for k := range right {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	diffs := []Difference{}
	for _, k := range sorted {
		l, lok := left[k]
		r, rok := right[k]
		switch {
		case lok && rok:
			if !reflect.DeepEqual(l, r) {
				diffs = append(diffs, Difference{Key: k, Left: l, Right: r, Presence: "both"})
			}
		case lok:
			diffs = append(diffs, Difference{Key: k, Left: l, Presence: "left-only"})
		default:
			diffs = append(diffs, Difference{Key: k, Right: r, Presence: "right-only"})
		}
	}
	return diffs
}

// Compare resolves two environments offline and prints their policy
// differences. It never needs Azure credentials. Differences are informational
// and exit 0 unless FailOnDiff is set; unresolved environments are exit 2.
func Compare(ctx context.Context, svc Services, req CompareRequest, stdout io.Writer) (int, error) {
	code, err := compare(ctx, svc, req, stdout)
	if err != nil {
		return ExitCodeForError(err), err
	}
	return code, nil
}

func compare(ctx context.Context, svc Services, req CompareRequest, stdout io.Writer) (int, error) {
	if req.Format == "" {
		req.Format = "console"
	}
	if req.Format != "console" && req.Format != "json" {
		return 0, Usagef("invalid --format %q for compare (want console or json)", req.Format)
	}
	if req.Left == "" || req.Right == "" {
		return 0, Usagef("compare needs two environment names")
	}
	if req.Dir == "" {
		req.Dir = "."
	}
	l, err := svc.Config.Resolve(ctx, ConfigRequest{Dir: req.Dir, Environment: req.Left})
	if err != nil {
		return 0, fmt.Errorf("resolve environment %q: %w", req.Left, err)
	}
	r, err := svc.Config.Resolve(ctx, ConfigRequest{Dir: req.Dir, Environment: req.Right})
	if err != nil {
		return 0, fmt.Errorf("resolve environment %q: %w", req.Right, err)
	}
	cmp := Comparison{
		Left: req.Left, Right: req.Right,
		LeftProfile: l.Profile, RightProfile: r.Profile,
		Differences: CompareSettings(l.Policy, r.Policy),
	}
	var sb strings.Builder
	if req.Format == "json" {
		b, err := json.MarshalIndent(cmp, "", "  ")
		if err != nil {
			return 0, fmt.Errorf("marshal comparison: %w", err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	} else {
		fmt.Fprintf(&sb, "Compare %s (profile %s) with %s (profile %s)\n", req.Left, l.Profile, req.Right, r.Profile)
		if len(cmp.Differences) == 0 {
			sb.WriteString("No policy differences.\n")
		}
		for _, d := range cmp.Differences {
			fmt.Fprintf(&sb, "  %s: %s -> %s\n", d.Key, render(d.Left, d.Presence == "right-only"), render(d.Right, d.Presence == "left-only"))
		}
	}
	code := ExitOK
	if req.FailOnDiff && len(cmp.Differences) > 0 {
		code = ExitFindings
	}
	return code, svc.emit(req.Out, []byte(findings.Redact(sb.String())), stdout)
}

func render(v any, absent bool) string {
	if absent {
		return "(unset)"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
