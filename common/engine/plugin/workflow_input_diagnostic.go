package plugin

import (
	"bytes"
	"encoding/json"
)

// WorkflowOperatorFailure contains only fixed contract names and a definition
// position. It never carries user-defined node IDs, column names or values.
type WorkflowOperatorFailure struct {
	NodeIndex  int    `json:"node_index"`
	OperatorID string `json:"operator_id"`
	Parameter  string `json:"parameter,omitempty"`
	Field      string `json:"field,omitempty"`
}

type WorkflowInputDiagnostic struct {
	SchemaVersion string                   `json:"schema_version"`
	Stage         string                   `json:"stage"`
	Reason        string                   `json:"reason"`
	Operator      *WorkflowOperatorFailure `json:"operator"`
}

func (o *WorkflowOperatorFailure) Valid(reason string) bool {
	if o == nil || o.NodeIndex < 1 || o.NodeIndex > 65535 {
		return false
	}
	parameter, field, ok := "", "", false
	switch o.OperatorID {
	case "batch_buffer":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_field_conflict":
			parameter, field, ok = "input_gdf", "distance", true
		case "operator_number_array_invalid":
			parameter, ok = "distances", true
		case "operator_positive_integer_required":
			parameter, ok = "resolution", true
		}
	case "distance_to":
		switch reason {
		case "operator_geometry_field_conflict":
			parameter, field, ok = "input_gdf", "distance", true
		case "operator_projected_crs_required", "operator_crs_mismatch":
			ok = true
		}
	case "check_line_proximity":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_reference_invalid":
			parameter, ok = "reference_gdf", true
		case "operator_positive_number_required":
			parameter, ok = "tolerance", true
		case "operator_crs_invalid":
			ok = true
		}
	}
	return ok && o.Parameter == parameter && o.Field == field
}

func ParseWorkflowInputDiagnostic(value interface{}) *WorkflowInputDiagnostic {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > 2048 {
		return nil
	}
	var d WorkflowInputDiagnostic
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&d) != nil || d.SchemaVersion != "addp.execution-failure/v3" || d.Stage != "execution" || !d.Operator.Valid(d.Reason) {
		return nil
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return nil
	}
	var context map[string]json.RawMessage
	if json.Unmarshal(raw["operator"], &context) != nil {
		return nil
	}
	for _, key := range []string{"parameter", "field"} {
		if v, ok := context[key]; ok && (bytes.Equal(v, []byte("null")) || bytes.Equal(v, []byte(`""`))) {
			return nil
		}
	}
	return &d
}
