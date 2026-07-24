package deadletter

import (
	"context"
	"fmt"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/transfer/internal/models"
)

type PayloadReference struct {
	Topic     string
	Partition int32
	Offset    int64
}

type PayloadSink interface {
	Write(ctx context.Context, topic, identity string, detectedAt time.Time, payload []byte) (PayloadReference, error)
}

type IndexStore interface {
	UpsertObservation(ctx context.Context, observation *models.DeadLetter) (*models.DeadLetter, error)
}

type RecordRequest struct {
	TenantID       uint
	TaskID         uint
	ExecutionID    string
	ApplyIdentity  string
	SourceIdentity string
	Record         engineplugin.ChangeRecord
	Error          ErrorDetail
	DetectedAt     time.Time
}

type Recorder struct {
	payloads PayloadSink
	index    IndexStore
}

func NewRecorder(payloads PayloadSink, index IndexStore) (*Recorder, error) {
	if payloads == nil || index == nil {
		return nil, fmt.Errorf("dead-letter payload and index stores are required")
	}
	return &Recorder{payloads: payloads, index: index}, nil
}

// Record 严格执行 Kafka payload -> Infra PostgreSQL control index。
// 目标 skip ledger 和 sync-state CAS 由 continuous runtime 在本方法成功返回后依次执行。
func (r *Recorder) Record(ctx context.Context, request RecordRequest) (*models.DeadLetter, error) {
	identity, err := IdentityForSourceRecord(request.ApplyIdentity, request.SourceIdentity, request.Record.Partition, request.Record.Offset)
	if err != nil {
		return nil, err
	}
	detectedAt := request.DetectedAt.UTC()
	if detectedAt.IsZero() {
		return nil, fmt.Errorf("dead-letter detected_at is required")
	}
	envelope, err := NewEnvelope(EnvelopeInput{
		Identity: identity.String(), TenantID: request.TenantID, TaskID: request.TaskID,
		ExecutionID: request.ExecutionID, ApplyIdentity: request.ApplyIdentity, SourceIdentity: request.SourceIdentity,
		Record: request.Record, Error: request.Error, DetectedAt: detectedAt,
	})
	if err != nil {
		return nil, err
	}
	payload, err := MarshalEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	topic, err := TopicName(request.TenantID, request.TaskID)
	if err != nil {
		return nil, err
	}
	reference, err := r.payloads.Write(ctx, topic, identity.String(), detectedAt, payload)
	if err != nil {
		return nil, fmt.Errorf("write transfer dead-letter payload: %w", err)
	}
	if reference.Topic != topic || reference.Partition < 0 || reference.Offset < 0 {
		return nil, fmt.Errorf("write transfer dead-letter payload returned an invalid Kafka reference")
	}

	var sourceTimestamp *time.Time
	if !request.Record.Timestamp.IsZero() {
		value := request.Record.Timestamp.UTC()
		sourceTimestamp = &value
	}
	observation := &models.DeadLetter{
		Identity: identity.String(), TenantID: request.TenantID, TaskID: request.TaskID, ApplyIdentity: request.ApplyIdentity,
		FirstExecutionID: request.ExecutionID, LastExecutionID: request.ExecutionID,
		SourceIdentity: request.SourceIdentity, SourceTopic: request.Record.Topic, SourcePartition: request.Record.Partition,
		SourceOffset: request.Record.Offset, SourceTimestamp: sourceTimestamp,
		ErrorCode: request.Error.Code, ErrorCategory: request.Error.Category, ErrorMessage: request.Error.Message,
		PayloadTopic: reference.Topic, PayloadPartition: reference.Partition, PayloadOffset: reference.Offset, PayloadAvailable: true,
		FirstObservedAt: detectedAt, LastObservedAt: detectedAt, OccurrenceCount: 1,
	}
	stored, err := r.index.UpsertObservation(ctx, observation)
	if err != nil {
		return nil, fmt.Errorf("write transfer dead-letter control index: %w", err)
	}
	return stored, nil
}
