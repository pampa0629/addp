package deadletter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
)

const EnvelopeSchemaV1 = "transfer.dead_letter/v1"

type ErrorDetail struct {
	Code     string `json:"code"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

type EnvelopeSource struct {
	Identity  string     `json:"identity"`
	Topic     string     `json:"topic"`
	Partition string     `json:"partition"`
	Offset    int64      `json:"offset"`
	Timestamp *time.Time `json:"timestamp"`
}

type EnvelopeHeader struct {
	Key         string  `json:"key"`
	ValueBase64 *string `json:"value_base64"`
}

type EnvelopeRecord struct {
	KeyBase64   *string          `json:"key_base64"`
	ValueBase64 *string          `json:"value_base64"`
	Headers     []EnvelopeHeader `json:"headers"`
}

type Envelope struct {
	Schema        string         `json:"schema"`
	Identity      string         `json:"identity"`
	TenantID      uint           `json:"tenant_id"`
	TaskID        uint           `json:"task_id"`
	ExecutionID   string         `json:"execution_id"`
	ApplyIdentity string         `json:"apply_identity"`
	Source        EnvelopeSource `json:"source"`
	Record        EnvelopeRecord `json:"record"`
	Error         ErrorDetail    `json:"error"`
	DetectedAt    time.Time      `json:"detected_at"`
}

type EnvelopeInput struct {
	Identity       string
	TenantID       uint
	TaskID         uint
	ExecutionID    string
	ApplyIdentity  string
	SourceIdentity string
	Record         engineplugin.ChangeRecord
	Error          ErrorDetail
	DetectedAt     time.Time
}

func NewEnvelope(input EnvelopeInput) (*Envelope, error) {
	if strings.TrimSpace(input.Identity) == "" || input.TenantID == 0 || input.TaskID == 0 {
		return nil, fmt.Errorf("dead-letter identity, tenant, and task are required")
	}
	if strings.TrimSpace(input.ExecutionID) == "" || strings.TrimSpace(input.ApplyIdentity) == "" || strings.TrimSpace(input.SourceIdentity) == "" {
		return nil, fmt.Errorf("dead-letter execution, apply identity, and source identity are required")
	}
	if strings.TrimSpace(input.Record.Topic) == "" || strings.TrimSpace(input.Record.Partition) == "" || input.Record.Offset < 0 {
		return nil, fmt.Errorf("dead-letter source topic, partition, and non-negative offset are required")
	}
	if strings.TrimSpace(input.Error.Code) == "" || strings.TrimSpace(input.Error.Category) == "" || strings.TrimSpace(input.Error.Message) == "" {
		return nil, fmt.Errorf("dead-letter stable error code, category, and safe message are required")
	}
	if input.DetectedAt.IsZero() {
		return nil, fmt.Errorf("dead-letter detected_at is required")
	}

	var sourceTimestamp *time.Time
	if !input.Record.Timestamp.IsZero() {
		value := input.Record.Timestamp.UTC()
		sourceTimestamp = &value
	}
	headers := make([]EnvelopeHeader, 0, len(input.Record.Headers))
	for _, header := range input.Record.Headers {
		headers = append(headers, EnvelopeHeader{Key: header.Key, ValueBase64: encodeBytes(header.Value)})
	}
	return &Envelope{
		Schema: EnvelopeSchemaV1, Identity: input.Identity,
		TenantID: input.TenantID, TaskID: input.TaskID, ExecutionID: input.ExecutionID, ApplyIdentity: input.ApplyIdentity,
		Source: EnvelopeSource{
			Identity: input.SourceIdentity, Topic: input.Record.Topic, Partition: input.Record.Partition,
			Offset: input.Record.Offset, Timestamp: sourceTimestamp,
		},
		Record: EnvelopeRecord{KeyBase64: encodeBytes(input.Record.Key), ValueBase64: encodeBytes(input.Record.Value), Headers: headers},
		Error:  input.Error, DetectedAt: input.DetectedAt.UTC(),
	}, nil
}

func MarshalEnvelope(envelope *Envelope) ([]byte, error) {
	if envelope == nil || envelope.Schema != EnvelopeSchemaV1 {
		return nil, fmt.Errorf("transfer dead-letter v1 envelope is required")
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("marshal transfer dead-letter envelope: %w", err)
	}
	return data, nil
}

func encodeBytes(value []byte) *string {
	if value == nil {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString(value)
	return &encoded
}
