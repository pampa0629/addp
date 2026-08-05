package taskprovider

import "fmt"

// ExecutionContract is the exact execution boundary of one persisted task.
// Task type capabilities intentionally do not carry this task-specific data.
type ExecutionContract struct {
	InputSchema   map[string]interface{} `json:"input_schema"`
	InputDefaults map[string]interface{} `json:"input_defaults"`
	InputUISchema map[string]interface{} `json:"input_ui_schema"`
	OutputSchema  map[string]interface{} `json:"output_schema"`
}

func ClosedObjectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
}

func EmptyExecutionContract() ExecutionContract {
	return ExecutionContract{
		InputSchema:   ClosedObjectSchema(),
		InputDefaults: map[string]interface{}{},
		InputUISchema: map[string]interface{}{},
		OutputSchema:  ClosedObjectSchema(),
	}
}

func ParseExecutionContract(raw interface{}) (*ExecutionContract, error) {
	payload, ok := raw.(map[string]interface{})
	if !ok {
		return nil, validationError("execution_contract must be an object")
	}
	allowedFields := map[string]struct{}{
		"input_schema": {}, "input_defaults": {}, "input_ui_schema": {}, "output_schema": {},
	}
	for field := range payload {
		if _, allowed := allowedFields[field]; !allowed {
			return nil, validationError("execution_contract.%s is not allowed", field)
		}
	}

	inputSchema, err := executionContractObjectSchema(payload, "input_schema")
	if err != nil {
		return nil, err
	}
	if inputSchema["additionalProperties"] != false {
		return nil, validationError("execution_contract.input_schema must be a closed object schema")
	}
	if len(schemaStringSlice(inputSchema["required"])) > 0 {
		return nil, validationError("execution_contract.input_schema.required must be empty because task definitions are directly executable")
	}
	outputSchema, err := executionContractObjectSchema(payload, "output_schema")
	if err != nil {
		return nil, err
	}
	if outputSchema["additionalProperties"] != false {
		return nil, validationError("execution_contract.output_schema must be a closed object schema")
	}
	inputDefaults, ok := payload["input_defaults"].(map[string]interface{})
	if !ok {
		return nil, validationError("execution_contract.input_defaults must be an object")
	}
	inputUISchema, ok := payload["input_ui_schema"].(map[string]interface{})
	if !ok {
		return nil, validationError("execution_contract.input_ui_schema must be an object")
	}

	properties, _ := inputSchema["properties"].(map[string]interface{})
	for name, value := range inputUISchema {
		if _, declared := properties[name]; !declared {
			return nil, validationError("execution_contract.input_ui_schema.%s is not declared in input_schema.properties", name)
		}
		if _, ok := value.(map[string]interface{}); !ok {
			return nil, validationError("execution_contract.input_ui_schema.%s must be an object", name)
		}
	}
	if err := ValidateExecutionParameters(inputSchema, inputDefaults, ParameterValidationOptions{}); err != nil {
		return nil, validationError("execution_contract.input_defaults is invalid: %v", err)
	}

	return &ExecutionContract{
		InputSchema:   inputSchema,
		InputDefaults: inputDefaults,
		InputUISchema: inputUISchema,
		OutputSchema:  outputSchema,
	}, nil
}

func ValidateExecutionContract(raw interface{}) error {
	_, err := ParseExecutionContract(raw)
	return err
}

func executionContractObjectSchema(payload map[string]interface{}, field string) (map[string]interface{}, error) {
	raw, exists := payload[field]
	if !exists {
		return nil, validationError("execution_contract.%s is required", field)
	}
	schema, ok := raw.(map[string]interface{})
	if !ok || schema["type"] != "object" {
		return nil, validationError("execution_contract.%s must be an object schema", field)
	}
	if err := validateJSONSchema(schema, fmt.Sprintf("execution_contract.%s", field)); err != nil {
		return nil, err
	}
	return schema, nil
}
