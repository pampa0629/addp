package api

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/execution"
)

// ManagerExecutionResponse exposes only Owner-declared execution facts.
// It must never embed TaskExecution or reuse raw runtime result maps.
type ManagerExecutionResponse struct {
	*execution.Observation
	Metadata ManagerExecutionMetadata `json:"metadata"`
}

type ManagerExecutionMetadata struct {
	Total         *int64 `json:"total,omitempty"`
	ReadySkipped  *int64 `json:"ready_skipped,omitempty"`
	Generated     *int64 `json:"generated,omitempty"`
	Rebuilt       *int64 `json:"rebuilt,omitempty"`
	Unsupported   *int64 `json:"unsupported,omitempty"`
	Failed        *int64 `json:"failed,omitempty"`
	MissingSource *int64 `json:"missing_source,omitempty"`

	TilesProcessed        *int64 `json:"tiles_processed,omitempty"`
	TilesTotalEstimate    *int64 `json:"tiles_total_estimate,omitempty"`
	TotalTiles            *int64 `json:"total_tiles,omitempty"`
	GeneratedTiles        *int64 `json:"generated_tiles,omitempty"`
	CachedTiles           *int64 `json:"cached_tiles,omitempty"`
	EmptyTiles            *int64 `json:"empty_tiles,omitempty"`
	SkippedTiles          *int64 `json:"skipped_tiles,omitempty"`
	OversizedSkippedTiles *int64 `json:"oversized_skipped_tiles,omitempty"`
	FailedTiles           *int64 `json:"failed_tiles,omitempty"`
	TotalSizeBytes        *int64 `json:"total_size_bytes,omitempty"`
	MaxTileSizeBytes      *int64 `json:"max_tile_size_bytes,omitempty"`
	ActualMaxZoom         *int64 `json:"actual_max_zoom,omitempty"`
	CurrentZoom           *int64 `json:"current_zoom,omitempty"`
	MinZoom               *int64 `json:"min_zoom,omitempty"`
	MaxZoom               *int64 `json:"max_zoom,omitempty"`

	LineageFacts    *ManagerExecutionLineage  `json:"lineage_facts,omitempty"`
	WorkflowRuntime *ManagerExecutionRuntime  `json:"workflow_runtime,omitempty"`
	GLBFacts        *ManagerExecutionGLBFacts `json:"glb_facts,omitempty"`
}

// Manager's lineage constructor produces resource identities and derive/embed
// operations; it does not produce schema snapshots or field mappings.
type ManagerExecutionLineage struct {
	SchemaVersion      string                      `json:"schema_version"`
	Inputs             []ManagerExecutionResource  `json:"inputs"`
	Outputs            []ManagerExecutionResource  `json:"outputs"`
	Operations         []ManagerExecutionOperation `json:"operations"`
	RuntimeExecutionID string                      `json:"runtime_execution_id,omitempty"`
	MetaScanRefs       []string                    `json:"meta_scan_refs,omitempty"`
}

type ManagerExecutionResource struct {
	Port            string `json:"port,omitempty"`
	Locator         string `json:"locator,omitempty"`
	ItemID          *uint  `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	WriteMode       string `json:"write_mode,omitempty"`
}

type ManagerExecutionOperation struct {
	Kind        string   `json:"kind"`
	Operator    string   `json:"operator,omitempty"`
	InputPorts  []string `json:"input_ports,omitempty"`
	OutputPorts []string `json:"output_ports,omitempty"`
}

type ManagerExecutionRuntime struct {
	EngineType      string   `json:"engine_type,omitempty"`
	Operator        string   `json:"operator,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	ExecutionTimeMs *float64 `json:"execution_time_ms,omitempty"`
}

type ManagerExecutionGLBFacts struct {
	SizeBytes    *int64                      `json:"size_bytes,omitempty"`
	SourceFormat string                      `json:"source_format,omitempty"`
	Conversion   *ManagerExecutionConversion `json:"conversion,omitempty"`
}

type ManagerExecutionConversion struct {
	SourceUnit      string   `json:"source_unit,omitempty"`
	UnitSource      string   `json:"unit_source,omitempty"`
	ScaleToMeters   *float64 `json:"scale_to_meters,omitempty"`
	ImporterVersion string   `json:"importer_version,omitempty"`
	TextureRefs     []string `json:"texture_refs,omitempty"`
	SourceSHA256    string   `json:"source_sha256,omitempty"`
	Meshes          *int64   `json:"meshes,omitempty"`
	Scope           string   `json:"scope,omitempty"`
}

func newManagerExecutionResponse(item *execution.TaskExecution) (*ManagerExecutionResponse, error) {
	response := &ManagerExecutionResponse{Observation: execution.Observe(item)}
	data, err := json.Marshal(item.Metadata)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &response.Metadata); err != nil {
		return nil, err
	}
	if facts := response.Metadata.LineageFacts; facts != nil && facts.SchemaVersion != execution.LineageFactsSchemaVersion {
		return nil, fmt.Errorf("invalid Manager lineage schema")
	}
	return response, nil
}
