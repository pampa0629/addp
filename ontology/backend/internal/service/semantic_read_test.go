package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSemanticResultBudgetAndCancellation(t *testing.T) {
	if err := boundSemanticResult(context.Background(), strings.Repeat("x", (128<<10)-2)); err != nil {
		t.Fatal(err)
	}
	if err := boundSemanticResult(context.Background(), strings.Repeat("x", 128<<10)); !errors.Is(err, ErrResultTooLarge) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := boundSemanticResult(ctx, "small"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
