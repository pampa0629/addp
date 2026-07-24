package continuous

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/planner"
	"github.com/google/uuid"
)

func TestBoundedReplayRunnerUsesExplicitRangeAndExecutionApplyIdentity(t *testing.T) {
	reader := &fakeChangeStreamReader{batch: &plugin.ChangeRecordBatch{Records: []plugin.ChangeRecord{
		{Topic: "orders.events", Partition: "0", Offset: 4, Value: []byte(`{"id":4}`), Position: kafkaOffsetPosition("0", 5)},
		{Topic: "orders.events", Partition: "0", Offset: 5, Value: []byte(`{"id":5}`), Position: kafkaOffsetPosition("0", 6)},
	}}}
	target := &fakeChangeApplyProvider{}
	targetChecked := false
	runner := &BoundedReplayRunner{
		PollTimeout: time.Millisecond,
		AssertTargetAbsent: func(context.Context, *planner.ContinuousTargetPlan) error {
			targetChecked = true
			return nil
		},
		GetPlugin: func(engineType string) (plugin.EnginePlugin, error) {
			if engineType == "kafka" {
				return &fakeChangeStreamProvider{reader: reader}, nil
			}
			return target, nil
		},
	}
	applyIdentity := uuid.NewString()
	result, err := runner.Run(context.Background(), replayTestPlan(), []ReplayOffsetRange{{Partition: "0", StartOffset: 4, EndOffset: 5}}, applyIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if !targetChecked || target.prepareOptions.ApplyIdentity != applyIdentity {
		t.Fatalf("target checked=%v prepare options=%#v", targetChecked, target.prepareOptions)
	}
	if !target.prepareOptions.RequireTargetAbsent {
		t.Fatal("bounded replay target prepare did not enforce race-safe target absence")
	}
	if len(target.lastBatch.Changes) != 1 || target.lastBatch.Changes[0].Row["id"] != int32(4) {
		t.Fatalf("replay batch = %#v", target.lastBatch)
	}
	if result.Positions["0"] != 5 || result.RecordsRead != 1 || result.RecordsWritten != 1 {
		t.Fatalf("replay result = %#v", result)
	}
}

func TestBoundedReplayRunnerRejectsEmptyAndRepeatedRanges(t *testing.T) {
	runner := &BoundedReplayRunner{}
	for _, ranges := range [][]ReplayOffsetRange{
		{{Partition: "0", StartOffset: 4, EndOffset: 4}},
		{{Partition: "0", StartOffset: 4, EndOffset: 5}, {Partition: "0", StartOffset: 5, EndOffset: 6}},
	} {
		if _, err := runner.Run(context.Background(), replayTestPlan(), ranges, uuid.NewString()); err == nil {
			t.Fatalf("invalid ranges were accepted: %#v", ranges)
		}
	}
}

func TestReplayTargetAbsenceValidatorRejectsExistingTable(t *testing.T) {
	target := replayTestPlan().Target
	target.Path = plugin.TabularItemPath(8, plugin.CatalogTermSchema, "replay", "orders_replay")
	catalog := &fakeReplayCatalogProvider{entries: []plugin.CatalogEntry{{Name: "orders_replay"}}}
	validator := NewReplayTargetAbsenceValidator(func(string) (plugin.EnginePlugin, error) { return catalog, nil })
	if err := validator(context.Background(), &target); !errors.Is(err, ErrReplayTargetExists) {
		t.Fatalf("existing target error = %v", err)
	}
	if got := catalog.parent.StringPath(); got != "replay" {
		t.Fatalf("catalog parent = %q", got)
	}
}

type fakeReplayCatalogProvider struct {
	fakeChangeApplyProvider
	entries []plugin.CatalogEntry
	parent  plugin.CatalogPath
}

func (p *fakeReplayCatalogProvider) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	p.parent = parent
	return p.entries, nil
}

func (p *fakeReplayCatalogProvider) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}

func TestBoundedReplayRunnerRejectsRangeOutsideRetentionBeforeTargetCreation(t *testing.T) {
	reader := &fakeChangeStreamReader{}
	target := &fakeChangeApplyProvider{}
	targetChecked := false
	runner := &BoundedReplayRunner{
		AssertTargetAbsent: func(context.Context, *planner.ContinuousTargetPlan) error {
			targetChecked = true
			return nil
		},
		GetPlugin: func(engineType string) (plugin.EnginePlugin, error) {
			if engineType == "kafka" {
				return &fakeChangeStreamProvider{reader: reader}, nil
			}
			return target, nil
		},
	}
	_, err := runner.Run(context.Background(), replayTestPlan(), []ReplayOffsetRange{{Partition: "0", StartOffset: 0, EndOffset: 7}}, uuid.NewString())
	if err == nil {
		t.Fatal("out-of-retention replay range was accepted")
	}
	if targetChecked || target.prepared {
		t.Fatalf("target was touched before retention validation: checked=%v prepared=%v", targetChecked, target.prepared)
	}
}

func replayTestPlan() *planner.ContinuousPlan {
	return &planner.ContinuousPlan{
		Source: planner.ContinuousSourcePlan{SourceIdentity: "addp://engine/30/path/orders.events?type=topic", PollBatchSize: 100},
		Target: planner.ContinuousTargetPlan{
			Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}}, Keys: []string{"id"},
		},
		Mappings:   []planner.ContinuousFieldPlan{{Source: "id", Target: "id", Type: datatype.FieldTypeInt}},
		SourceKeys: []string{"id"}, SourceType: "kafka", TargetType: "postgresql",
		Envelope: planner.ContinuousEnvelopeRecord, RecordFailureMode: planner.RecordFailureModeBlock,
	}
}
