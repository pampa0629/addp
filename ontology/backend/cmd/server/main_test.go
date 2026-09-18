package main

import (
	"context"
	"errors"
	"github.com/addp/common/modulelifecycle"
	"testing"
	"time"
)

func TestReadinessChecksAreBoundedAndSanitized(t *testing.T) {
	check := readyCheck("falkordb", func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Fatal("unbounded readiness")
		}
		return errors.New("secret internal endpoint")
	})
	r := check(context.Background())
	if r.Status != modulelifecycle.CheckNotReady || r.ErrorCode != "falkordb_unavailable" {
		t.Fatalf("%+v", r)
	}
	check = readyCheck("postgres", func(context.Context) error { return nil })
	if check(context.Background()).Status != modulelifecycle.CheckReady {
		t.Fatal("healthy dependency rejected")
	}
}
