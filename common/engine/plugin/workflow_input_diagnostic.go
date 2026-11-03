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
	case "contains", "intersects":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_reference_invalid":
			parameter, ok = "gdf_b", true
		case "operator_crs_invalid":
			ok = true
		}
	case "dissolve":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_group_fields_invalid":
			parameter, ok = "group_fields", true
		case "operator_crs_invalid", "operator_dissolve_fields_conflict":
			ok = true
		}
	case "union":
		switch reason {
		case "operator_input_invalid", "operator_input_overlap":
			parameter, ok = "input_gdf", true
		case "operator_reference_invalid", "operator_reference_overlap":
			parameter, ok = "gdf_b", true
		case "operator_crs_invalid", "operator_overlay_fields_conflict":
			ok = true
		}
	case "clip":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_mask_invalid":
			parameter, ok = "mask_gdf", true
		case "operator_crs_invalid":
			ok = true
		}
	case "difference":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_mask_invalid":
			parameter, ok = "gdf_b", true
		case "operator_crs_invalid":
			ok = true
		}
	case "intersection":
		switch reason {
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_reference_invalid":
			parameter, ok = "gdf_b", true
		case "operator_crs_invalid", "operator_overlay_fields_conflict":
			ok = true
		}
	case "extract_line_nodes":
		switch reason {
		case "operator_input_invalid", "operator_endpoint_network_invalid":
			parameter, ok = "input_gdf", true
		case "operator_projected_crs_required":
			ok = true
		case "operator_field_conflict":
			parameter = "input_gdf"
			switch o.Field {
			case "node_id", "degree", "source_id", "source_part_no", "end":
				field, ok = o.Field, true
			}
		}
	case "trim_line_endpoints":
		switch reason {
		case "operator_distance_limit_invalid":
			parameter, ok = "max_distance", true
		case "operator_input_invalid":
			parameter, ok = "input_gdf", true
		case "operator_reference_invalid":
			parameter, ok = "reference_gdf", true
		case "operator_crs_invalid":
			parameter, ok = "", true
		case "operator_source_topology_invalid":
			parameter, ok = "input_gdf", true
		case "operator_trim_conflict":
			parameter, ok = "", true
		}

	case "extract_polygon_rings":
		parameter = "input_gdf"
		switch reason {
		case "operator_geometry_invalid":
			ok = true
		case "operator_field_conflict":
			switch o.Field {
			case "source_id", "part_no", "ring_no", "ring_id", "is_hole":
				field, ok = o.Field, true
			}
		}
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
