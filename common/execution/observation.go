package execution

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// Observation deliberately does not embed TaskExecution. Adding an internal
// model field must never expand the tenant-facing diagnostic contract.
type Observation struct {
	ID                     int64            `json:"id"`
	ExecutionID            string           `json:"execution_id"`
	Module                 string           `json:"module"`
	TaskType               string           `json:"task_type"`
	Source                 string           `json:"source"`
	SourceTaskID           *string          `json:"source_task_id,omitempty"`
	SourceTaskName         *string          `json:"source_task_name,omitempty"`
	ParentExecutionID      *string          `json:"parent_execution_id,omitempty"`
	Status                 string           `json:"status"`
	Progress               int              `json:"progress"`
	CurrentStep            *string          `json:"current_step,omitempty"`
	ExecutionBoundary      string           `json:"execution_boundary"`
	RetryOfExecutionID     *string          `json:"retry_of_execution_id,omitempty"`
	Attempt                int              `json:"attempt"`
	MaxAttempts            int              `json:"max_attempts"`
	TriggerType            string           `json:"trigger_type"`
	TriggeredBy            *int             `json:"triggered_by,omitempty"`
	StartedAt              *time.Time       `json:"started_at,omitempty"`
	CompletedAt            *time.Time       `json:"completed_at,omitempty"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
	ExecutionTimeMs        *int64           `json:"execution_time_ms,omitempty"`
	RowsAffected           *int64           `json:"rows_affected,omitempty"`
	RecordsRead            *int64           `json:"records_read,omitempty"`
	RecordsWritten         *int64           `json:"records_written,omitempty"`
	BytesRead              *int64           `json:"bytes_read,omitempty"`
	BytesWritten           *int64           `json:"bytes_written,omitempty"`
	Metadata               models.JSONMap   `json:"metadata"`
	ErrorDetails           models.JSONMap   `json:"error_details,omitempty"`
	Steps                  []DiagnosticStep `json:"steps"`
	StepsAttemptUnverified bool             `json:"steps_attempt_unverified"`
	StepsTruncated         bool             `json:"steps_truncated"`
	DiagnosticsTruncated   bool             `json:"diagnostics_truncated"`
}

type DiagnosticStep struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Phase     string `json:"phase,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	Duration  *int64 `json:"duration,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

var diagnosticCredentials = regexp.MustCompile(`(?i)(bearer\s+)[^\s,;]+|((?:password|passwd|pwd|token|secret|api[_-]?key|authorization)\s*[=:]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;]+)|[a-z][a-z0-9+.-]*://[^\s]+`)
var diagnosticQuotedValues = regexp.MustCompile(`'[^']*'|"[^"]*"|[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}|\b1[3-9][0-9]{9}\b`)

// SafeDiagnosticText is an output defense. Owners must still produce messages
// without row values or credentials; arbitrary raw runtime bodies are excluded.
func SafeDiagnosticText(value string) string {
	value = diagnosticCredentials.ReplaceAllString(value, "[redacted]")
	value = diagnosticQuotedValues.ReplaceAllString(value, "[redacted]")
	runes := []rune(value)
	if len(runes) > 1024 {
		return string(runes[:1024]) + "… [truncated]"
	}
	return string(runes)
}

func safeStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := SafeDiagnosticText(*value)
	return &copyValue
}

func Observe(item *TaskExecution) *Observation {
	if item == nil {
		return nil
	}
	steps, truncated := diagnosticSteps(item.Metadata["step_results"])
	metadata, diagnosticsTruncated := safeExecutionMetadata(item.Metadata)
	errorDetails := models.JSONMap{}
	if item.Status == ExecutionStatusFailed || item.Status == ExecutionStatusTimeout || item.Status == ExecutionStatusCancelled {
		errorDetails["category"] = FailureCategory(item.ErrorDetails)
		if item.Status == ExecutionStatusTimeout {
			errorDetails["category"] = "timeout"
		}
		if item.Status == ExecutionStatusCancelled {
			errorDetails["category"] = "cancelled"
		}
		for _, key := range []string{"code", "error_code"} {
			if value, ok := item.ErrorDetails[key].(string); ok && diagnosticCode.MatchString(value) && !strings.Contains(strings.ToLower(value), "secret") {
				errorDetails[key] = value
			}
		}
		if value := diagnosticScalar(item.ErrorDetails["failed_targets_count"]); value != nil {
			if _, isText := value.(string); !isText {
				errorDetails["failed_targets_count"] = value
			}
		}
	}
	return &Observation{
		ID: item.ID, ExecutionID: item.ExecutionID, Module: item.Module, TaskType: item.TaskType, Source: item.Source,
		SourceTaskID: item.SourceTaskID, SourceTaskName: safeStringPointer(item.SourceTaskName), ParentExecutionID: item.ParentExecutionID,
		Status: item.Status, Progress: item.Progress, CurrentStep: diagnosticCurrentStep(item), ExecutionBoundary: item.ExecutionBoundary,
		RetryOfExecutionID: item.RetryOfExecutionID, Attempt: item.Attempt, MaxAttempts: item.MaxAttempts, TriggerType: item.TriggerType,
		TriggeredBy: item.TriggeredBy, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		ExecutionTimeMs: item.ExecutionTimeMs, RowsAffected: item.RowsAffected, RecordsRead: item.RecordsRead, RecordsWritten: item.RecordsWritten,
		BytesRead: item.BytesRead, BytesWritten: item.BytesWritten, Metadata: metadata, ErrorDetails: errorDetails,
		Steps: steps, StepsTruncated: truncated, DiagnosticsTruncated: diagnosticsTruncated, StepsAttemptUnverified: item.Attempt > 1 && len(steps) > 0,
	}
}

func diagnosticScalar(value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return SafeDiagnosticText(typed)
	case bool, int, int32, int64, uint, uint32, uint64, float32, float64, json.Number:
		return value
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return nil
	}
}

// This set contains only observation fields, not an Owner Permission registry.
var diagnosticMetadataScalars = strings.Fields(`source_srid target_srid extent_srid min_zoom max_zoom total_tiles generated_tiles cached_tiles failed_tiles stop_reason target_schema target_table target_kind target_geometry_column row_count_estimate analyze_executed storage_type catalog_nodes_scanned items_scanned fields_scanned finding_count quality_score recovery_reason recovered_from_execution_id recovery_attempt recovery_consecutive_failures recovery_not_before recovery_circuit_state`)

func safeExecutionMetadata(metadata models.JSONMap) (models.JSONMap, bool) {
	result := models.JSONMap{}
	truncated := false
	for _, key := range diagnosticMetadataScalars {
		if value := diagnosticScalar(metadata[key]); value != nil {
			result[key] = value
		}
	}
	if facts, ok := metadata["lineage_facts"]; ok {
		data, err := json.Marshal(facts)
		var lineage LineageFacts
		if err == nil && json.Unmarshal(data, &lineage) == nil && lineage.SchemaVersion == LineageFactsSchemaVersion {
			for i := range lineage.Inputs {
				lineage.Inputs[i] = safeLineageResource(lineage.Inputs[i])
			}
			for i := range lineage.Outputs {
				lineage.Outputs[i] = safeLineageResource(lineage.Outputs[i])
			}
			lineage.Operations = nil
			lineage.RuntimeExecutionID = SafeDiagnosticText(lineage.RuntimeExecutionID)
			if len(lineage.MetaScanRefs) > 100 {
				lineage.MetaScanRefs = lineage.MetaScanRefs[:100]
				truncated = true
			}
			for i := range lineage.MetaScanRefs {
				lineage.MetaScanRefs[i] = SafeDiagnosticText(lineage.MetaScanRefs[i])
			}
			if len(lineage.Inputs) > 100 {
				lineage.Inputs = lineage.Inputs[:100]
				truncated = true
			}
			if len(lineage.Outputs) > 100 {
				lineage.Outputs = lineage.Outputs[:100]
				truncated = true
			}
			result["lineage_facts"] = lineage
		}
	}
	if target, ok := asDiagnosticObject(metadata["tile_generation_target"]); ok {
		fields := models.JSONMap{}
		for _, key := range strings.Fields("schema table geom_column srid target_kind optimization_recommended optimization_recommendation") {
			if value := diagnosticScalar(target[key]); value != nil {
				fields[key] = value
			}
		}
		result["tile_generation_target"] = fields
	}
	if continuous, ok := metadata["continuous"]; ok {
		result["continuous"] = safeContinuous(continuous, 0, &truncated)
	}
	if extraction, ok := metadata["extraction"]; ok {
		if object, ok := asDiagnosticObject(extraction); ok {
			counts := models.JSONMap{}
			for _, key := range strings.Fields("documents extracted unsupported failed indexed index_failed") {
				if value := diagnosticScalar(object[key]); value != nil {
					counts[key] = value
				}
			}
			result["extraction"] = counts
		}
	}
	if object, ok := asDiagnosticObject(metadata["result"]); ok {
		if runtime, ok := asDiagnosticObject(object["runtime_status"]); ok {
			summary := models.JSONMap{}
			for _, key := range strings.Fields("status runtime_execution_id progress error_code started_at execution_time_ms") {
				if value := diagnosticScalar(runtime[key]); value != nil {
					summary[key] = value
				}
			}
			result["runtime_status"] = summary
		}
	}
	return result, truncated
}

var continuousKeys = map[string]bool{}

func init() {
	for _, key := range strings.Fields(`schema_version request_id from_revision to_revision detected_at scope source_partition source_offset missing_fields unexpected_fields incompatible_fields diagnostics capture values positions partitions health checkpoint_health sampled_at observed_at sample_time runtime_state apply_mode active_partition_count records_read records_written bytes_read bytes_written last_committed_at last_event_at owner_instance_id checkpoint_stale_after_seconds generation provider source_recovery source_transactions status unavailable_reason capture_position current_position earliest_available_position position_headroom window_seconds earliest_available_at fra_used_percent fra_reclaimable_percent active_count longest_duration_seconds undo_blocks undo_bytes partition earliest_offset latest_offset next_offset lag_records recovery_headroom_records source_rate_records_per_second retention_horizon_seconds checkpoint_age_seconds committed_position retention_health type version error schema_change source_kind position next_offset partition_id earliest latest committed lag headroom source_rate retention_horizon checkpoint_age checkpoint_stale_after_seconds`) {
		continuousKeys[key] = true
	}
}

func safeContinuous(value interface{}, depth int, truncated *bool) interface{} {
	if depth > 8 {
		*truncated = true
		return nil
	}
	if object, ok := asDiagnosticObject(value); ok {
		result := models.JSONMap{}
		for key, value := range object {
			if continuousKeys[key] {
				if key == "error" || key == "unavailable_reason" {
					if text, ok := value.(string); ok && text != "" {
						result[key] = FailureCategory(models.JSONMap{"message": text})
					}
					continue
				}
				if key == "partitions" {
					if partitions, ok := asDiagnosticObject(value); ok {
						projected := models.JSONMap{}
						names := make([]string, 0, len(partitions))
						for name := range partitions {
							names = append(names, name)
						}
						sort.Strings(names)
						if len(names) > 200 {
							names = names[:200]
							*truncated = true
						}
						for _, name := range names {
							projected[SafeDiagnosticText(name)] = safeContinuous(partitions[name], depth+1, truncated)
						}
						result[key] = projected
						continue
					}
				}
				result[key] = safeContinuous(value, depth+1, truncated)
			}
		}
		return result
	}
	if list, ok := value.([]interface{}); ok {
		if len(list) > 200 {
			list = list[:200]
			*truncated = true
		}
		result := make([]interface{}, 0, len(list))
		for _, item := range list {
			result = append(result, safeContinuous(item, depth+1, truncated))
		}
		return result
	}
	return diagnosticScalar(value)
}

func asDiagnosticObject(value interface{}) (map[string]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if object, ok := value.(map[string]interface{}); ok {
		return object, true
	}
	if object, ok := value.(models.JSONMap); ok {
		return map[string]interface{}(object), true
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var object map[string]interface{}
	if json.Unmarshal(data, &object) != nil {
		return nil, false
	}
	return object, object != nil
}

func diagnosticSteps(value interface{}) ([]DiagnosticStep, bool) {
	object, ok := asDiagnosticObject(value)
	steps := []DiagnosticStep{}
	if !ok {
		return steps, false
	}
	ids := make([]string, 0, len(object))
	for id := range object {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	truncated := len(ids) > 200
	if truncated {
		ids = ids[:200]
	}
	for _, id := range ids {
		entry, ok := asDiagnosticObject(object[id])
		if !ok {
			continue
		}
		step := DiagnosticStep{ID: SafeDiagnosticText(id), Status: "unknown"}
		if status, ok := entry["status"].(string); ok {
			switch status {
			case "pending", "running", "success", "failed", "timeout", "cancelled", "skipped":
				step.Status = status
			}
		}
		if v, ok := entry["error"].(string); ok && (step.Status == "failed" || step.Status == "timeout" || step.Status == "cancelled") {
			step.ErrorCode = FailureCategory(models.JSONMap{"code": entry["error_code"], "message": v})
		}
		if phase, ok := entry["phase"].(string); ok {
			switch phase {
			case "dispatching", "waiting", "terminal":
				step.Phase = phase
			}
		}
		for _, key := range []string{"started_at", "ended_at"} {
			if v := diagnosticScalar(entry[key]); v != nil {
				if text, ok := v.(string); ok {
					if key == "started_at" {
						step.StartedAt = text
					} else {
						step.EndedAt = text
					}
				}
			}
		}
		if data, err := json.Marshal(entry["duration"]); err == nil {
			var duration int64
			if entry["duration"] != nil && json.Unmarshal(data, &duration) == nil && duration >= 0 {
				step.Duration = &duration
			}
		}
		steps = append(steps, step)
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].StartedAt == steps[j].StartedAt {
			return steps[i].ID < steps[j].ID
		}
		return steps[i].StartedAt < steps[j].StartedAt
	})
	return steps, truncated
}

func safeLineageResource(ref LineageResourceRef) LineageResourceRef {
	ref.SchemaSnapshot = nil
	ref.Port = SafeDiagnosticText(ref.Port)
	ref.ItemFingerprint = SafeDiagnosticText(ref.ItemFingerprint)
	ref.WriteMode = SafeDiagnosticText(ref.WriteMode)
	if ref.Locator != "" {
		if resource, err := resourcetree.ParseURI(ref.Locator); err == nil {
			// Re-encode only canonical locator fields, discarding arbitrary query data.
			ref.Locator = resource.ToURI()
		} else if uri, err := url.Parse(ref.Locator); err == nil && uri.Scheme == "addp-infra" && uri.Host == "minio" && uri.User == nil && uri.Fragment == "" && (uri.Query().Get("type") == "object" || uri.Query().Get("type") == "prefix") {
			uri.RawQuery = url.Values{"type": []string{uri.Query().Get("type")}}.Encode()
			ref.Locator = uri.String()
		} else {
			ref.Locator = ""
		}
	}
	return ref
}

var diagnosticCode = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.]{0,79}$`)

// FailureCategory reuses stored errors to derive a bounded diagnostic cause.
// Unstructured error messages can contain SQL, row values or vendor response
// bodies, so tenant diagnostics never echo them. Detailed domain evidence stays
// behind the Owner's result/diagnostic authorization.
func FailureCategory(details models.JSONMap) string {
	text := strings.ToLower(fmt.Sprint(details["code"], " ", details["error_code"], " ", details["message"]))
	for _, rule := range []struct {
		category string
		patterns []string
	}{
		{"owner_unavailable", []string{"provider_unavailable", "contract_unavailable", "service_auth_unavailable"}},
		{"submission_uncertain", []string{"dispatch_uncertain"}},
		{"coordinator_lost", []string{"lease_expired", "lease_missing", "coordinator_stopped"}},
		{"child_failed", []string{"child_failed"}},
		{"permission_denied", []string{"dispatch_denied", "permission denied", "access denied", "unauthorized", "forbidden", "权限不足", "无权限"}},
		{"timeout", []string{"deadline exceeded", "timed out", "timeout", "超时"}},
		{"cancelled", []string{"context canceled", "context cancelled", "取消"}},
		{"connection_failed", []string{"connection refused", "connection reset", "connection failed", "connect failed", "连接失败", "连接中断"}},
		{"resource_exhausted", []string{"out of memory", "no space left", "resource exhausted", "内存不足", "空间不足"}},
		{"invalid_input", []string{"invalid parameter", "invalid input", "syntax error", "plan_invalid", "plan_missing", "step_state_invalid", "binding_invalid", "contract_invalid", "参数无效", "语法错误"}},
	} {
		for _, pattern := range rule.patterns {
			if strings.Contains(text, pattern) {
				return rule.category
			}
		}
	}
	return "execution_failed"
}

func diagnosticCurrentStep(item *TaskExecution) *string {
	if item.CurrentStep == nil {
		return nil
	}
	if item.Status == ExecutionStatusFailed || item.Status == ExecutionStatusTimeout || item.Status == ExecutionStatusCancelled {
		// Legacy owners sometimes write the entire error into current_step. Only a
		// recorded step identity is safe evidence of the last phase after failure.
		steps, ok := asDiagnosticObject(item.Metadata["step_results"])
		if !ok || steps[*item.CurrentStep] == nil {
			return nil
		}
	}
	return safeStringPointer(item.CurrentStep)
}
