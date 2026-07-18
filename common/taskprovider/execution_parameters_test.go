package taskprovider

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestValidateExecutionParametersAcceptsV1SchemaSubset(t *testing.T) {
	schema := executionParameterSchemaForTest()
	parameters := map[string]interface{}{
		"mode":   "deep",
		"limit":  100,
		"ratio":  0.5,
		"force":  true,
		"labels": []interface{}{"甲", "乙"},
		"config": map[string]interface{}{"owner": "addp"},
	}
	if err := ValidateExecutionParameters(schema, parameters, ParameterValidationOptions{}); err != nil {
		t.Fatalf("ValidateExecutionParameters() error = %v, want nil", err)
	}
}

func TestValidateExecutionParametersRejectsNonFiniteNumbers(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ratio": map[string]interface{}{"type": "number"},
		},
		"additionalProperties": false,
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := ValidateExecutionParameters(schema, map[string]interface{}{"ratio": value}, ParameterValidationOptions{})
		if err == nil || !strings.Contains(err.Error(), "parameters.ratio must be number") {
			t.Fatalf("value %v error = %v, want number rejection", value, err)
		}
	}
}

func TestValidateExecutionParametersRejectsContractViolations(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]interface{}
		want       string
	}{
		{name: "required", parameters: map[string]interface{}{}, want: "parameters.mode is required"},
		{name: "enum", parameters: map[string]interface{}{"mode": "wide"}, want: "parameters.mode must be one of"},
		{name: "integer", parameters: map[string]interface{}{"mode": "basic", "limit": 1.5}, want: "parameters.limit must be integer"},
		{name: "minimum", parameters: map[string]interface{}{"mode": "basic", "limit": 0}, want: "parameters.limit must be greater than or equal to 1"},
		{name: "maximum", parameters: map[string]interface{}{"mode": "basic", "limit": 1001}, want: "parameters.limit must be less than or equal to 1000"},
		{name: "array size", parameters: map[string]interface{}{"mode": "basic", "labels": []interface{}{}}, want: "parameters.labels must contain at least 1 items"},
		{name: "nested string", parameters: map[string]interface{}{"mode": "basic", "config": map[string]interface{}{"owner": ""}}, want: "parameters.config.owner must contain at least 1 characters"},
		{name: "additional", parameters: map[string]interface{}{"mode": "basic", "legacy": true}, want: "parameters.legacy is not allowed"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateExecutionParameters(executionParameterSchemaForTest(), testCase.parameters, ParameterValidationOptions{})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want containing %q", err, testCase.want)
			}
		})
	}
}

func TestValidateExecutionParametersReturnsStructuredError(t *testing.T) {
	err := ValidateExecutionParameters(
		executionParameterSchemaForTest(),
		map[string]interface{}{"mode": "basic", "limit": 1001},
		ParameterValidationOptions{},
	)
	var validationErr *ParameterValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *ParameterValidationError", err, err)
	}
	if validationErr.Rule != ParameterRuleMaximum || validationErr.Path != "parameters.limit" || validationErr.Limit != float64(1000) {
		t.Fatalf("validation error = %#v", validationErr)
	}
}

func TestValidateExecutionParametersAllowsTemplatesOnlyBeforeResolution(t *testing.T) {
	schema := executionParameterSchemaForTest()
	parameters := map[string]interface{}{"mode": "{{scan.mode}}", "limit": "{{scan.limit}}"}
	if err := ValidateExecutionParameters(schema, parameters, ParameterValidationOptions{AllowTemplateStrings: true}); err != nil {
		t.Fatalf("template validation error = %v, want nil", err)
	}
	if err := ValidateExecutionParameters(schema, parameters, ParameterValidationOptions{}); err == nil || !strings.Contains(err.Error(), "parameters.mode must be one of") {
		t.Fatalf("strict validation error = %v, want enum rejection", err)
	}
}

func TestValidateExecutionParametersValidatesAdditionalPropertiesSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": map[string]interface{}{"type": "integer", "minimum": float64(1)},
	}
	if err := ValidateExecutionParameters(schema, map[string]interface{}{"workers": 2}, ParameterValidationOptions{}); err != nil {
		t.Fatalf("valid additional property error = %v", err)
	}
	if err := ValidateExecutionParameters(schema, map[string]interface{}{"workers": 0}, ParameterValidationOptions{}); err == nil || !strings.Contains(err.Error(), "parameters.workers must be greater than or equal to 1") {
		t.Fatalf("invalid additional property error = %v", err)
	}
}

func executionParameterSchemaForTest() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode":   map[string]interface{}{"type": "string", "enum": []interface{}{"basic", "deep"}},
			"limit":  map[string]interface{}{"type": "integer", "minimum": float64(1), "maximum": float64(1000)},
			"ratio":  map[string]interface{}{"type": "number", "minimum": float64(0), "maximum": float64(1)},
			"force":  map[string]interface{}{"type": "boolean"},
			"labels": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "minItems": float64(1), "maxItems": float64(3)},
			"config": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"owner": map[string]interface{}{"type": "string", "minLength": float64(1)},
				},
				"required":             []interface{}{"owner"},
				"additionalProperties": false,
			},
		},
		"required":             []interface{}{"mode"},
		"additionalProperties": false,
	}
}
