package projectionstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
)

type runnerChangeSource struct {
	batch            *dataprotection.ProjectionChangesResponse
	acknowledgements []string
}

func (s *runnerChangeSource) ListProtectionProjectionChangesForTenant(context.Context, uint, string, int) (*dataprotection.ProjectionChangesResponse, error) {
	return s.batch, nil
}

func (s *runnerChangeSource) AcknowledgeProtectionProjectionCursorForTenant(_ context.Context, _ uint, cursor string) error {
	s.acknowledgements = append(s.acknowledgements, cursor)
	return nil
}

type runnerAcknowledgementBarrier struct {
	err     error
	cursors []string
}

func (b *runnerAcknowledgementBarrier) ReadyToAcknowledge(_ context.Context, _ int64, cursor string) error {
	b.cursors = append(b.cursors, cursor)
	return b.err
}

func TestRunnerDoesNotAcknowledgeUntilPostCommitBarrierSucceeds(t *testing.T) {
	store, err := New(openProjectionStoreDB(t), "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "sha256:item"}
	projection := enrollingProjection(t, "manager", target)
	source := &runnerChangeSource{batch: &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{
			ChangeID: "change-1", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection,
		}},
		NextCursor: "cursor-1",
	}}
	barrier := &runnerAcknowledgementBarrier{err: errors.New("old execution still active")}
	runner := NewRunner(store, source, nil, time.Second, barrier)

	if err := runner.syncTenant(context.Background(), 7); err == nil {
		t.Fatal("syncTenant() must wait when the acknowledgement barrier is not ready")
	}
	if len(source.acknowledgements) != 0 {
		t.Fatalf("acknowledgements = %#v", source.acknowledgements)
	}
	if cursor, err := store.CurrentCursor(context.Background(), 7); err != nil || cursor != "cursor-1" {
		t.Fatalf("durable cursor = %q, err = %v", cursor, err)
	}
	if !store.Gate(7, target, time.Now().UTC()).Managed {
		t.Fatal("projection must be installed before the acknowledgement barrier runs")
	}

	barrier.err = nil
	source.batch = &dataprotection.ProjectionChangesResponse{
		SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		NextCursor:    "cursor-1",
	}
	if err := runner.syncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(barrier.cursors) != 2 || barrier.cursors[1] != "cursor-1" {
		t.Fatalf("barrier cursors = %#v", barrier.cursors)
	}
	if len(source.acknowledgements) != 1 || source.acknowledgements[0] != "cursor-1" {
		t.Fatalf("acknowledgements = %#v", source.acknowledgements)
	}
}
