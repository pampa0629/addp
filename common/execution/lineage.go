package execution

import (
	"github.com/addp/common/datatype"
)

// LineageFacts 是执行 owner 写入 common.task_executions.metadata 的统一血缘事实。
// 事实只包含已确认提交的持久化效果，任务整体失败不撤销这些提交。
// Owner 将完整事实与真实终态原子落库后，Meta collector 再解析并投影。
type LineageFacts struct {
	SchemaVersion      string               `json:"schema_version"`
	Inputs             []LineageResourceRef `json:"inputs"`
	Outputs            []LineageResourceRef `json:"outputs"`
	Operations         []LineageOperation   `json:"operations"`
	RuntimeExecutionID string               `json:"runtime_execution_id,omitempty"`
	MetaScanRefs       []string             `json:"meta_scan_refs,omitempty"`
}

type LineageResourceRef struct {
	Port            string                 `json:"port,omitempty"`
	Locator         string                 `json:"locator,omitempty"`
	ItemID          *uint                  `json:"item_id,omitempty"`
	ItemFingerprint string                 `json:"item_fingerprint,omitempty"`
	WriteMode       string                 `json:"write_mode,omitempty"`
	SchemaSnapshot  *LineageSchemaSnapshot `json:"schema_snapshot,omitempty"`
}

type LineageOperation struct {
	Kind               string                `json:"kind"`
	Operator           string                `json:"operator,omitempty"`
	InputPorts         []string              `json:"input_ports,omitempty"`
	OutputPorts        []string              `json:"output_ports,omitempty"`
	FieldLineageStatus string                `json:"field_lineage_status,omitempty"`
	FieldMappings      []LineageFieldMapping `json:"field_mappings,omitempty"`
}

type LineageSchemaSnapshot struct {
	Hash   string               `json:"hash"`
	Fields []datatype.FieldInfo `json:"fields"`
}

// Generated mappings have no input; they are evidence, not resource edges.
type LineageFieldMapping struct {
	InputPort      string `json:"input_port,omitempty"`
	OutputPort     string `json:"output_port"`
	SourceField    string `json:"source_field,omitempty"`
	TargetField    string `json:"target_field"`
	Transformation string `json:"transformation"`
}

const LineageFactsSchemaVersion = "addp.lineage-facts/v1"
