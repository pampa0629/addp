package taskprovider

import (
	"strings"
	"testing"
)

func TestParseExecutionContractAcceptsConcreteClosedContract(t *testing.T) {
	contract, err := ParseExecutionContract(map[string]interface{}{
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"distance": map[string]interface{}{"type": "number", "minimum": float64(0)},
			},
			"additionalProperties": false,
		},
		"input_defaults":  map[string]interface{}{"distance": float64(100)},
		"input_ui_schema": map[string]interface{}{"distance": map[string]interface{}{"control": "number"}},
		"output_schema":   ClosedObjectSchema(),
	})
	if err != nil {
		t.Fatalf("ParseExecutionContract() error = %v", err)
	}
	if contract.InputDefaults["distance"] != float64(100) {
		t.Fatalf("input_defaults = %#v", contract.InputDefaults)
	}
}

func TestParseExecutionContractRejectsOpenAndUnknownFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]interface{})
		want   string
	}{
		{
			name: "unknown field",
			mutate: func(payload map[string]interface{}) {
				payload["execution_schema"] = ClosedObjectSchema()
			},
			want: "execution_schema is not allowed",
		},
		{
			name: "open input",
			mutate: func(payload map[string]interface{}) {
				payload["input_schema"].(map[string]interface{})["additionalProperties"] = true
			},
			want: "input_schema must be a closed object schema",
		},
		{
			name: "open output",
			mutate: func(payload map[string]interface{}) {
				payload["output_schema"].(map[string]interface{})["additionalProperties"] = true
			},
			want: "output_schema must be a closed object schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := map[string]interface{}{
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"distance": map[string]interface{}{"type": "number"},
					},
					"additionalProperties": false,
				},
				"input_defaults":  map[string]interface{}{"distance": float64(100)},
				"input_ui_schema": map[string]interface{}{},
				"output_schema":   ClosedObjectSchema(),
			}
			tt.mutate(payload)
			_, err := ParseExecutionContract(payload)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseExecutionContractAcceptsRequiredRuntimeInputsWithPartialDefaults(t *testing.T) {
	contract, err := ParseExecutionContract(map[string]interface{}{
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_locator": map[string]interface{}{"type": "string", "format": "resource-locator", "minLength": float64(1)},
				"batch_size":     map[string]interface{}{"type": "integer", "minimum": float64(1)},
			},
			"required":             []interface{}{"target_locator"},
			"additionalProperties": false,
		},
		"input_defaults":  map[string]interface{}{"batch_size": float64(1000)},
		"input_ui_schema": map[string]interface{}{"target_locator": map[string]interface{}{"control": "resource_tree_picker"}},
		"output_schema":   ClosedObjectSchema(),
	})
	if err != nil {
		t.Fatalf("ParseExecutionContract() error = %v", err)
	}
	if contract.InputDefaults["batch_size"] != float64(1000) {
		t.Fatalf("input_defaults = %#v", contract.InputDefaults)
	}
}
