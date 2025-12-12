package service

import (
	"testing"

	"github.com/addp/orchestrator/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestResolveTemplateReferences 测试参数模板解析
func TestResolveTemplateReferences(t *testing.T) {
	executor := &Executor{}

	// 模拟步骤结果
	stepResults := models.StepResults{
		"sql_extract": {
			Status: "success",
			Result: map[string]interface{}{
				"result_table": "temp_table_123",
				"row_count":    100,
				"geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Point",
								"coordinates": []interface{}{116.404, 39.915},
							},
						},
					},
				},
			},
		},
		"spatial_analysis": {
			Status: "success",
			Result: map[string]interface{}{
				"execution_id": "exec-456",
				"geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Polygon",
								"coordinates": []interface{}{},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		params   map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "Simple field reference",
			params: map[string]interface{}{
				"table_name": "{{sql_extract.result_table}}",
			},
			expected: map[string]interface{}{
				"table_name": "temp_table_123",
			},
		},
		{
			name: "Nested field reference",
			params: map[string]interface{}{
				"input_geojson": "{{sql_extract.geojson}}",
			},
			expected: map[string]interface{}{
				"input_geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Point",
								"coordinates": []interface{}{116.404, 39.915},
							},
						},
					},
				},
			},
		},
		{
			name: "Multiple references",
			params: map[string]interface{}{
				"source_table":  "{{sql_extract.result_table}}",
				"result_geojson": "{{spatial_analysis.geojson}}",
				"row_count":     "{{sql_extract.row_count}}",
			},
			expected: map[string]interface{}{
				"source_table": "temp_table_123",
				"result_geojson": map[string]interface{}{
					"type": "FeatureCollection",
					"features": []interface{}{
						map[string]interface{}{
							"type": "Feature",
							"geometry": map[string]interface{}{
								"type":        "Polygon",
								"coordinates": []interface{}{},
							},
						},
					},
				},
				"row_count": 100,
			},
		},
		{
			name: "Mixed with static values",
			params: map[string]interface{}{
				"dynamic_table": "{{sql_extract.result_table}}",
				"static_value":  "fixed_string",
				"numeric_value": 42,
			},
			expected: map[string]interface{}{
				"dynamic_table": "temp_table_123",
				"static_value":  "fixed_string",
				"numeric_value": 42,
			},
		},
		{
			name: "Nested map with references",
			params: map[string]interface{}{
				"config": map[string]interface{}{
					"table":  "{{sql_extract.result_table}}",
					"format": "geojson",
					"data":   "{{sql_extract.geojson}}",
				},
			},
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"table":  "temp_table_123",
					"format": "geojson",
					"data": map[string]interface{}{
						"type": "FeatureCollection",
						"features": []interface{}{
							map[string]interface{}{
								"type": "Feature",
								"geometry": map[string]interface{}{
									"type":        "Point",
									"coordinates": []interface{}{116.404, 39.915},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Non-existent step reference",
			params: map[string]interface{}{
				"value": "{{nonexistent_step.field}}",
			},
			expected: map[string]interface{}{
				"value": nil,
			},
		},
		{
			name: "Non-existent field reference",
			params: map[string]interface{}{
				"value": "{{sql_extract.nonexistent_field}}",
			},
			expected: map[string]interface{}{
				"value": nil,
			},
		},
		{
			name: "Array with references",
			params: map[string]interface{}{
				"tables": []interface{}{
					"{{sql_extract.result_table}}",
					"static_table",
				},
			},
			expected: map[string]interface{}{
				"tables": []interface{}{
					"temp_table_123",
					"static_table",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := executor.resolveTemplateReferences(tt.params, stepResults)
			assert.Equal(t, tt.expected, resolved)
		})
	}
}

// TestSplitPath 测试路径分割
func TestSplitPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name:     "Simple path",
			path:     "step.result.field",
			expected: []string{"step", "result", "field"},
		},
		{
			name:     "Nested path",
			path:     "step.result.nested.field.value",
			expected: []string{"step", "result", "nested", "field", "value"},
		},
		{
			name:     "Single element",
			path:     "step",
			expected: []string{"step"},
		},
		{
			name:     "Empty path",
			path:     "",
			expected: []string{},
		},
		{
			name:     "Path with consecutive dots",
			path:     "step..result",
			expected: []string{"step", "result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveStringTemplate 测试字符串模板解析
func TestResolveStringTemplate(t *testing.T) {
	executor := &Executor{}

	stepResults := models.StepResults{
		"step1": {
			Status: "success",
			Result: map[string]interface{}{
				"table": "my_table",
				"nested": map[string]interface{}{
					"value": 123,
				},
			},
		},
	}

	tests := []struct {
		name     string
		template string
		expected interface{}
	}{
		{
			name:     "Valid template",
			template: "{{step1.table}}",
			expected: "my_table",
		},
		{
			name:     "Nested template",
			template: "{{step1.nested.value}}",
			expected: 123,
		},
		{
			name:     "Not a template",
			template: "plain_string",
			expected: "plain_string",
		},
		{
			name:     "Invalid template format",
			template: "{{incomplete",
			expected: "{{incomplete",
		},
		{
			name:     "Non-existent step",
			template: "{{nonexistent.field}}",
			expected: nil,
		},
		{
			name:     "Empty template",
			template: "{{}}",
			expected: "{{}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.resolveStringTemplate(tt.template, stepResults)
			assert.Equal(t, tt.expected, result)
		})
	}
}
