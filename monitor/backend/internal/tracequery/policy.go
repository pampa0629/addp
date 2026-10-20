package tracequery

import (
	_ "embed"
	"encoding/json"
)

//go:embed policy.json
var policyJSON []byte

type Policy struct {
	Version                int `json:"version"`
	DecisionWaitSeconds    int `json:"decision_wait_seconds"`
	PendingTraces          int `json:"pending_traces"`
	NormalPercent          int `json:"normal_percent"`
	SlowMS                 int `json:"slow_ms"`
	RetainedSpansPerSecond int `json:"retained_spans_per_second"`
	RetentionHours         int `json:"retention_hours"`
}

// Definition is the immutable Monitor-owned definition, not an activation receipt.
func Definition() Policy { var p Policy; _ = json.Unmarshal(policyJSON, &p); return p }
