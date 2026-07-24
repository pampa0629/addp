package deadletter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/transfer/internal/models"
)

type reconcileIndexFake struct {
	references []models.DeadLetterPayloadReference
	marked     []models.DeadLetterPayloadReference
	stale      map[string]bool
}

func (f *reconcileIndexFake) ListAvailablePayloadReferences(_ context.Context, afterIdentity string, limit int) ([]models.DeadLetterPayloadReference, error) {
	var result []models.DeadLetterPayloadReference
	for _, reference := range f.references {
		if reference.Identity <= afterIdentity {
			continue
		}
		result = append(result, reference)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (f *reconcileIndexFake) MarkPayloadUnavailable(_ context.Context, reference models.DeadLetterPayloadReference, _ time.Time) (bool, error) {
	f.marked = append(f.marked, reference)
	return !f.stale[reference.Identity], nil
}

type reconcileProbeFake struct {
	availability map[string]bool
	err          error
}

func (f reconcileProbeFake) Probe(_ context.Context, _ []models.DeadLetterPayloadReference) (map[string]bool, error) {
	return f.availability, f.err
}

func TestPayloadAvailabilityReconcilerAppliesConfirmedUnavailableWithCAS(t *testing.T) {
	refs := []models.DeadLetterPayloadReference{
		{Identity: "a", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 10},
		{Identity: "b", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 11},
		{Identity: "c", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 12},
	}
	index := &reconcileIndexFake{references: refs, stale: map[string]bool{"c": true}}
	reconciler, err := NewPayloadAvailabilityReconciler(index, reconcileProbeFake{
		availability: map[string]bool{"a": true, "b": false, "c": false},
		err:          errors.New("one topic timed out"),
	}, PayloadAvailabilityReconcilerConfig{Interval: time.Minute, BatchSize: 100, Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := reconciler.RunOnce(context.Background())
	if err == nil {
		t.Fatal("partial probe error was lost")
	}
	if stats.Candidates != 3 || stats.ConfirmedAvailable != 1 || stats.ConfirmedUnavailable != 2 ||
		stats.UpdatedUnavailable != 1 || stats.StaleReferences != 1 || stats.Unresolved != 0 {
		t.Fatalf("reconcile stats = %#v", stats)
	}
	if len(index.marked) != 2 || index.marked[0].Identity != "b" || index.marked[1].Identity != "c" {
		t.Fatalf("marked references = %#v", index.marked)
	}
}

func TestPayloadAvailabilityReconcilerRotatesIdentityCursor(t *testing.T) {
	refs := []models.DeadLetterPayloadReference{
		{Identity: "a", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 10},
		{Identity: "b", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 11},
		{Identity: "c", Topic: "__addp_dlq.7.11", Partition: 0, Offset: 12},
	}
	index := &reconcileIndexFake{references: refs, stale: map[string]bool{}}
	reconciler, err := NewPayloadAvailabilityReconciler(index, reconcileProbeFake{availability: map[string]bool{
		"a": true, "b": true, "c": true,
	}}, PayloadAvailabilityReconcilerConfig{Interval: time.Minute, BatchSize: 2, Timeout: time.Second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := reconciler.RunOnce(context.Background())
	if err != nil || first.Candidates != 2 {
		t.Fatalf("first cycle stats=%#v err=%v", first, err)
	}
	second, err := reconciler.RunOnce(context.Background())
	if err != nil || second.Candidates != 1 {
		t.Fatalf("second cycle stats=%#v err=%v", second, err)
	}
	third, err := reconciler.RunOnce(context.Background())
	if err != nil || third.Candidates != 2 {
		t.Fatalf("wrapped cycle stats=%#v err=%v", third, err)
	}
}
