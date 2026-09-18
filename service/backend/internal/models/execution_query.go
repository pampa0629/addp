package models

// ExecutionQueryParameter uses canonical text to preserve exact numeric values.
type ExecutionQueryParameter struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Value *string `json:"value"`
}

// ExecutionQueryPreview is a read-only compilation result, not an execution.
type ExecutionQueryPreview struct {
	Language         string                    `json:"language"`
	Query            string                    `json:"query"`
	Parameters       []ExecutionQueryParameter `json:"parameters"`
	EngineID         uint                      `json:"engine_id"`
	EngineName       string                    `json:"engine_name"`
	EngineType       string                    `json:"engine_type"`
	Version          int64                     `json:"version"`
	ImplementationID int64                     `json:"implementation_id"`
	RevisionID       int64                     `json:"revision_id"`
	ResultKind       string                    `json:"result_kind"`
}
