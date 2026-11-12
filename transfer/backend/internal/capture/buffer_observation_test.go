package capture

import (
	"context"
	"errors"
	"github.com/addp/transfer/internal/models"
	"log/slog"
	"testing"
	"time"
)

func TestBufferObservationTracksPausedBacklogAndRetentionLoss(t *testing.T) {
	next := int64(10)
	store := &fakeCaptureStore{committed: &next, resource: &models.CaptureResource{ID: 1, TaskID: 7, Status: models.CaptureStatusRunning, ResourceVersion: 1}}
	topics := &fakeTopicControl{latest: 10}
	s := &Supervisor{store: store, topics: topics, config: SupervisorConfig{MonitorInterval: time.Second, SourceProbeTimeout: time.Second}, logger: slog.Default()}
	sample := func() *models.CaptureBufferObservation {
		t.Helper()
		if err := s.observeBuffer(context.Background(), store.resource); err != nil {
			t.Fatal(err)
		}
		return models.NewCaptureSummary(store.resource).BufferObservation
	}
	value := sample()
	if *value.LagOffsets != "0" || value.RecoveryState != "available" {
		t.Fatalf("caught up: %+v", value)
	}
	// No worker/execution is running: source messages accumulate while committed stays fixed.
	topics.latest = 110
	value = sample()
	if *value.LagOffsets != "100" || *value.CommittedOffset != "10" {
		t.Fatalf("paused: %+v", value)
	}
	topics.earliest = 11
	if value = sample(); value.RecoveryState != "lost" {
		t.Fatalf("retention: %+v", value)
	}
	topics.bufferErr = errors.New("private broker detail")
	value = sample()
	if value.Status != "unavailable" || value.LagOffsets != nil || value.LatestOffset != nil || value.RecoveryState != "unknown" {
		t.Fatalf("failure reused old sample: %+v", value)
	}
	topics.bufferErr = nil
	topics.earliest = 0
	next = 110
	if value = sample(); *value.LagOffsets != "0" || value.RecoveryState != "available" {
		t.Fatalf("resumed: %+v", value)
	}
	store.committed = nil
	if value = sample(); value.RecoveryState != "not_started" || value.CommittedOffset != nil {
		t.Fatalf("unstarted: %+v", value)
	}
	topics.earliest = 1
	if value = sample(); value.RecoveryState != "lost" {
		t.Fatalf("unstarted retention loss: %+v", value)
	}
	store.resource.Status = models.CaptureStatusStopped
	if models.NewCaptureSummary(store.resource).BufferObservation != nil {
		t.Fatal("stopped generation exposes sample")
	}
}

func TestBufferObservationRejectsFuturePositionAndLifecycleConflict(t *testing.T) {
	next := int64(11)
	store := &fakeCaptureStore{committed: &next, resource: &models.CaptureResource{ID: 1, Status: models.CaptureStatusRunning, ResourceVersion: 2}}
	s := &Supervisor{store: store, topics: &fakeTopicControl{latest: 10}, config: SupervisorConfig{MonitorInterval: time.Second, SourceProbeTimeout: time.Second}}
	if err := s.observeBuffer(context.Background(), store.resource); err != nil {
		t.Fatal(err)
	}
	if models.NewCaptureSummary(store.resource).BufferObservation.Status != "unavailable" {
		t.Fatal("future position accepted")
	}
	old := *store.resource
	store.resource.ResourceVersion++
	if err := s.observeBuffer(context.Background(), &old); err == nil {
		t.Fatal("stale lifecycle publication accepted")
	}
}
