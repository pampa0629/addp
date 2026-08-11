package continuous

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/kafka"
	"github.com/addp/transfer/internal/deadletter"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
)

type ContinuousStateStore interface {
	List(ctx context.Context, taskID uint, sourceIdentity string) ([]models.SyncState, error)
	ClaimContinuousPartition(ctx context.Context, taskID uint, sourceIdentity, partition, positionType, positionVersion, owner string, fencingToken uint64) (*models.SyncState, error)
	CommitContinuousPosition(ctx context.Context, id, taskID uint, expectedVersion, fencingToken uint64, owner string, position models.JSONMap, executionID string) error
}

type ContinuousProgressStore interface {
	RecordProgress(ctx context.Context, claim repository.RuntimeLeaseClaim, progress repository.ContinuousProgress) error
	RecordDiagnostics(ctx context.Context, claim repository.RuntimeLeaseClaim, diagnostics repository.ContinuousDiagnostics) error
}

type ContinuousCaptureStore interface {
	GetLatest(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error)
}

type ContinuousSchemaChangeStore interface {
	RecordSchemaChange(ctx context.Context, claim repository.RuntimeLeaseClaim, change repository.ContinuousSchemaChange) error
}

type ContinuousDeadLetterRecorder interface {
	Record(ctx context.Context, request deadletter.RecordRequest) (*models.DeadLetter, error)
}

type PluginGetter func(engineType string) (engineplugin.EnginePlugin, error)

type DataSessionRunner struct {
	Resolver                 planner.EngineResolver
	States                   ContinuousStateStore
	Progress                 ContinuousProgressStore
	Captures                 ContinuousCaptureStore
	InfraKafkaConnection     engineplugin.ConnectionInfo
	GetPlugin                PluginGetter
	PollTimeout              time.Duration
	MaxBytes                 int
	DiagnosticsInterval      time.Duration
	RetentionDegradedHorizon time.Duration
	RetentionCriticalHorizon time.Duration
	CheckpointStaleAfter     time.Duration
	Now                      func() time.Time
	DeadLetters              ContinuousDeadLetterRecorder
	MetadataScanner          PreparedTargetMetadataScanner
}

const (
	continuousHealthHealthy  = "healthy"
	continuousHealthDegraded = "degraded"
	continuousHealthCritical = "critical"
	continuousHealthUnknown  = "unknown"
)

type sourceLatestSample struct {
	Latest    int64
	SampledAt time.Time
}

func (r *DataSessionRunner) Run(ctx context.Context, claim repository.RuntimeLeaseClaim) error {
	if r == nil || r.Resolver == nil || r.States == nil || r.Progress == nil {
		return fmt.Errorf("continuous data runner dependencies are required")
	}
	plan, err := r.buildPlan(ctx, claim)
	if err != nil {
		return err
	}
	if plan.RecordFailureMode == planner.RecordFailureModeDeadLetter && r.DeadLetters == nil {
		return fmt.Errorf("continuous dead-letter mode requires a dead-letter recorder")
	}
	getPlugin := r.GetPlugin
	if getPlugin == nil {
		getPlugin = engineplugin.Get
	}
	sourcePlugin, err := getPlugin(plan.SourceType)
	if err != nil {
		return err
	}
	source, ok := sourcePlugin.(engineplugin.ChangeStreamReaderProvider)
	if !ok {
		return fmt.Errorf("source engine %q does not implement ChangeStreamReaderProvider", plan.SourceType)
	}
	targetPlugin, err := getPlugin(plan.TargetType)
	if err != nil {
		return err
	}
	target, ok := targetPlugin.(engineplugin.PartitionedTableChangeApplyProvider)
	if !ok {
		return fmt.Errorf("target engine %q does not implement PartitionedTableChangeApplyProvider", plan.TargetType)
	}
	applyOptions := engineplugin.PartitionedTableChangeApplyOptions{
		ApplyIdentity: claim.Task.ApplyIdentity, SourceIdentity: plan.Source.SourceIdentity,
		Fields: plan.Target.Fields, SpatialInfo: plan.Target.SpatialInfo, Keys: plan.Target.Keys,
	}
	if err := target.PreparePartitionedTableChangeApply(ctx, plan.Target.ConnInfo, plan.Target.Path, applyOptions); err != nil {
		return fmt.Errorf("prepare continuous target: %w", err)
	}
	if claim.Task.AutoScanMetadata {
		if r.MetadataScanner == nil {
			return fmt.Errorf("continuous target metadata scanner is required when auto_scan_metadata is enabled")
		}
		if err := r.MetadataScanner.ScanPreparedTarget(ctx, claim, plan); err != nil {
			return fmt.Errorf("scan prepared continuous target metadata: %w", err)
		}
	}
	committed, committedAtByPartition, err := r.committedPositions(ctx, claim.Task.ID, plan.Source.SourceIdentity)
	if err != nil {
		return err
	}
	pollTimeout := r.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 5 * time.Second
	}
	consumerGroup := strings.TrimSpace(plan.Source.ConsumerGroup)
	if consumerGroup == "" {
		consumerGroup = "addp-transfer-" + claim.Task.ApplyIdentity
	}
	reader, err := source.OpenChangeStream(ctx, plan.Source.ConnInfo, plan.Source.Path, engineplugin.ChangeStreamReadOptions{
		ConsumerGroup: consumerGroup, CommittedPositions: committed,
		InitialPosition: plan.Source.InitialPosition, PollTimeout: pollTimeout, MaxBytes: r.MaxBytes,
	})
	if err != nil {
		return fmt.Errorf("open continuous source: %w", err)
	}
	defer reader.Close(context.Background())
	now := r.Now
	if now == nil {
		now = time.Now
	}
	diagnosticsInterval := r.DiagnosticsInterval
	if diagnosticsInterval <= 0 {
		diagnosticsInterval = 15 * time.Second
	}
	degradedHorizon := r.RetentionDegradedHorizon
	if degradedHorizon <= 0 {
		degradedHorizon = 6 * time.Hour
	}
	criticalHorizon := r.RetentionCriticalHorizon
	if criticalHorizon <= 0 {
		criticalHorizon = time.Hour
	}
	if criticalHorizon >= degradedHorizon {
		return fmt.Errorf("continuous retention critical horizon must be less than degraded horizon")
	}
	checkpointStaleAfter := r.CheckpointStaleAfter
	if checkpointStaleAfter <= 0 {
		checkpointStaleAfter = 5 * time.Minute
	}
	if checkpointStaleAfter <= diagnosticsInterval {
		return fmt.Errorf("continuous checkpoint stale threshold must be greater than diagnostics interval")
	}

	partitionStates := map[string]*models.SyncState{}
	latestSamples := map[string]sourceLatestSample{}
	var nextDiagnosticsAt time.Time
	var recordsRead, recordsWritten int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		currentTime := now()
		if nextDiagnosticsAt.IsZero() || !currentTime.Before(nextDiagnosticsAt) {
			diagnostics, nextSamples := collectContinuousDiagnostics(
				ctx, reader, committed, committedAtByPartition, latestSamples,
				currentTime, degradedHorizon, criticalHorizon, checkpointStaleAfter,
			)
			if err := r.Progress.RecordDiagnostics(ctx, claim, diagnostics); err != nil {
				return fmt.Errorf("record continuous diagnostics: %w", err)
			}
			latestSamples = nextSamples
			nextDiagnosticsAt = currentTime.Add(diagnosticsInterval)
		}
		batch, err := reader.Poll(ctx, plan.Source.PollBatchSize)
		if err != nil {
			return fmt.Errorf("poll continuous source: %w", err)
		}
		if batch == nil || len(batch.Records) == 0 {
			continue
		}
		groups := groupRecordsByPartition(batch.Records)
		for _, group := range groups {
			if err := ctx.Err(); err != nil {
				return err
			}
			recordsRead += int64(len(group.records))
			state := partitionStates[group.partition]
			if state == nil {
				state, err = r.States.ClaimContinuousPartition(
					ctx, claim.Task.ID, plan.Source.SourceIdentity, group.partition,
					engineplugin.ChangeStreamPositionTypeKafkaOffset, engineplugin.ChangeStreamPositionVersionV1,
					claim.Lease.OwnerInstanceID, claim.Lease.FencingToken,
				)
				if err != nil {
					return fmt.Errorf("claim continuous partition %q: %w", group.partition, err)
				}
				partitionStates[group.partition] = state
			}
			start, hasStart, err := syncStatePosition(state)
			if err != nil {
				return err
			}
			if !hasStart {
				start = kafkaOffsetPosition(group.partition, group.records[0].Offset)
			}
			changes, err := r.mapPartitionRecords(ctx, claim, group.records, start, plan, now)
			if err != nil {
				var schemaErr *SchemaChangeError
				if errors.As(err, &schemaErr) {
					if recorder, ok := r.Progress.(ContinuousSchemaChangeStore); ok {
						if recordErr := recorder.RecordSchemaChange(ctx, claim, repository.ContinuousSchemaChange{
							DetectedAt: now(), Scope: schemaErr.Scope,
							SourcePartition: schemaErr.SourcePartition, SourceOffset: schemaErr.SourceOffset,
							MissingFields:      append([]string(nil), schemaErr.MissingFields...),
							UnexpectedFields:   append([]string(nil), schemaErr.UnexpectedFields...),
							IncompatibleFields: append([]string(nil), schemaErr.IncompatibleFields...),
						}); recordErr != nil {
							return fmt.Errorf("record continuous schema change: %w", recordErr)
						}
					}
				}
				return fmt.Errorf("map continuous partition %q: %w", group.partition, err)
			}
			if len(changes) == 0 {
				committedAt := now()
				if err := r.Progress.RecordProgress(ctx, claim, repository.ContinuousProgress{
					RecordsRead: recordsRead, RecordsWritten: recordsWritten, Partition: group.partition,
					Position: positionJSON(start), CommittedAt: committedAt, LastEventAt: latestRecordTimestamp(group.records),
				}); err != nil {
					return fmt.Errorf("record continuous progress: %w", err)
				}
				continue
			}
			changeBatch := &engineplugin.PartitionedTableChangeBatch{
				Partition: group.partition, StartPosition: start,
				EndPosition: changes[len(changes)-1].Position, Changes: changes,
			}
			result, err := target.ApplyPartitionedTableChanges(ctx, plan.Target.ConnInfo, plan.Target.Path, changeBatch, applyOptions)
			if err != nil {
				return fmt.Errorf("apply continuous partition %q: %w", group.partition, err)
			}
			if result == nil {
				return fmt.Errorf("apply continuous partition %q returned nil result", group.partition)
			}
			if err := r.States.CommitContinuousPosition(
				ctx, state.ID, claim.Task.ID, state.StateVersion, claim.Lease.FencingToken,
				claim.Lease.OwnerInstanceID, positionJSON(result.Position), claim.Execution.ExecutionID,
			); err != nil {
				return fmt.Errorf("commit continuous partition %q: %w", group.partition, err)
			}
			state.StateVersion++
			state.Position = positionJSON(result.Position)
			committed[group.partition] = result.Position
			recordsWritten += int64(result.AppliedRecords)
			committedAt := now()
			committedAtByPartition[group.partition] = committedAt
			if err := r.Progress.RecordProgress(ctx, claim, repository.ContinuousProgress{
				RecordsRead: recordsRead, RecordsWritten: recordsWritten, Partition: group.partition,
				Position: state.Position, CommittedAt: committedAt, LastEventAt: latestRecordTimestamp(group.records),
			}); err != nil {
				return fmt.Errorf("record continuous progress: %w", err)
			}
		}
	}
}

func (r *DataSessionRunner) mapPartitionRecords(
	ctx context.Context,
	claim repository.RuntimeLeaseClaim,
	records []engineplugin.ChangeRecord,
	start engineplugin.ChangeStreamPosition,
	plan *planner.ContinuousPlan,
	now func() time.Time,
) ([]engineplugin.PartitionedTableChange, error) {
	if plan.RecordFailureMode != planner.RecordFailureModeDeadLetter {
		return mapPartitionRecords(records, start, plan)
	}
	changes := make([]engineplugin.PartitionedTableChange, 0, len(records))
	for _, record := range records {
		mapped, err := mapPartitionRecords([]engineplugin.ChangeRecord{record}, start, plan)
		if err == nil {
			changes = append(changes, mapped...)
			continue
		}
		var dataErr *RecordDataError
		if !errors.As(err, &dataErr) {
			return nil, err
		}
		if _, recordErr := r.DeadLetters.Record(ctx, deadletter.RecordRequest{
			TenantID: claim.Task.TenantID, TaskID: claim.Task.ID, ExecutionID: claim.Execution.ExecutionID,
			ApplyIdentity: claim.Task.ApplyIdentity, SourceIdentity: plan.Source.SourceIdentity,
			Record:     record,
			Error:      deadletter.ErrorDetail{Code: dataErr.Code, Category: dataErr.Category, Message: dataErr.Message},
			DetectedAt: now(),
		}); recordErr != nil {
			return nil, fmt.Errorf("persist dead-letter for source offset %d: %w", record.Offset, recordErr)
		}
		changes = append(changes, engineplugin.PartitionedTableChange{
			Operation: engineplugin.TableChangeOperationSkip, Position: record.Position,
		})
	}
	return changes, nil
}

func (r *DataSessionRunner) buildPlan(ctx context.Context, claim repository.RuntimeLeaseClaim) (*planner.ContinuousPlan, error) {
	resolver := planner.BindEngineResolver(r.Resolver, claim.Task.TenantID)
	if planner.IsDatabaseCDCTaskConfig(claim.Task.Config) {
		if r.Captures == nil {
			return nil, fmt.Errorf("database CDC data runner requires capture resource store")
		}
		if len(r.InfraKafkaConnection) == 0 {
			return nil, fmt.Errorf("database CDC data runner requires Infra Kafka connection")
		}
		resource, err := r.Captures.GetLatest(ctx, claim.Task.ID, claim.Task.TenantID)
		if err != nil {
			return nil, fmt.Errorf("load database CDC capture generation: %w", err)
		}
		if resource.Status != models.CaptureStatusRunning || !resource.TopicCreated || !resource.ConnectorCreated {
			return nil, fmt.Errorf("database CDC capture generation is not running")
		}
		spec, err := planner.ParseDatabaseCDCTaskSpec(claim.Task.Config)
		if err != nil {
			return nil, fmt.Errorf("parse database CDC task: %w", err)
		}
		return planner.BuildDatabaseCDCContinuousPlan(spec, resolver, planner.DatabaseCDCStreamBinding{
			Provider: string(resource.SourceType),
			ConnInfo: r.InfraKafkaConnection, Path: internalKafkaTopicPath(resource.TopicName),
			ConsumerGroup: resource.ConsumerGroup, SourceIdentity: resource.SourceIdentity,
			Database: resource.SourceDatabase, Schema: resource.SourceSchema, Table: resource.SourceTable,
			SpatialInfo: datatype.SpatialInfoFromPayload(map[string]interface{}(resource.SourceSpatialInfo)),
		}, claim.Task.BatchSize)
	}
	spec, err := planner.ParseContinuousTaskSpec(claim.Task.Config)
	if err != nil {
		return nil, fmt.Errorf("parse continuous task: %w", err)
	}
	plan, err := planner.BuildContinuousPlan(spec, resolver)
	if err != nil {
		return nil, fmt.Errorf("build continuous plan: %w", err)
	}
	return plan, nil
}

func internalKafkaTopicPath(topic string) engineplugin.CatalogPath {
	model := (&kafka.KafkaPlugin{}).CatalogModel()
	path := engineplugin.CatalogRootPath(model, 0)
	path.Segments = append(path.Segments, engineplugin.CatalogSegment{
		Term: kafka.CatalogTermTopic, Kind: kafka.CatalogKindTopic, Name: topic,
	})
	return path
}

func collectContinuousDiagnostics(
	ctx context.Context,
	reader engineplugin.ChangeStreamReader,
	committed map[string]engineplugin.ChangeStreamPosition,
	committedAt map[string]time.Time,
	previous map[string]sourceLatestSample,
	sampledAt time.Time,
	degradedHorizon time.Duration,
	criticalHorizon time.Duration,
	checkpointStaleAfter time.Duration,
) (repository.ContinuousDiagnostics, map[string]sourceLatestSample) {
	diagnostics := repository.ContinuousDiagnostics{
		SampledAt: sampledAt, Health: continuousHealthUnknown,
		DegradedHorizonSeconds: degradedHorizon.Seconds(), CriticalHorizonSeconds: criticalHorizon.Seconds(),
		CheckpointStaleAfterSeconds: checkpointStaleAfter.Seconds(), CheckpointHealth: continuousHealthUnknown,
		Partitions: map[string]repository.ContinuousPartitionDiagnostics{},
	}
	ranges, err := reader.PositionRanges(ctx)
	if err != nil {
		diagnostics.Error = err.Error()
		return diagnostics, previous
	}
	nextSamples := make(map[string]sourceLatestSample, len(ranges))
	overall := continuousHealthHealthy
	overallCheckpoint := continuousHealthHealthy
	for _, positionRange := range ranges {
		partition := positionRange.Partition
		earliest, earliestErr := kafkaNextOffset(positionRange.Earliest)
		latest, latestErr := kafkaNextOffset(positionRange.Latest)
		if earliestErr != nil || latestErr != nil || latest < earliest {
			diagnostics.Error = fmt.Sprintf("invalid source position range for partition %q", partition)
			return diagnostics, previous
		}
		partitionDiagnostics := repository.ContinuousPartitionDiagnostics{
			Partition: partition, EarliestOffset: earliest, LatestOffset: latest,
			Health: continuousHealthUnknown, CheckpointHealth: continuousHealthUnknown,
		}
		nextSamples[partition] = sourceLatestSample{Latest: latest, SampledAt: sampledAt}
		position, hasCommitted := committed[partition]
		if hasCommitted {
			nextOffset, positionErr := kafkaNextOffset(position)
			if positionErr != nil {
				diagnostics.Error = fmt.Sprintf("invalid committed position for partition %q: %v", partition, positionErr)
				return diagnostics, previous
			}
			lag := latest - nextOffset
			if lag < 0 {
				lag = 0
			}
			headroom := nextOffset - earliest
			partitionDiagnostics.NextOffset = &nextOffset
			partitionDiagnostics.LagRecords = &lag
			partitionDiagnostics.RecoveryHeadroomRecords = &headroom
			switch {
			case lag == 0:
				partitionDiagnostics.CheckpointHealth = continuousHealthHealthy
			case !committedAt[partition].IsZero() && !sampledAt.Before(committedAt[partition]):
				age := sampledAt.Sub(committedAt[partition]).Seconds()
				partitionDiagnostics.CheckpointAgeSeconds = &age
				if age > checkpointStaleAfter.Seconds() {
					partitionDiagnostics.CheckpointHealth = continuousHealthDegraded
				} else {
					partitionDiagnostics.CheckpointHealth = continuousHealthHealthy
				}
			}
			switch {
			case nextOffset < earliest || nextOffset > latest:
				partitionDiagnostics.Health = continuousHealthCritical
			case lag == 0:
				partitionDiagnostics.Health = continuousHealthHealthy
			default:
				if prior, ok := previous[partition]; ok && sampledAt.After(prior.SampledAt) && latest >= prior.Latest {
					rate := float64(latest-prior.Latest) / sampledAt.Sub(prior.SampledAt).Seconds()
					if rate > 0 {
						horizon := float64(headroom) / rate
						partitionDiagnostics.SourceRateRecordsPerSecond = &rate
						partitionDiagnostics.RetentionHorizonSeconds = &horizon
						switch {
						case horizon <= criticalHorizon.Seconds():
							partitionDiagnostics.Health = continuousHealthCritical
						case horizon <= degradedHorizon.Seconds():
							partitionDiagnostics.Health = continuousHealthDegraded
						default:
							partitionDiagnostics.Health = continuousHealthHealthy
						}
					}
				}
			}
		}
		diagnostics.Partitions[partition] = partitionDiagnostics
		overall = worseContinuousHealth(overall, partitionDiagnostics.Health)
		overallCheckpoint = worseContinuousHealth(overallCheckpoint, partitionDiagnostics.CheckpointHealth)
	}
	diagnostics.Health = overall
	diagnostics.CheckpointHealth = overallCheckpoint
	return diagnostics, nextSamples
}

func worseContinuousHealth(left, right string) string {
	rank := map[string]int{
		continuousHealthHealthy: 0, continuousHealthUnknown: 1,
		continuousHealthDegraded: 2, continuousHealthCritical: 3,
	}
	if rank[right] > rank[left] {
		return right
	}
	return left
}

func latestRecordTimestamp(records []engineplugin.ChangeRecord) *time.Time {
	var latest time.Time
	for _, record := range records {
		if record.Timestamp.After(latest) {
			latest = record.Timestamp
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func (r *DataSessionRunner) committedPositions(ctx context.Context, taskID uint, sourceIdentity string) (map[string]engineplugin.ChangeStreamPosition, map[string]time.Time, error) {
	states, err := r.States.List(ctx, taskID, sourceIdentity)
	if err != nil {
		return nil, nil, fmt.Errorf("list continuous committed positions: %w", err)
	}
	positions := make(map[string]engineplugin.ChangeStreamPosition, len(states))
	committedAt := make(map[string]time.Time, len(states))
	for i := range states {
		position, ok, err := syncStatePosition(&states[i])
		if err != nil {
			return nil, nil, err
		}
		if ok {
			positions[states[i].Partition] = position
			if states[i].PositionCommittedAt != nil {
				committedAt[states[i].Partition] = *states[i].PositionCommittedAt
			}
		}
	}
	return positions, committedAt, nil
}

type partitionRecordGroup struct {
	partition string
	records   []engineplugin.ChangeRecord
}

func groupRecordsByPartition(records []engineplugin.ChangeRecord) []partitionRecordGroup {
	groups := make([]partitionRecordGroup, 0)
	indexes := map[string]int{}
	for _, record := range records {
		index, ok := indexes[record.Partition]
		if !ok {
			index = len(groups)
			indexes[record.Partition] = index
			groups = append(groups, partitionRecordGroup{partition: record.Partition})
		}
		groups[index].records = append(groups[index].records, record)
	}
	return groups
}

func mapPartitionRecords(records []engineplugin.ChangeRecord, start engineplugin.ChangeStreamPosition, plan *planner.ContinuousPlan) ([]engineplugin.PartitionedTableChange, error) {
	if len(records) == 0 {
		return nil, nil
	}
	startOffset, err := kafkaNextOffset(start)
	if err != nil {
		return nil, err
	}
	changes := make([]engineplugin.PartitionedTableChange, 0, len(records))
	for _, record := range records {
		nextOffset, err := kafkaNextOffset(record.Position)
		if err != nil {
			return nil, err
		}
		if nextOffset <= startOffset {
			continue
		}
		operation := engineplugin.TableChangeOperationUpsert
		var row map[string]interface{}
		switch plan.Envelope {
		case planner.ContinuousEnvelopeRecord:
			row, err = decodeAndMapRecord(record, plan)
		case planner.ContinuousEnvelopePostgreSQLDebezium:
			var event *ChangeEvent
			event, err = decodePostgreSQLDebeziumRecord(record, plan)
			if err == nil {
				row = event.Row
				switch event.Operation {
				case changeEventOperationSnapshot, changeEventOperationUpsert:
					operation = engineplugin.TableChangeOperationUpsert
				case changeEventOperationDelete:
					operation = engineplugin.TableChangeOperationDelete
				default:
					err = fmt.Errorf("unsupported normalized change event operation %q", event.Operation)
				}
			}
		case planner.ContinuousEnvelopeMySQLDebezium:
			var event *ChangeEvent
			event, err = decodeMySQLDebeziumRecord(record, plan)
			if err == nil {
				row = event.Row
				switch event.Operation {
				case changeEventOperationSnapshot, changeEventOperationUpsert:
					operation = engineplugin.TableChangeOperationUpsert
				case changeEventOperationDelete:
					operation = engineplugin.TableChangeOperationDelete
				default:
					err = fmt.Errorf("unsupported normalized change event operation %q", event.Operation)
				}
			}
		case planner.ContinuousEnvelopeOracleDebezium:
			var event *ChangeEvent
			event, err = decodeOracleDebeziumRecord(record, plan)
			if err == nil {
				row = event.Row
				switch event.Operation {
				case changeEventOperationSnapshot, changeEventOperationUpsert:
					operation = engineplugin.TableChangeOperationUpsert
				case changeEventOperationDelete:
					operation = engineplugin.TableChangeOperationDelete
				default:
					err = fmt.Errorf("unsupported normalized change event operation %q", event.Operation)
				}
			}
		default:
			err = fmt.Errorf("unsupported continuous envelope %q", plan.Envelope)
		}
		if err != nil {
			var schemaErr *SchemaChangeError
			if errors.As(err, &schemaErr) {
				schemaErr.SourcePartition = record.Partition
				schemaErr.SourceOffset = record.Offset
			}
			return nil, fmt.Errorf("offset %d: %w", record.Offset, err)
		}
		changes = append(changes, engineplugin.PartitionedTableChange{
			Operation: operation, Position: record.Position, Row: row,
		})
	}
	return changes, nil
}

func decodeAndMapRecord(record engineplugin.ChangeRecord, plan *planner.ContinuousPlan) (map[string]interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(record.Value))
	decoder.UseNumber()
	var source map[string]interface{}
	if err := decoder.Decode(&source); err != nil {
		return nil, newRecordDataError("invalid_json_object", recordErrorCategoryDecode, "record value must be a JSON object", err)
	}
	if source == nil {
		return nil, newRecordDataError("invalid_json_object", recordErrorCategoryDecode, "record value must be a JSON object", nil)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, newRecordDataError("multiple_json_values", recordErrorCategoryDecode, "record value must contain exactly one JSON object", nil)
	}
	allowed := make(map[string]bool, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		allowed[mapping.Source] = true
	}
	unknownFields := make([]string, 0)
	for field := range source {
		if !allowed[field] {
			unknownFields = append(unknownFields, field)
		}
	}
	if len(unknownFields) > 0 {
		sort.Strings(unknownFields)
		return nil, newRecordDataError("unknown_source_field", recordErrorCategoryFieldValidation, fmt.Sprintf("unknown source field %q", unknownFields[0]), nil)
	}
	for _, key := range plan.SourceKeys {
		value, ok := source[key]
		if !ok || value == nil {
			return nil, newRecordDataError("missing_source_key", recordErrorCategoryKeyValidation, fmt.Sprintf("missing non-null source key field %q", key), nil)
		}
	}
	row := make(map[string]interface{}, len(plan.Mappings))
	for _, mapping := range plan.Mappings {
		value, ok := source[mapping.Source]
		if !ok || value == nil {
			if mapping.Default != nil {
				value = mapping.Default
			} else if mapping.Nullable {
				row[mapping.Target] = nil
				continue
			} else {
				return nil, newRecordDataError("missing_required_field", recordErrorCategoryFieldValidation, fmt.Sprintf("missing required source field %q", mapping.Source), nil)
			}
		}
		converted, err := coerceContinuousValue(value, mapping.Type)
		if err != nil {
			return nil, newRecordDataError("incompatible_field_type", recordErrorCategoryTypeConversion, fmt.Sprintf("source field %q is incompatible with target type %q", mapping.Source, mapping.Type), err)
		}
		row[mapping.Target] = converted
	}
	for _, key := range plan.Target.Keys {
		if value, ok := row[key]; !ok || value == nil {
			return nil, newRecordDataError("null_target_key", recordErrorCategoryKeyValidation, fmt.Sprintf("mapped target key field %q is null", key), nil)
		}
	}
	return row, nil
}

func coerceContinuousValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	switch fieldType {
	case datatype.FieldTypeString:
		if typed, ok := value.(string); ok {
			return typed, nil
		}
	case datatype.FieldTypeBool:
		if typed, ok := value.(bool); ok {
			return typed, nil
		}
	case datatype.FieldTypeInt:
		integer, err := continuousInt64(value)
		if err == nil && integer >= math.MinInt32 && integer <= math.MaxInt32 {
			return int32(integer), nil
		}
		if err == nil {
			err = fmt.Errorf("integer is outside int32 range")
		}
		return nil, err
	case datatype.FieldTypeBigInt:
		return continuousInt64(value)
	case datatype.FieldTypeFloat, datatype.FieldTypeDouble:
		return continuousFloat64(value)
	case datatype.FieldTypeDecimal:
		switch typed := value.(type) {
		case string:
			if _, _, err := big.ParseFloat(typed, 10, 256, big.ToNearestEven); err == nil {
				return typed, nil
			}
		case json.Number:
			if _, _, err := big.ParseFloat(typed.String(), 10, 256, big.ToNearestEven); err == nil {
				return typed.String(), nil
			}
		case float64:
			if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
				return strconv.FormatFloat(typed, 'g', -1, 64), nil
			}
		}
	case datatype.FieldTypeDate:
		if typed, ok := value.(string); ok {
			if _, err := time.Parse("2006-01-02", typed); err == nil {
				return typed, nil
			}
		}
		if days, err := continuousInt64(value); err == nil && days >= -719162 && days <= 2932896 {
			return time.Unix(days*24*60*60, 0).UTC().Format("2006-01-02"), nil
		}
	case datatype.FieldTypeTime:
		if typed, ok := value.(string); ok {
			for _, layout := range []string{"15:04:05", "15:04:05.999999999"} {
				if _, err := time.Parse(layout, typed); err == nil {
					return typed, nil
				}
			}
		}
		if milliseconds, err := continuousInt64(value); err == nil && milliseconds >= 0 && milliseconds < 24*60*60*1000 {
			return time.UnixMilli(milliseconds).UTC().Format("15:04:05.999"), nil
		}
	case datatype.FieldTypeTimestamp:
		if typed, ok := value.(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return parsed, nil
			}
		}
		if milliseconds, err := continuousInt64(value); err == nil {
			return time.UnixMilli(milliseconds).UTC(), nil
		}
	case datatype.FieldTypeUUID:
		if typed, ok := value.(string); ok {
			if _, err := uuid.Parse(typed); err == nil {
				return typed, nil
			}
		}
	case datatype.FieldTypeJSON:
		if typed, ok := value.(string); ok && json.Valid([]byte(typed)) {
			return typed, nil
		}
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded), nil
		}
	}
	return nil, fmt.Errorf("value %T is incompatible with target_type %q", value, fieldType)
}

func continuousInt64(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		if math.Trunc(typed) == typed && typed >= math.MinInt64 && typed <= math.MaxInt64 {
			return int64(typed), nil
		}
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	}
	return 0, fmt.Errorf("value %T is not an integer", value)
}

func continuousFloat64(value interface{}) (float64, error) {
	var result float64
	var err error
	switch typed := value.(type) {
	case json.Number:
		result, err = typed.Float64()
	case float64:
		result = typed
	case int:
		result = float64(typed)
	case int64:
		result = float64(typed)
	default:
		err = fmt.Errorf("value %T is not numeric", value)
	}
	if err != nil {
		return 0, err
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, fmt.Errorf("numeric value must be finite")
	}
	return result, nil
}

func syncStatePosition(state *models.SyncState) (engineplugin.ChangeStreamPosition, bool, error) {
	if state == nil || len(state.Position) == 0 {
		return engineplugin.ChangeStreamPosition{}, false, nil
	}
	encoded, err := json.Marshal(state.Position)
	if err != nil {
		return engineplugin.ChangeStreamPosition{}, false, err
	}
	var position engineplugin.ChangeStreamPosition
	if err := json.Unmarshal(encoded, &position); err != nil {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("decode sync state partition %q position: %w", state.Partition, err)
	}
	if _, err := kafkaNextOffset(position); err != nil {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("invalid sync state partition %q position: %w", state.Partition, err)
	}
	if position.Partition != state.Partition {
		return engineplugin.ChangeStreamPosition{}, false, fmt.Errorf("sync state position partition %q does not match row partition %q", position.Partition, state.Partition)
	}
	return position, true, nil
}

func positionJSON(position engineplugin.ChangeStreamPosition) models.JSONMap {
	values := make(map[string]string, len(position.Values))
	for key, value := range position.Values {
		values[key] = value
	}
	return models.JSONMap{
		"type": position.Type, "version": position.Version, "partition": position.Partition, "values": values,
	}
}

func kafkaNextOffset(position engineplugin.ChangeStreamPosition) (int64, error) {
	if position.Type != engineplugin.ChangeStreamPositionTypeKafkaOffset || position.Version != engineplugin.ChangeStreamPositionVersionV1 {
		return 0, fmt.Errorf("unsupported position %s/%s", position.Type, position.Version)
	}
	if strings.TrimSpace(position.Partition) == "" {
		return 0, fmt.Errorf("position partition is required")
	}
	next, err := strconv.ParseInt(position.Values["next_offset"], 10, 64)
	if err != nil || next < 0 {
		return 0, fmt.Errorf("position requires non-negative next_offset")
	}
	return next, nil
}

func kafkaOffsetPosition(partition string, nextOffset int64) engineplugin.ChangeStreamPosition {
	return engineplugin.ChangeStreamPosition{
		Type: engineplugin.ChangeStreamPositionTypeKafkaOffset, Version: engineplugin.ChangeStreamPositionVersionV1,
		Partition: partition, Values: map[string]string{"next_offset": strconv.FormatInt(nextOffset, 10)},
	}
}
