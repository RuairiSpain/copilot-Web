package compare_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ruairispain/copilot-web/foundry-doctor/internal/compare"
	"github.com/ruairispain/copilot-web/foundry-doctor/internal/model"
)

func TestSignatureCompiles(t *testing.T) {
	var fn func(context.Context, model.Environment, model.Environment) (compare.Diff, error) = compare.Environments
	if _, err := fn(context.Background(), model.Environment{}, model.Environment{}); !errors.Is(err, compare.ErrNotImplemented) {
		t.Fatalf("unexpected %v", err)
	}
}
