package continuous

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
	"github.com/twpayne/go-geom"
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

func TestDataSessionRunnerConsumesRegisteredPostgreSQLCDCGeneration(t *testing.T) {
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	resolver := planner.StaticEngineResolver{
		12: {Type: "postgresql", ConnInfo: plugin.ConnectionInfo{"database": "business"}},
		20: {Type: "postgresql", ConnInfo: plugin.ConnectionInfo{"host": "unused"}, Capabilities: &targetCaps},
	}
	shapeWKB, err := commonSpatial.GeomToEWKB(geom.NewPointFlat(geom.XY, []float64{1, 2}), 4549)
	if err != nil {
		t.Fatal(err)
	}
	shapeValue := `{"wkb":"` + base64.StdEncoding.EncodeToString(shapeWKB) + `","srid":4549}`
	reader := &fakeChangeStreamReader{batch: &plugin.ChangeRecordBatch{Records: []plugin.ChangeRecord{
		{Topic: "__addp_cdc.7.42.1", Partition: "0", Offset: 0, Key: []byte(`{"id":1}`), Value: []byte(debeziumEnvelope("r", `null`, `{"id":1,"shape":`+shapeValue+`}`, "business", "public", "orders")), Position: kafkaOffsetPosition("0", 1)},
		{Topic: "__addp_cdc.7.42.1", Partition: "0", Offset: 1, Key: []byte(`{"id":1}`), Value: []byte(debeziumEnvelope("d", `{"id":1}`, `null`, "business", "public", "orders")), Position: kafkaOffsetPosition("0", 2)},
	}}}
	source := &fakeChangeStreamProvider{reader: reader}
	target := &fakeChangeApplyProvider{}
	states := &fakeContinuousStateStore{target: target}
	progress := &fakeContinuousProgressStore{committed: make(chan repository.ContinuousProgress, 1)}
	runner := &DataSessionRunner{
		Resolver: resolver, States: states, Progress: progress,
		Captures: &fakeCaptureStore{resource: &models.CaptureResource{
			TaskID: 42, TenantID: 7, Status: models.CaptureStatusRunning,
			TopicName: "__addp_cdc.7.42.1", ConsumerGroup: "__addp_cdc_consumer.7.42.1",
			SourceIdentity: "addp://engine/12/path/public/orders?type=table",
			SourceDatabase: "business", SourceSchema: "public", SourceTable: "orders",
			SourceSpatialInfo: models.JSONMap(datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4549, 2))),
			TopicCreated:      true, ConnectorCreated: true,
		}},
		InfraKafkaConnection: plugin.ConnectionInfo{"bootstrap_servers": "infra"},
		PollTimeout:          time.Millisecond,
		GetPlugin: func(engineType string) (plugin.EnginePlugin, error) {
			if engineType == "kafka" {
				return source, nil
			}
			return target, nil
		},
	}
	claim := continuousRunnerClaim()
	claim.Task.TenantID = 7
	claim.Task.BatchSize = 100
	claim.Task.Config = postgresqlCDCGeometryRunnerConfig()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, claim) }()
	select {
	case <-progress.committed:
		if source.options.ConsumerGroup != "__addp_cdc_consumer.7.42.1" {
			t.Fatalf("consumer group = %q", source.options.ConsumerGroup)
		}
		if len(target.lastBatch.Changes) != 2 || target.lastBatch.Changes[0].Operation != plugin.TableChangeOperationUpsert || target.lastBatch.Changes[1].Operation != plugin.TableChangeOperationDelete {
			t.Fatalf("CDC target batch = %#v", target.lastBatch)
		}
		if target.prepareOptions.SpatialInfo == nil || target.prepareOptions.SpatialInfo.PrimaryGeometryName() != "geometry" ||
			target.prepareOptions.SpatialInfo.PrimarySRIDValue() != 4549 {
			t.Fatalf("CDC target spatial options = %#v", target.prepareOptions.SpatialInfo)
		}
		if _, ok := target.lastBatch.Changes[0].Row["geometry"].([]byte); !ok {
			t.Fatalf("CDC geometry row = %T, want []byte EWKB", target.lastBatch.Changes[0].Row["geometry"])
		}
	case <-time.After(time.Second):
		t.Fatal("CDC runner did not commit progress")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestDataSessionRunnerRecordsIncompatiblePostgreSQLCDCSchemaField(t *testing.T) {
	targetCaps := (&postgresql.PostgreSQLPlugin{}).Capabilities()
	resolver := planner.StaticEngineResolver{
		12: {Type: "postgresql", ConnInfo: plugin.ConnectionInfo{"database": "business"}},
		20: {Type: "postgresql", ConnInfo: plugin.ConnectionInfo{"host": "unused"}, Capabilities: &targetCaps},
	}
	reader := &fakeChangeStreamReader{batch: &plugin.ChangeRecordBatch{Records: []plugin.ChangeRecord{{
		Topic: "__addp_cdc.7.42.1", Partition: "0", Offset: 0,
		Key: []byte(`{"id":1}`), Value: []byte(debeziumEnvelope("c", `null`, `{"id":"bad"}`, "business", "public", "orders")),
		Position: kafkaOffsetPosition("0", 1),
	}}}}
	target := &fakeChangeApplyProvider{}
	progress := &fakeContinuousProgressStore{schemaChange: make(chan repository.ContinuousSchemaChange, 1)}
	runner := &DataSessionRunner{
		Resolver: resolver, States: &fakeContinuousStateStore{target: target}, Progress: progress,
		Captures: &fakeCaptureStore{resource: &models.CaptureResource{
			TaskID: 42, TenantID: 7, Status: models.CaptureStatusRunning,
			TopicName: "__addp_cdc.7.42.1", ConsumerGroup: "__addp_cdc_consumer.7.42.1",
			SourceIdentity: "addp://engine/12/path/public/orders?type=table",
			SourceDatabase: "business", SourceSchema: "public", SourceTable: "orders",
			TopicCreated: true, ConnectorCreated: true,
		}},
		InfraKafkaConnection: plugin.ConnectionInfo{"bootstrap_servers": "infra"}, PollTimeout: time.Millisecond,
		GetPlugin: func(engineType string) (plugin.EnginePlugin, error) {
			if engineType == "kafka" {
				return &fakeChangeStreamProvider{reader: reader}, nil
			}
			return target, nil
		},
	}
	claim := continuousRunnerClaim()
	claim.Task.TenantID = 7
	claim.Task.Config = postgresqlCDCRunnerConfig()
	err := runner.Run(context.Background(), claim)
	var schemaErr *SchemaChangeError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("Run() error = %v, want SchemaChangeError", err)
	}
	select {
	case change := <-progress.schemaChange:
		if change.Scope != "Debezium after" || len(change.IncompatibleFields) != 1 || change.IncompatibleFields[0] != "id" {
			t.Fatalf("recorded schema change = %#v", change)
		}
	default:
		t.Fatal("schema change was not recorded")
	}
	if target.applied {
		t.Fatal("target applied an incompatible CDC event")
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

func postgresqlCDCRunnerConfig() models.JSONMap {
	return models.JSONMap{
		"runtime": map[string]interface{}{"boundary": "continuous"},
		"load": map[string]interface{}{
			"mode": "incremental", "change_detection": map[string]interface{}{"type": "cdc", "bootstrap": "initial_snapshot"},
		},
		"source": map[string]interface{}{
			"locator": "addp://engine/12/path/public/orders?type=table", "data_type": "table", "representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": "addp://engine/20/path/public?type=schema", "name": "orders_cdc", "data_type": "table", "representation": "native",
			"policy": map[string]interface{}{"apply_mode": "upsert_delete", "keys": []interface{}{"id"}},
		},
		"transforms": []interface{}{map[string]interface{}{
			"type": "field_mapping", "version": "v1", "mode": "project",
			"fields": []interface{}{map[string]interface{}{
				"source": "id", "target": "id", "target_type": "bigint", "nullable": false,
			}},
		}},
	}
}

func postgresqlCDCGeometryRunnerConfig() models.JSONMap {
	config := postgresqlCDCRunnerConfig()
	transform := config["transforms"].([]interface{})[0].(map[string]interface{})
	transform["fields"] = append(transform["fields"].([]interface{}), map[string]interface{}{
		"source": "shape", "target": "geometry", "target_type": "geometry", "nullable": true,
	})
	return config
}

type fakeChangeStreamProvider struct {
	reader  plugin.ChangeStreamReader
	options plugin.ChangeStreamReadOptions
}

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
func (p *fakeChangeStreamProvider) OpenChangeStream(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, options plugin.ChangeStreamReadOptions) (plugin.ChangeStreamReader, error) {
	p.options = options
	return p.reader, nil
}

type fakeCaptureStore struct{ resource *models.CaptureResource }

func (s *fakeCaptureStore) GetLatest(context.Context, uint, uint) (*models.CaptureResource, error) {
	return s.resource, nil
}

type fakeChangeStreamReader struct {
	batch *plugin.ChangeRecordBatch
	sent  bool
}

func (r *fakeChangeStreamReader) PositionRanges(context.Context) ([]plugin.ChangeStreamPositionRange, error) {
	return []plugin.ChangeStreamPositionRange{{
		Partition: "0", Earliest: kafkaOffsetPosition("0", 0), Latest: kafkaOffsetPosition("0", 6),
	}}, nil
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
	prepared       bool
	applied        bool
	prepareOptions plugin.PartitionedTableChangeApplyOptions
	lastBatch      *plugin.PartitionedTableChangeBatch
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
func (p *fakeChangeApplyProvider) PreparePartitionedTableChangeApply(_ context.Context, _ plugin.ConnectionInfo, _ plugin.CatalogPath, options plugin.PartitionedTableChangeApplyOptions) error {
	p.prepared = true
	p.prepareOptions = options
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
	committed    chan repository.ContinuousProgress
	schemaChange chan repository.ContinuousSchemaChange
}

func (s *fakeContinuousProgressStore) RecordProgress(_ context.Context, _ repository.RuntimeLeaseClaim, progress repository.ContinuousProgress) error {
	s.committed <- progress
	return nil
}

func (s *fakeContinuousProgressStore) RecordDiagnostics(context.Context, repository.RuntimeLeaseClaim, repository.ContinuousDiagnostics) error {
	return nil
}

func (s *fakeContinuousProgressStore) RecordSchemaChange(_ context.Context, _ repository.RuntimeLeaseClaim, change repository.ContinuousSchemaChange) error {
	if s.schemaChange != nil {
		s.schemaChange <- change
	}
	return nil
}

func TestCollectContinuousDiagnosticsCalculatesLagAndRetentionHealth(t *testing.T) {
	sampledAt := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	reader := &diagnosticsChangeStreamReader{ranges: []plugin.ChangeStreamPositionRange{{
		Partition: "0", Earliest: kafkaOffsetPosition("0", 0), Latest: kafkaOffsetPosition("0", 100),
	}}}
	committed := map[string]plugin.ChangeStreamPosition{"0": kafkaOffsetPosition("0", 50)}
	previous := map[string]sourceLatestSample{"0": {Latest: 80, SampledAt: sampledAt.Add(-10 * time.Second)}}

	diagnostics, _ := collectContinuousDiagnostics(
		context.Background(), reader, committed,
		map[string]time.Time{"0": sampledAt.Add(-5 * time.Second)}, previous, sampledAt,
		time.Minute, 10*time.Second, 30*time.Second,
	)
	partition := diagnostics.Partitions["0"]
	if diagnostics.Health != continuousHealthDegraded || partition.Health != continuousHealthDegraded {
		t.Fatalf("health = %q/%q, want degraded", diagnostics.Health, partition.Health)
	}
	if partition.LagRecords == nil || *partition.LagRecords != 50 {
		t.Fatalf("lag = %#v, want 50", partition.LagRecords)
	}
	if partition.RecoveryHeadroomRecords == nil || *partition.RecoveryHeadroomRecords != 50 {
		t.Fatalf("headroom = %#v, want 50", partition.RecoveryHeadroomRecords)
	}
	if partition.SourceRateRecordsPerSecond == nil || *partition.SourceRateRecordsPerSecond != 2 {
		t.Fatalf("source rate = %#v, want 2", partition.SourceRateRecordsPerSecond)
	}
	if partition.RetentionHorizonSeconds == nil || *partition.RetentionHorizonSeconds != 25 {
		t.Fatalf("retention horizon = %#v, want 25", partition.RetentionHorizonSeconds)
	}
}

func TestCollectContinuousDiagnosticsMarksLostRetentionCritical(t *testing.T) {
	reader := &diagnosticsChangeStreamReader{ranges: []plugin.ChangeStreamPositionRange{{
		Partition: "0", Earliest: kafkaOffsetPosition("0", 20), Latest: kafkaOffsetPosition("0", 100),
	}}}
	diagnostics, _ := collectContinuousDiagnostics(
		context.Background(), reader,
		map[string]plugin.ChangeStreamPosition{"0": kafkaOffsetPosition("0", 10)}, nil, nil, time.Now(),
		6*time.Hour, time.Hour, 5*time.Minute,
	)
	partition := diagnostics.Partitions["0"]
	if diagnostics.Health != continuousHealthCritical || partition.Health != continuousHealthCritical {
		t.Fatalf("health = %q/%q, want critical", diagnostics.Health, partition.Health)
	}
	if partition.RecoveryHeadroomRecords == nil || *partition.RecoveryHeadroomRecords != -10 {
		t.Fatalf("headroom = %#v, want -10", partition.RecoveryHeadroomRecords)
	}
}

func TestCollectContinuousDiagnosticsKeepsColdLagUnknownAndCaughtUpHealthy(t *testing.T) {
	reader := &diagnosticsChangeStreamReader{ranges: []plugin.ChangeStreamPositionRange{
		{Partition: "0", Earliest: kafkaOffsetPosition("0", 0), Latest: kafkaOffsetPosition("0", 100)},
		{Partition: "1", Earliest: kafkaOffsetPosition("1", 0), Latest: kafkaOffsetPosition("1", 50)},
	}}
	diagnostics, _ := collectContinuousDiagnostics(
		context.Background(), reader, map[string]plugin.ChangeStreamPosition{
			"0": kafkaOffsetPosition("0", 90), "1": kafkaOffsetPosition("1", 50),
		}, nil, nil, time.Now(), 6*time.Hour, time.Hour, 5*time.Minute,
	)
	if diagnostics.Partitions["0"].Health != continuousHealthUnknown {
		t.Fatalf("cold lagging health = %q, want unknown", diagnostics.Partitions["0"].Health)
	}
	if diagnostics.Partitions["1"].Health != continuousHealthHealthy {
		t.Fatalf("caught up health = %q, want healthy", diagnostics.Partitions["1"].Health)
	}
	if diagnostics.Health != continuousHealthUnknown {
		t.Fatalf("overall health = %q, want unknown", diagnostics.Health)
	}
}

func TestCollectContinuousDiagnosticsMarksOnlyLaggingStaleCheckpointDegraded(t *testing.T) {
	sampledAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	reader := &diagnosticsChangeStreamReader{ranges: []plugin.ChangeStreamPositionRange{
		{Partition: "0", Earliest: kafkaOffsetPosition("0", 0), Latest: kafkaOffsetPosition("0", 100)},
		{Partition: "1", Earliest: kafkaOffsetPosition("1", 0), Latest: kafkaOffsetPosition("1", 50)},
	}}
	diagnostics, _ := collectContinuousDiagnostics(
		context.Background(), reader,
		map[string]plugin.ChangeStreamPosition{
			"0": kafkaOffsetPosition("0", 80), "1": kafkaOffsetPosition("1", 50),
		},
		map[string]time.Time{
			"0": sampledAt.Add(-6 * time.Minute), "1": sampledAt.Add(-24 * time.Hour),
		}, nil, sampledAt, 6*time.Hour, time.Hour, 5*time.Minute,
	)
	if diagnostics.CheckpointHealth != continuousHealthDegraded {
		t.Fatalf("checkpoint health = %q, want degraded", diagnostics.CheckpointHealth)
	}
	stale := diagnostics.Partitions["0"]
	if stale.CheckpointHealth != continuousHealthDegraded || stale.CheckpointAgeSeconds == nil || *stale.CheckpointAgeSeconds != 360 {
		t.Fatalf("stale checkpoint diagnostics = %#v", stale)
	}
	if caughtUp := diagnostics.Partitions["1"]; caughtUp.CheckpointHealth != continuousHealthHealthy || caughtUp.CheckpointAgeSeconds != nil {
		t.Fatalf("caught-up checkpoint diagnostics = %#v", caughtUp)
	}
}

type diagnosticsChangeStreamReader struct {
	fakeChangeStreamReader
	ranges []plugin.ChangeStreamPositionRange
	err    error
}

func (r *diagnosticsChangeStreamReader) PositionRanges(context.Context) ([]plugin.ChangeStreamPositionRange, error) {
	return r.ranges, r.err
}
