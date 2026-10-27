package api

import (
	"encoding/json"
	"errors"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commoni18n "github.com/addp/common/middleware/i18n"
	transferi18n "github.com/addp/transfer/i18n"
	"github.com/addp/transfer/internal/models"
	"github.com/gin-gonic/gin"
)

// ExecutionResponse is the sole professional HTTP projection. Internal runtime
// state may grow without expanding this contract.
type ExecutionResponse struct {
	ID             uint                   `json:"id"`
	ExecutionID    string                 `json:"execution_id"`
	TaskID         uint                   `json:"task_id"`
	Status         models.ExecutionStatus `json:"status"`
	StartTime      models.LocalTime       `json:"start_time"`
	EndTime        *models.LocalTime      `json:"end_time,omitempty"`
	RecordsRead    int64                  `json:"records_read"`
	RecordsWritten int64                  `json:"records_written"`
	BytesRead      int64                  `json:"bytes_read"`
	BytesWritten   int64                  `json:"bytes_written"`
	// Safe diagnostics; code=query_output_lineage_unresolved identifies unprovable query lineage without private text. | 安全诊断；血缘不可证明使用稳定 code，不返回私有文本。
	ErrorDetails models.JSONMap    `json:"error_details,omitempty"`
	Metadata     ExecutionMetadata `json:"metadata"`
	TriggerType  string            `json:"trigger_type"`
	TriggerBy    *uint             `json:"trigger_by,omitempty"`
}

type ExecutionListResponse struct {
	Data       []*ExecutionResponse `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

type ExecutionProgressResponse struct {
	ExecutionID    string                 `json:"execution_id"`
	TaskID         uint                   `json:"task_id"`
	Status         models.ExecutionStatus `json:"status"`
	StartTime      models.LocalTime       `json:"start_time"`
	EndTime        *models.LocalTime      `json:"end_time,omitempty"`
	RecordsRead    int64                  `json:"records_read"`
	RecordsWritten int64                  `json:"records_written"`
	BytesRead      int64                  `json:"bytes_read"`
	BytesWritten   int64                  `json:"bytes_written"`
	// Safe diagnostics; code=query_output_lineage_unresolved identifies unprovable query lineage without private text. | 安全诊断；血缘不可证明使用稳定 code，不返回私有文本。
	ErrorDetails models.JSONMap `json:"error_details,omitempty"`
	Duration     float64        `json:"duration"`
	QPS          float64        `json:"qps"`
}

type ExecutionMetadata struct {
	InitialAuthorization        *ExecutionInitialAuthorization `json:"initial_authorization,omitempty"`
	MetadataScan                *ExecutionMetadataScan         `json:"metadata_scan,omitempty"`
	LineageFacts                *ExecutionLineageFacts         `json:"lineage_facts,omitempty"`
	Continuous                  *ExecutionContinuous           `json:"continuous,omitempty"`
	StopReason                  string                         `json:"stop_reason,omitempty"`
	RecoveryReason              string                         `json:"recovery_reason,omitempty"`
	RecoveredFromExecutionID    string                         `json:"recovered_from_execution_id,omitempty"`
	RecoveryAttempt             *uint                          `json:"recovery_attempt,omitempty"`
	RecoveryConsecutiveFailures *uint                          `json:"recovery_consecutive_failures,omitempty"`
	RecoveryBackoffSeconds      *float64                       `json:"recovery_backoff_seconds,omitempty"`
	RecoveryNotBefore           *time.Time                     `json:"recovery_not_before,omitempty"`
	RecoveryCircuitState        string                         `json:"recovery_circuit_state,omitempty"`
}

type ExecutionInitialAuthorization struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

type ExecutionLineageFacts struct {
	SchemaVersion      string                      `json:"schema_version"`
	Inputs             []ExecutionLineageResource  `json:"inputs,omitempty"`
	Outputs            []ExecutionLineageResource  `json:"outputs,omitempty"`
	Operations         []ExecutionLineageOperation `json:"operations,omitempty"`
	RuntimeExecutionID string                      `json:"runtime_execution_id,omitempty"`
	MetaScanRefs       []string                    `json:"meta_scan_refs,omitempty"`
}

type ExecutionLineageOperation struct {
	Kind               string                  `json:"kind"`
	Operator           string                  `json:"operator,omitempty"`
	InputPorts         []string                `json:"input_ports,omitempty"`
	OutputPorts        []string                `json:"output_ports,omitempty"`
	FieldLineageStatus string                  `json:"field_lineage_status,omitempty"`
	FieldMappings      []ExecutionFieldMapping `json:"field_mappings,omitempty"`
}

type ExecutionFieldMapping struct {
	InputPort      string `json:"input_port,omitempty"`
	OutputPort     string `json:"output_port"`
	SourceField    string `json:"source_field,omitempty"`
	TargetField    string `json:"target_field"`
	Transformation string `json:"transformation"`
}

type ExecutionLineageResource struct {
	Port            string                   `json:"port,omitempty"`
	Locator         string                   `json:"locator"`
	ItemID          *uint                    `json:"item_id,omitempty"`
	ItemFingerprint string                   `json:"item_fingerprint,omitempty"`
	WriteMode       string                   `json:"write_mode,omitempty"`
	SchemaSnapshot  *ExecutionSchemaSnapshot `json:"schema_snapshot,omitempty"`
}

type ExecutionSchemaSnapshot struct {
	Hash   string                 `json:"hash"`
	Fields []ExecutionSchemaField `json:"fields"`
}

// Frozen structural evidence excludes comments and value-bearing expressions.
type ExecutionSchemaField struct {
	Name            string   `json:"name,omitempty"`
	Path            []string `json:"path,omitempty"`
	Type            string   `json:"type,omitempty"`
	ElementType     string   `json:"element_type,omitempty"`
	NativeType      string   `json:"native_type,omitempty"`
	Nullable        bool     `json:"nullable"`
	PrimaryKey      bool     `json:"primary_key,omitempty"`
	Size            int      `json:"size,omitempty"`
	Precision       int      `json:"precision,omitempty"`
	Scale           int      `json:"scale,omitempty"`
	OrdinalPosition int      `json:"ordinal_position,omitempty"`
	Generated       bool     `json:"generated,omitempty"`
}

type ExecutionContinuous struct {
	Partitions      map[string]ExecutionPartition `json:"partitions,omitempty"`
	Diagnostics     *ExecutionDiagnostics         `json:"diagnostics,omitempty"`
	Capture         *ExecutionCapture             `json:"capture,omitempty"`
	SchemaChange    *ExecutionSchemaChange        `json:"schema_change,omitempty"`
	LastCommittedAt *time.Time                    `json:"last_committed_at,omitempty"`
	LastEventAt     *time.Time                    `json:"last_event_at,omitempty"`
}

type ExecutionPartition struct {
	Type    string                  `json:"type"`
	Version string                  `json:"version"`
	Values  ExecutionPartitionValue `json:"values"`
}

type ExecutionPartitionValue struct {
	// ChangeStreamPosition.Values stores decimal strings, including Kafka offsets.
	NextOffset *int64 `json:"next_offset,omitempty,string" swaggertype:"string" example:"42"`
}

type ExecutionPartitionDiagnostics struct {
	Partition                  string   `json:"partition"`
	EarliestOffset             int64    `json:"earliest_offset"`
	LatestOffset               int64    `json:"latest_offset"`
	NextOffset                 *int64   `json:"next_offset,omitempty"`
	LagRecords                 *int64   `json:"lag_records,omitempty"`
	RecoveryHeadroomRecords    *int64   `json:"recovery_headroom_records,omitempty"`
	SourceRateRecordsPerSecond *float64 `json:"source_rate_records_per_second,omitempty"`
	RetentionHorizonSeconds    *float64 `json:"retention_horizon_seconds,omitempty"`
	Health                     string   `json:"health"`
	CheckpointAgeSeconds       *float64 `json:"checkpoint_age_seconds,omitempty"`
	CheckpointHealth           string   `json:"checkpoint_health"`
}

type ExecutionDiagnostics struct {
	SampledAt                   *time.Time                               `json:"sampled_at,omitempty"`
	Health                      string                                   `json:"health"`
	DegradedHorizonSeconds      float64                                  `json:"degraded_horizon_seconds"`
	CriticalHorizonSeconds      float64                                  `json:"critical_horizon_seconds"`
	CheckpointStaleAfterSeconds float64                                  `json:"checkpoint_stale_after_seconds"`
	CheckpointHealth            string                                   `json:"checkpoint_health"`
	Partitions                  map[string]ExecutionPartitionDiagnostics `json:"partitions,omitempty"`
	// Replaced with a fixed localized message before serializing.
	Error string `json:"error,omitempty"`
}

type ExecutionCapture struct {
	Generation         uint64                        `json:"generation"`
	SourceRecovery     *ExecutionCaptureRecovery     `json:"source_recovery,omitempty"`
	SourceTransactions *ExecutionCaptureTransactions `json:"source_transactions,omitempty"`
}

type ExecutionCaptureRecovery struct {
	SchemaVersion             string     `json:"schema_version"`
	Provider                  string     `json:"provider"`
	Health                    string     `json:"health"`
	CapturePosition           string     `json:"capture_position,omitempty"`
	CurrentPosition           string     `json:"current_position,omitempty"`
	EarliestAvailablePosition string     `json:"earliest_available_position,omitempty"`
	PositionHeadroom          string     `json:"position_headroom,omitempty"`
	EarliestAvailableAt       *time.Time `json:"earliest_available_at,omitempty"`
	WindowSeconds             *int64     `json:"window_seconds,omitempty"`
	FRAUsedPercent            *float64   `json:"fra_used_percent,omitempty"`
	FRAReclaimablePercent     *float64   `json:"fra_reclaimable_percent,omitempty"`
	SampledAt                 time.Time  `json:"sampled_at"`
}

type ExecutionCaptureTransactions struct {
	SchemaVersion         string    `json:"schema_version"`
	Provider              string    `json:"provider"`
	Status                string    `json:"status"`
	ActiveCount           uint64    `json:"active_count"`
	OldestStartPosition   string    `json:"oldest_start_position,omitempty"`
	OldestDurationSeconds *int64    `json:"oldest_duration_seconds,omitempty"`
	UsedUndoBlocks        string    `json:"used_undo_blocks,omitempty"`
	UsedUndoRecords       string    `json:"used_undo_records,omitempty"`
	SampledAt             time.Time `json:"sampled_at"`
}

type ExecutionSchemaChange struct {
	RequestID          uint                   `json:"request_id"`
	Status             string                 `json:"status"`
	Generation         uint64                 `json:"generation"`
	FromRevision       uint64                 `json:"from_revision"`
	ToRevision         uint64                 `json:"to_revision"`
	DetectedAt         *time.Time             `json:"detected_at,omitempty"`
	StoppedAt          *time.Time             `json:"stopped_at,omitempty"`
	Scope              string                 `json:"scope"`
	SourcePartition    string                 `json:"source_partition"`
	SourceOffset       int64                  `json:"source_offset"`
	MissingFields      []string               `json:"missing_fields,omitempty"`
	UnexpectedFields   []string               `json:"unexpected_fields,omitempty"`
	IncompatibleFields []string               `json:"incompatible_fields,omitempty"`
	MetadataScan       *ExecutionMetadataScan `json:"metadata_scan,omitempty"`
}

type ExecutionMetadataScan struct {
	ErrorCode   string `json:"error_code,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	Status      string `json:"status"`
	Attempt     uint   `json:"attempt"`
}

// ModuleExecutionResult is authorized by Service Client/source, independently
// of User reads. Only export-session artifact identities cross this boundary.
type ModuleExecutionResult struct {
	ExportProtectionCurrent *bool                   `json:"export_protection_current,omitempty"`
	ExecutionID             string                  `json:"execution_id"`
	TaskID                  uint                    `json:"task_id"`
	Status                  models.ExecutionStatus  `json:"status"`
	ErrorMsg                string                  `json:"error_msg,omitempty"`
	Metadata                ModuleExecutionMetadata `json:"metadata"`
}

type ModuleExecutionMetadata struct {
	TargetRefs []ExecutionTargetRef `json:"target_refs,omitempty"`
}

type ExecutionTargetRef struct {
	Path      string `json:"path"`
	Role      string `json:"role,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Extension string `json:"extension,omitempty"`
	Entry     string `json:"entry,omitempty"`
}

func projectExecution(c *gin.Context, item *models.TaskExecution) (*ExecutionResponse, error) {
	metadata := ExecutionMetadata{}
	if err := projectExecutionMetadata(item.Metadata, &metadata); err != nil {
		return nil, err
	}
	metadata.InitialAuthorization = nil
	if raw, exists := item.Metadata[models.ProductionAuthorizationMetadataKey]; exists {
		state, err := models.DecodeProductionAuthorization(raw)
		if err == nil {
			metadata.InitialAuthorization = &ExecutionInitialAuthorization{Status: state.Status, Code: executionEnum(state.Code,
				"system_unavailable", "authorization_pending", "scan_pending", "scan_failed", "production_ineligible", "source_access_denied", "execution_permission_denied", "target_history_exists", "engine_changed")}
		}
	}
	metadata.StopReason = executionEnum(metadata.StopReason, "paused", "stopped", "cleanup", "worker_shutdown", "lease_expired", "execution_failed", "schema_change_blocked")
	if metadata.MetadataScan != nil {
		metadata.MetadataScan.Status = executionEnum(metadata.MetadataScan.Status, "submitted", "failed")
		metadata.MetadataScan.ErrorCode = executionEnum(metadata.MetadataScan.ErrorCode, "scan_origin_unavailable", "scan_output_unavailable", "scan_service_unavailable", "scan_submission_failed")
	}
	metadata.RecoveryReason = executionEnum(metadata.RecoveryReason, "worker_shutdown", "lease_expired", "execution_failed")
	metadata.RecoveryCircuitState = executionEnum(metadata.RecoveryCircuitState, "closed", "open", "half_open")
	if metadata.Continuous != nil && metadata.Continuous.Diagnostics != nil && metadata.Continuous.Diagnostics.Error != "" {
		metadata.Continuous.Diagnostics.Error = commoni18n.T(c, transferi18n.MsgExecutionDiagnosticsUnavailable)
	}
	return &ExecutionResponse{
		ID: item.ID, ExecutionID: item.ExecutionID, TaskID: item.TaskID, Status: item.Status,
		StartTime: item.StartTime, EndTime: item.EndTime, RecordsRead: item.RecordsRead,
		RecordsWritten: item.RecordsWritten, BytesRead: item.BytesRead, BytesWritten: item.BytesWritten,
		ErrorDetails: item.ErrorDetails, Metadata: metadata, TriggerType: item.TriggerType, TriggerBy: item.TriggerBy,
	}, nil
}

func projectModuleExecution(c *gin.Context, item *models.TaskExecution) (*ModuleExecutionResult, error) {
	metadata := ModuleExecutionMetadata{}
	if err := projectExecutionMetadata(item.Metadata, &metadata); err != nil {
		return nil, err
	}
	result := &ModuleExecutionResult{ExecutionID: item.ExecutionID, TaskID: item.TaskID, Status: item.Status, Metadata: metadata}
	if item.Status == models.ExecutionStatusFailed || string(item.Status) == commonExecution.ExecutionStatusTimeout {
		result.ErrorMsg = commoni18n.T(c, transferi18n.MsgExecutionFailed)
	}
	return result, nil
}

func projectExecutionMetadata(input models.JSONMap, output interface{}) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

func executionEnum(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return ""
}

func respondExecutionReadError(c *gin.Context, err error) {
	if errors.Is(err, commonAPI.ErrNotFound) {
		commonAPI.NotFoundError(c, commoni18n.T(c, transferi18n.MsgExecutionNotFound))
		return
	}
	commonAPI.InternalServerError(c, commoni18n.T(c, transferi18n.MsgExecutionReadUnavailable))
}

func respondExecution(c *gin.Context, status int, item *models.TaskExecution) {
	response, err := projectExecution(c, item)
	if err != nil {
		respondExecutionReadError(c, err)
		return
	}
	c.JSON(status, response)
}

func respondExecutions(c *gin.Context, items []models.TaskExecution, total int64, page, pageSize int) {
	responses := make([]*ExecutionResponse, 0, len(items))
	for i := range items {
		response, err := projectExecution(c, &items[i])
		if err != nil {
			respondExecutionReadError(c, err)
			return
		}
		responses = append(responses, response)
	}
	commonAPI.SendPaginatedResponse(c, responses, total, page, pageSize)
}
