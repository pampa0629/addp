package execution

import "github.com/addp/common/models"

// LineageFacts 是执行 owner 写入 common.task_executions.metadata 的统一血缘事实。
// 事实只在执行成功后由 Meta collector 解析并投影为当前关系。
type LineageFacts struct {
	SchemaVersion      string               `json:"schema_version"`
	Inputs             []LineageResourceRef `json:"inputs"`
	Outputs            []LineageResourceRef `json:"outputs"`
	Operations         []LineageOperation   `json:"operations"`
	RuntimeExecutionID string               `json:"runtime_execution_id,omitempty"`
	MetaScanRefs       []string             `json:"meta_scan_refs,omitempty"`
}

type LineageResourceRef struct {
	Port            string         `json:"port,omitempty"`
	Locator         string         `json:"locator,omitempty"`
	ItemID          *uint          `json:"item_id,omitempty"`
	ItemFingerprint string         `json:"item_fingerprint,omitempty"`
	FieldName       string         `json:"field_name,omitempty"`
	WriteMode       string         `json:"write_mode,omitempty"`
	SchemaSnapshot  models.JSONMap `json:"schema_snapshot,omitempty"`
}

type LineageOperation struct {
	Kind        string   `json:"kind"`
	Operator    string   `json:"operator,omitempty"`
	InputPorts  []string `json:"input_ports,omitempty"`
	OutputPorts []string `json:"output_ports,omitempty"`
}

const LineageFactsSchemaVersion = "addp.lineage-facts/v1"
