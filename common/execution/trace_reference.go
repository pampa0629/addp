package execution

import (
	"encoding/json"

	"github.com/addp/common/execution/observability"
	"github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
)

// TraceReference is the single pure-value contract, also used by the SDK.
type TraceReference = observability.TraceReference

const TraceSchema = observability.Schema
const ObservabilityKey = "observability"

func traceValue(value any, field string) (*TraceReference, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, observability.ErrReference
	}
	object, err := jsonmap.DecodeObjectStrict(raw, observability.MaxReferenceBytes+128, 4)
	if err != nil || len(object) != 2 || object["schema"] != TraceSchema {
		return nil, observability.ErrReference
	}
	reference, ok := object[field]
	if !ok {
		return nil, observability.ErrReference
	}
	encoded, err := json.Marshal(reference)
	if err != nil {
		return nil, observability.ErrReference
	}
	return observability.ParseTraceReference(encoded)
}

// EnqueueTrace and LatestTrace read only the authoritative persisted namespace.
// Missing values are ordinary for executions predating optional trace support.
func EnqueueTrace(item *TaskExecution) (*TraceReference, error) {
	if item == nil {
		return nil, observability.ErrReference
	}
	value, ok := item.ExecutionConfig[ObservabilityKey]
	if !ok {
		return nil, nil
	}
	return traceValue(value, "enqueue")
}
func LatestTrace(item *TaskExecution) (*TraceReference, error) {
	if item == nil {
		return nil, observability.ErrReference
	}
	value, ok := item.Metadata[ObservabilityKey]
	if !ok {
		return nil, nil
	}
	return traceValue(value, "latest")
}

// FreezeEnqueueTrace copies the namespace and never replaces an existing fact.
// Callers own the original create transaction and strip request-owned namespaces.
func FreezeEnqueueTrace(item *TaskExecution, reference *TraceReference) error {
	if item == nil {
		return observability.ErrReference
	}
	if reference == nil {
		return nil
	}
	if reference.Validate() != nil {
		return observability.ErrReference
	}
	if _, exists := item.ExecutionConfig[ObservabilityKey]; exists {
		prior, err := EnqueueTrace(item)
		if err != nil || prior == nil || prior.TraceID != reference.TraceID || prior.SpanID != reference.SpanID ||
			prior.ModuleName != reference.ModuleName || prior.Role != reference.Role || prior.InstanceID != reference.InstanceID || !prior.StartedAt.Equal(reference.StartedAt) {
			return observability.ErrReference
		}
		return nil
	}
	config := make(models.JSONMap, len(item.ExecutionConfig)+1)
	for key, value := range item.ExecutionConfig {
		config[key] = value
	}
	config[ObservabilityKey] = map[string]any{"schema": TraceSchema, "enqueue": *reference}
	item.ExecutionConfig = config
	return nil
}

// LatestTraceMetadata prepares a copy; it performs no write or authorization.
// Owners persist it with the actual state transition under the original lease.
func LatestTraceMetadata(metadata models.JSONMap, reference *TraceReference) (models.JSONMap, error) {
	if reference == nil {
		return metadata, nil
	}
	if reference.Validate() != nil {
		return nil, observability.ErrReference
	}
	if value, exists := metadata[ObservabilityKey]; exists {
		if _, err := traceValue(value, "latest"); err != nil {
			return nil, err
		}
	}
	result := make(models.JSONMap, len(metadata)+1)
	for key, value := range metadata {
		result[key] = value
	}
	result[ObservabilityKey] = map[string]any{"schema": TraceSchema, "latest": *reference}
	return result, nil
}
