package transform

import (
	"encoding/json"
	"fmt"

	"github.com/addp/transfer/pkg/pipeline"
)

func init() {
	MustRegisterTransform("filter", newFilterTransformFactory(), pipeline.TransformCapability{
		Name:        "filter",
		Description: "Filter rows based on field conditions (supports AND/OR logic)",
		ConfigSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"conditions"},
			"properties": map[string]interface{}{
				"conditions": map[string]interface{}{
					"type":        "array",
					"description": "List of filter conditions",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"field": map[string]string{
								"type":        "string",
								"description": "Field name",
							},
							"operator": map[string]interface{}{
								"type":        "string",
								"description": "Comparison operator",
								"enum":        []string{"eq", "ne", "gt", "lt", "gte", "lte", "contains"},
							},
							"value": map[string]interface{}{
								"description": "Comparison value",
							},
						},
						"required": []string{"field", "operator"},
					},
					"minItems": 1,
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"description": "Logical mode (and/or)",
					"enum":        []string{"and", "or"},
					"default":     "and",
				},
			},
		},
		Version: "1.0.0",
		Author:  "ADDP Transfer Module",
	})

	MustRegisterTransform("rename", newRenameTransformFactory(), pipeline.TransformCapability{
		Name:        "rename",
		Description: "Rename fields according to the provided mapping",
		ConfigSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"mappings"},
			"properties": map[string]interface{}{
				"mappings": map[string]interface{}{
					"type":        "object",
					"description": "Field rename mapping old_name -> new_name",
					"additionalProperties": map[string]string{
						"type": "string",
					},
				},
			},
		},
		Version: "1.0.0",
		Author:  "ADDP Transfer Module",
	})

	MustRegisterTransform("select", newSelectTransformFactory(), pipeline.TransformCapability{
		Name:        "select",
		Description: "Select subset of fields from rows",
		ConfigSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"fields"},
			"properties": map[string]interface{}{
				"fields": map[string]interface{}{
					"type":        "array",
					"description": "Fields to keep",
					"items": map[string]string{
						"type": "string",
					},
					"minItems": 1,
				},
			},
		},
		Version: "1.0.0",
		Author:  "ADDP Transfer Module",
	})
}

func newFilterTransformFactory() pipeline.TransformFactory {
	return func(config map[string]interface{}) (pipeline.Transform, error) {
		var (
			conditions []pipeline.FilterCondition
			mode       string
		)

		if raw, ok := config["conditions"]; ok {
			data, err := json.Marshal(raw)
			if err != nil {
				return nil, fmt.Errorf("filter: invalid conditions: %w", err)
			}
			if err := json.Unmarshal(data, &conditions); err != nil {
				return nil, fmt.Errorf("filter: parse conditions error: %w", err)
			}
		} else {
			return nil, fmt.Errorf("filter: missing conditions")
		}

		if m, ok := config["mode"].(string); ok {
			mode = m
		}

		return pipeline.NewFilterTransform(conditions, mode), nil
	}
}

func newRenameTransformFactory() pipeline.TransformFactory {
	return func(config map[string]interface{}) (pipeline.Transform, error) {
		raw, ok := config["mappings"]
		if !ok {
			return nil, fmt.Errorf("rename: missing mappings")
		}

		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("rename: invalid mappings: %w", err)
		}

		mappings := make(map[string]string)
		if err := json.Unmarshal(data, &mappings); err != nil {
			return nil, fmt.Errorf("rename: parse mappings error: %w", err)
		}

		return pipeline.NewRenameFieldsTransform(mappings), nil
	}
}

func newSelectTransformFactory() pipeline.TransformFactory {
	return func(config map[string]interface{}) (pipeline.Transform, error) {
		raw, ok := config["fields"]
		if !ok {
			return nil, fmt.Errorf("select: missing fields")
		}

		data, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("select: invalid fields: %w", err)
		}

		var fields []string
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, fmt.Errorf("select: parse fields error: %w", err)
		}

		return pipeline.NewSelectFieldsTransform(fields), nil
	}
}
