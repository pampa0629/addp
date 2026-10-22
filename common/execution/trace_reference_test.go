package execution

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/models"
)

func testTraceReference() *TraceReference {
	return &TraceReference{TraceID: "11111111111111111111111111111111", SpanID: "2222222222222222", ModuleName: "orchestrator", Role: "backend", InstanceID: "process-1", StartedAt: time.Date(2026, 10, 23, 1, 2, 3, 0, time.UTC)}
}
func TestEnqueueReferenceImmutableAndNoMapAlias(t *testing.T) {
	original := models.JSONMap{"business": "kept"}
	item := &TaskExecution{ExecutionConfig: original}
	ref := testTraceReference()
	if err := FreezeEnqueueTrace(item, ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := original[ObservabilityKey]; ok {
		t.Fatal("mutated original config")
	}
	ref.SpanID = "3333333333333333"
	frozen, err := EnqueueTrace(item)
	if err != nil || frozen.SpanID != "2222222222222222" {
		t.Fatal("aliased ref", err)
	}
	if err := FreezeEnqueueTrace(item, frozen); err != nil {
		t.Fatal("not idempotent", err)
	}
	before, _ := json.Marshal(item.ExecutionConfig)
	if err := FreezeEnqueueTrace(item, ref); err == nil {
		t.Fatal("rewrote enqueue")
	}
	after, _ := json.Marshal(item.ExecutionConfig)
	if string(before) != string(after) {
		t.Fatal("changed invalid write")
	}
	for _, value := range []any{nil, map[string]any{"schema": TraceSchema, "enqueue": frozen, "extra": "secret"}, map[string]any{"schema": "old", "enqueue": frozen}} {
		bad := &TaskExecution{ExecutionConfig: models.JSONMap{ObservabilityKey: value}}
		if _, err := EnqueueTrace(bad); err == nil {
			t.Fatal("accepted malformed namespace")
		}
		if err := FreezeEnqueueTrace(bad, frozen); err == nil {
			t.Fatal("overwrote malformed namespace")
		}
	}
}
func TestLatestReferenceReplacesOneFactPreservingOwnerMetadata(t *testing.T) {
	item := &TaskExecution{}
	if value, err := EnqueueTrace(item); value != nil || err != nil {
		t.Fatal("missing enqueue not optional")
	}
	if value, err := LatestTrace(item); value != nil || err != nil {
		t.Fatal("missing latest not optional")
	}
	metadata := models.JSONMap{"business": "kept"}
	ref := testTraceReference()
	first, err := LatestTraceMetadata(metadata, ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := metadata[ObservabilityKey]; exists {
		t.Fatal("original metadata changed")
	}
	ref.SpanID = "3333333333333333"
	second, err := LatestTraceMetadata(first, ref)
	if err != nil {
		t.Fatal(err)
	}
	if second["business"] != "kept" || len(second) != 2 {
		t.Fatal("metadata lost or history grew")
	}
	firstRef, _ := LatestTrace(&TaskExecution{Metadata: first})
	secondRef, _ := LatestTrace(&TaskExecution{Metadata: second})
	if firstRef.SpanID != "2222222222222222" || secondRef.SpanID != "3333333333333333" {
		t.Fatal("latest aliased")
	}
	secondRef.SpanID = "4444444444444444"
	again, _ := LatestTrace(&TaskExecution{Metadata: second})
	if again.SpanID != "3333333333333333" {
		t.Fatal("read reference aliases metadata")
	}
	raw, _ := json.Marshal(second)
	var reloaded models.JSONMap
	if json.Unmarshal(raw, &reloaded) != nil {
		t.Fatal("JSON reload")
	}
	if got, err := LatestTrace(&TaskExecution{Metadata: reloaded}); err != nil || got.SpanID != again.SpanID {
		t.Fatal("persisted JSON not readable", err)
	}
	bad := models.JSONMap{"business": "kept", ObservabilityKey: map[string]any{"schema": TraceSchema, "latest": nil}}
	if _, err := LatestTraceMetadata(bad, ref); err == nil {
		t.Fatal("overwrote malformed latest")
	}
	if _, err := LatestTraceMetadata(metadata, nil); err != nil {
		t.Fatal("disabled reference")
	}
}
