package continuous

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
)

func TestDataSessionRunnerAppliesThenCommitsPartitionPosition(t *testing.T) {
	sourceCaps := (&kafka.KafkaPlugin{}).Capabilities()
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	resolver := planner.StaticEngineResolver{
		30: {Type: "kafka", ConnInfo: plugin.ConnectionInfo{"bootstrap_servers": "unused"}, Capabilities: &sourceCaps},
		8:  {Type: "postgresql", ConnInfo: plugin.ConnectionInfo{"host": "unused"}, Capabilities: &targetCaps},
	}
	reader := &fakeChangeStreamReader{batch: &plugin.ChangeRecordBatch{Records: []plugin.ChangeRecord{
		{Topic: "orders.events", Partition: "0", Offset: 4, Value: []byte(`{"id":7,"name":"first"}`), Position: kafkaOffsetPosition("0", 5)},
		{Topic: "orders.events", Partition: "0", Offset: 5, Value: []byte(`{"id":7,"name":"latest"}`), Position: kafkaOffsetPosition("0", 6)},
	}}}
	target := &fakeChangeApplyProvider{}
	states := &fakeContinuousStateStore{target: target}
	progress := &fakeContinuousProgressStore{committed: make(chan repository.ContinuousProgress, 1)}
	runner := &DataSessionRunner{
		Resolver: resolver, States: states, Progress: progress, PollTimeout: time.Millisecond,
		GetPlugin: func(engineType string) (plugin.EnginePlugin, error) {
			if engineType == "kafka" {
				return &fakeChangeStreamProvider{reader: reader}, nil
			}
			return target, nil
		},
	}
	claim := continuousRunnerClaim()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, claim) }()
	select {
	case got := <-progress.committed:
		if got.RecordsRead != 2 || got.RecordsWritten != 1 {
			t.Fatalf("progress = %#v", got)
		}
		if states.committedOffset != 6 {
			t.Fatalf("committed offset = %d, want 6", states.committedOffset)
		}
		if len(target.lastBatch.Changes) != 2 || target.lastBatch.Changes[1].Row["name"] != "latest" {
			t.Fatalf("target batch = %#v", target.lastBatch)
		}
		if gotID, ok := target.lastBatch.Changes[0].Row["id"].(int32); !ok || gotID != 7 {
			t.Fatalf("mapped id = %#v, want int32(7)", target.lastBatch.Changes[0].Row["id"])
		}
	case <-time.After(time.Second):
		t.Fatal("continuous runner did not commit progress")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("continuous runner did not stop after cancellation")
	}
}

func TestDecodeAndMapRecordRejectsUnknownAndMissingRequiredFields(t *testing.T) {
	plan := &planner.ContinuousPlan{
		Mappings: []planner.ContinuousFieldPlan{
			{Source: "id", Target: "id", Type: "int", Nullable: false},
			{Source: "name", Target: "name", Type: "string", Nullable: false},
		},
		SourceKeys: []string{"id"}, Target: planner.ContinuousTargetPlan{Keys: []string{"id"}},
	}
	for _, value := range [][]byte{
		[]byte(`{"id":1,"name":"ok","extra":true}`),
		[]byte(`{"id":1}`),
		[]byte(`{"id":null,"name":"bad"}`),
		[]byte(`{"id":"not-an-int","name":"bad"}`),
	} {
		if _, err := decodeAndMapRecord(plugin.ChangeRecord{Value: value}, plan); err == nil {
			t.Fatalf("decodeAndMapRecord(%s) error = nil", value)
		}
	}
}

func continuousRunnerClaim() repository.RuntimeLeaseClaim {
	return repository.RuntimeLeaseClaim{
		Task: models.TransferTask{
			ID: 42, ApplyIdentity: uuid.NewString(), TaskType: "sync", Config: continuousRunnerConfig(),
		},
		Execution: commonExecution.TaskExecution{ExecutionID: "exec-continuous"},
		Lease:     models.RuntimeLease{TaskID: 42, OwnerInstanceID: "worker-a", FencingToken: 3},
	}
}

func continuousRunnerConfig() models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous"},
		"load":    map[string]interface{}{"mode": "incremental", "change_detection": map[string]interface{}{"type": "kafka"}},
		"source": map[string]interface{}{
			"locator": "addp://engine/30/path/orders.events?type=topic", "representation": "native",
			"change_stream": map[string]interface{}{
				"envelope": "record", "encoding": "json", "key": map[string]interface{}{"source": "value", "fields": []interface{}{"id"}},
				"start": map[string]interface{}{"mode": "committed", "initial": "earliest"}, "poll_batch_size": 100,
			},
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/8/path/public?type=schema", "name": "orders", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{
				map[string]interface{}{"source": "id", "target": "id", "target_type": "int", "nullable": false},
				map[string]interface{}{"source": "name", "target": "name", "target_type": "string", "nullable": false},
			},
		}},
	}
}

type fakeChangeStreamProvider struct{ reader plugin.ChangeStreamReader }

func (p *fakeChangeStreamProvider) Type() string                                       { return "kafka" }
func (p *fakeChangeStreamProvider) DisplayName() string                                { return "fake kafka" }
func (p *fakeChangeStreamProvider) EngineOrigin() string                               { return "general" }
func (p *fakeChangeStreamProvider) DefaultPort() int                                   { return 9092 }
func (p *fakeChangeStreamProvider) RequiredFields() []string                           { return nil }
func (p *fakeChangeStreamProvider) SensitiveFields() []string                          { return nil }
func (p *fakeChangeStreamProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *fakeChangeStreamProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *fakeChangeStreamProvider) Capabilities() plugin.EngineCapabilities {
	return (&kafka.KafkaPlugin{}).Capabilities()
}
func (p *fakeChangeStreamProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *fakeChangeStreamProvider) OpenChangeStream(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ChangeStreamReadOptions) (plugin.ChangeStreamReader, error) {
	return p.reader, nil
}

type fakeChangeStreamReader struct {
	batch *plugin.ChangeRecordBatch
	sent  bool
}

func (r *fakeChangeStreamReader) Poll(ctx context.Context, _ int) (*plugin.ChangeRecordBatch, error) {
	if !r.sent {
		r.sent = true
		return r.batch, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (r *fakeChangeStreamReader) Assignments() []string                  { return []string{"0"} }
func (r *fakeChangeStreamReader) Pause(context.Context, []string) error  { return nil }
func (r *fakeChangeStreamReader) Resume(context.Context, []string) error { return nil }
func (r *fakeChangeStreamReader) Close(context.Context) error            { return nil }

type fakeChangeApplyProvider struct {
	prepared  bool
	applied   bool
	lastBatch *plugin.PartitionedTableChangeBatch
}

func (p *fakeChangeApplyProvider) Type() string                                       { return "postgresql" }
func (p *fakeChangeApplyProvider) DisplayName() string                                { return "fake postgresql" }
func (p *fakeChangeApplyProvider) EngineOrigin() string                               { return "general" }
func (p *fakeChangeApplyProvider) DefaultPort() int                                   { return 5432 }
func (p *fakeChangeApplyProvider) RequiredFields() []string                           { return nil }
func (p *fakeChangeApplyProvider) SensitiveFields() []string                          { return nil }
func (p *fakeChangeApplyProvider) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *fakeChangeApplyProvider) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *fakeChangeApplyProvider) Capabilities() plugin.EngineCapabilities {
	return (&postgresql.PostgreSQLPlugin{}).Capabilities()
}
func (p *fakeChangeApplyProvider) StoreSemantics() plugin.StoreSemantics {
	return plugin.StoreSemanticsFromCapabilities(p.Capabilities())
}
func (p *fakeChangeApplyProvider) PreparePartitionedTableChangeApply(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.PartitionedTableChangeApplyOptions) error {
	p.prepared = true
	return nil
}
func (p *fakeChangeApplyProvider) ApplyPartitionedTableChanges(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, batch *plugin.PartitionedTableChangeBatch, _ plugin.PartitionedTableChangeApplyOptions) (*plugin.PartitionedTableChangeApplyResult, error) {
	p.applied = true
	p.lastBatch = batch
	return &plugin.PartitionedTableChangeApplyResult{AppliedRecords: 1, SkippedRecords: 1, Position: batch.EndPosition}, nil
}

type fakeContinuousStateStore struct {
	target          *fakeChangeApplyProvider
	committedOffset int64
}

func (s *fakeContinuousStateStore) List(context.Context, uint, string) ([]models.SyncState, error) {
	return nil, nil
}
func (s *fakeContinuousStateStore) ClaimContinuousPartition(context.Context, uint, string, string, string, string, string, uint64) (*models.SyncState, error) {
	return &models.SyncState{ID: 1, TaskID: 42, Partition: "0", PositionType: plugin.ChangeStreamPositionTypeKafkaOffset, PositionVersion: plugin.ChangeStreamPositionVersionV1}, nil
}
func (s *fakeContinuousStateStore) CommitContinuousPosition(_ context.Context, _, _ uint, _ uint64, _ uint64, _ string, position models.JSONMap, _ string) error {
	if !s.target.applied {
		return errors.New("position committed before target apply")
	}
	state := &models.SyncState{Partition: "0", Position: position}
	parsed, _, err := syncStatePosition(state)
	if err != nil {
		return err
	}
	s.committedOffset, err = kafkaNextOffset(parsed)
	return err
}

type fakeContinuousProgressStore struct {
	committed chan repository.ContinuousProgress
}

func (s *fakeContinuousProgressStore) RecordProgress(_ context.Context, _ repository.RuntimeLeaseClaim, progress repository.ContinuousProgress) error {
	s.committed <- progress
	return nil
}
