package taskprovider

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var fullTemplatePattern = regexp.MustCompile(`^\s*\{\{[^{}]+\}\}\s*$`)

// ParameterValidationRule identifies a stable execution parameter validation failure.
type ParameterValidationRule string

const (
	ParameterRuleSchemaRequired     ParameterValidationRule = "schema_required"
	ParameterRuleSchemaType         ParameterValidationRule = "schema_type"
	ParameterRuleRequired           ParameterValidationRule = "required"
	ParameterRuleEnum               ParameterValidationRule = "enum"
	ParameterRuleType               ParameterValidationRule = "type"
	ParameterRuleAdditionalProperty ParameterValidationRule = "additional_property"
	ParameterRuleMinimum            ParameterValidationRule = "minimum"
	ParameterRuleMaximum            ParameterValidationRule = "maximum"
	ParameterRuleMinItems           ParameterValidationRule = "min_items"
	ParameterRuleMaxItems           ParameterValidationRule = "max_items"
	ParameterRuleMinLength          ParameterValidationRule = "min_length"
	ParameterRuleMaxLength          ParameterValidationRule = "max_length"
)

// ParameterValidationError carries stable, localizable execution parameter
// validation details while retaining an English Error string for logs.
type ParameterValidationError struct {
	Rule     ParameterValidationRule
	Path     string
	Expected string
	Limit    interface{}
	Allowed  []interface{}
}

func (e *ParameterValidationError) Error() string {
	switch e.Rule {
	case ParameterRuleSchemaRequired:
		return "input_schema is required"
	case ParameterRuleSchemaType:
		return "input_schema.type must be object"
	case ParameterRuleRequired:
		return fmt.Sprintf("%s is required", e.Path)
	case ParameterRuleEnum:
		encoded, _ := json.Marshal(e.Allowed)
		return fmt.Sprintf("%s must be one of %s", e.Path, string(encoded))
	case ParameterRuleType:
		return fmt.Sprintf("%s must be %s", e.Path, e.Expected)
	case ParameterRuleAdditionalProperty:
		return fmt.Sprintf("%s is not allowed by input_schema", e.Path)
	case ParameterRuleMinimum:
		return fmt.Sprintf("%s must be greater than or equal to %v", e.Path, e.Limit)
	case ParameterRuleMaximum:
		return fmt.Sprintf("%s must be less than or equal to %v", e.Path, e.Limit)
	case ParameterRuleMinItems:
		return fmt.Sprintf("%s must contain at least %v items", e.Path, e.Limit)
	case ParameterRuleMaxItems:
		return fmt.Sprintf("%s must contain no more than %v items", e.Path, e.Limit)
	case ParameterRuleMinLength:
		return fmt.Sprintf("%s must contain at least %v characters", e.Path, e.Limit)
	case ParameterRuleMaxLength:
		return fmt.Sprintf("%s must contain no more than %v characters", e.Path, e.Limit)
	default:
		return "execution parameters are invalid"
	}
}

// ParameterValidationOptions controls validation that differs between persisted
// orchestration definitions and resolved runtime parameters.
type ParameterValidationOptions struct {
	AllowTemplateStrings bool
	AllowMissingRequired bool
}

// ValidateExecutionParameters validates one execution's parameter overrides
// against a concrete task's execution contract. Owner modules still perform
// final domain validation.
func ValidateExecutionParameters(schema map[string]interface{}, parameters map[string]interface{}, options ParameterValidationOptions) error {
	if schema == nil {
		return &ParameterValidationError{Rule: ParameterRuleSchemaRequired, Path: "input_schema"}
	}
	if schemaType, _ := schema["type"].(string); schemaType != "object" {
		return &ParameterValidationError{Rule: ParameterRuleSchemaType, Path: "input_schema.type", Expected: "object"}
	}
	if parameters == nil {
		parameters = map[string]interface{}{}
	}
	return validateSchemaValue(schema, parameters, "parameters", options)
}

func validateSchemaValue(schema map[string]interface{}, value interface{}, path string, options ParameterValidationOptions) error {
	if options.AllowTemplateStrings && isFullTemplateString(value) {
		return nil
	}

	if enumValues, ok := schemaSlice(schema["enum"]); ok && !containsSchemaValue(enumValues, value) {
		return &ParameterValidationError{Rule: ParameterRuleEnum, Path: path, Allowed: enumValues}
	}

	schemaType, _ := schema["type"].(string)
	if err := validateSchemaType(schemaType, value, path); err != nil {
		return err
	}

	switch typed := value.(type) {
	case map[string]interface{}:
		if err := validateObjectValue(schema, typed, path, options); err != nil {
			return err
		}
	case []interface{}:
		if err := validateArrayValue(schema, typed, path, options); err != nil {
			return err
		}
	case string:
		if err := validateStringValue(schema, typed, path); err != nil {
			return err
		}
	default:
		if number, ok := schemaNumber(value); ok {
			if minimum, exists := schemaNumber(schema["minimum"]); exists && number < minimum {
				return &ParameterValidationError{Rule: ParameterRuleMinimum, Path: path, Limit: schema["minimum"]}
			}
			if maximum, exists := schemaNumber(schema["maximum"]); exists && number > maximum {
				return &ParameterValidationError{Rule: ParameterRuleMaximum, Path: path, Limit: schema["maximum"]}
			}
		}
	}
	return nil
}

func validateSchemaType(schemaType string, value interface{}, path string) error {
	if schemaType == "" {
		return nil
	}
	valid := false
	switch schemaType {
	case "object":
		_, valid = value.(map[string]interface{})
	case "array":
		_, valid = value.([]interface{})
	case "string":
		_, valid = value.(string)
	case "number":
		_, valid = schemaNumber(value)
	case "integer":
		number, ok := schemaNumber(value)
		valid = ok && math.Trunc(number) == number
	case "boolean":
		_, valid = value.(bool)
	case "null":
		valid = value == nil
	}
	if !valid {
		return &ParameterValidationError{Rule: ParameterRuleType, Path: path, Expected: schemaType}
	}
	return nil
}

func validateObjectValue(schema map[string]interface{}, value map[string]interface{}, path string, options ParameterValidationOptions) error {
	properties, _ := schema["properties"].(map[string]interface{})
	if !options.AllowMissingRequired {
		for _, requiredName := range schemaStringSlice(schema["required"]) {
			if _, exists := value[requiredName]; !exists {
				return &ParameterValidationError{Rule: ParameterRuleRequired, Path: path + "." + requiredName}
			}
		}
	}

	names := make([]string, 0, len(value))
	for name := range value {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fieldValue := value[name]
		fieldSchema, declared := objectSchema(properties[name])
		if declared {
			if err := validateSchemaValue(fieldSchema, fieldValue, path+"."+name, options); err != nil {
				return err
			}
			continue
		}

		switch additional := schema["additionalProperties"].(type) {
		case bool:
			if !additional {
				return &ParameterValidationError{Rule: ParameterRuleAdditionalProperty, Path: path + "." + name}
			}
		case map[string]interface{}:
			if err := validateSchemaValue(additional, fieldValue, path+"."+name, options); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArrayValue(schema map[string]interface{}, value []interface{}, path string, options ParameterValidationOptions) error {
	if minimum, ok := schemaNonNegativeInteger(schema["minItems"]); ok && len(value) < minimum {
		return &ParameterValidationError{Rule: ParameterRuleMinItems, Path: path, Limit: minimum}
	}
	if maximum, ok := schemaNonNegativeInteger(schema["maxItems"]); ok && len(value) > maximum {
		return &ParameterValidationError{Rule: ParameterRuleMaxItems, Path: path, Limit: maximum}
	}
	itemSchema, ok := schema["items"].(map[string]interface{})
	if !ok {
		return nil
	}
	for index, item := range value {
		if err := validateSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index), options); err != nil {
			return err
		}
	}
	return nil
}

func validateStringValue(schema map[string]interface{}, value string, path string) error {
	length := utf8.RuneCountInString(value)
	if minimum, ok := schemaNonNegativeInteger(schema["minLength"]); ok && length < minimum {
		return &ParameterValidationError{Rule: ParameterRuleMinLength, Path: path, Limit: minimum}
	}
	if maximum, ok := schemaNonNegativeInteger(schema["maxLength"]); ok && length > maximum {
		return &ParameterValidationError{Rule: ParameterRuleMaxLength, Path: path, Limit: maximum}
	}
	return nil
}

func isFullTemplateString(value interface{}) bool {
	text, ok := value.(string)
	return ok && fullTemplatePattern.MatchString(text)
}

func objectSchema(value interface{}) (map[string]interface{}, bool) {
	schema, ok := value.(map[string]interface{})
	return schema, ok
}

func schemaSlice(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if items, ok := value.([]interface{}); ok {
		return items, true
	}
	raw := reflect.ValueOf(value)
	if raw.Kind() != reflect.Slice && raw.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]interface{}, raw.Len())
	for index := 0; index < raw.Len(); index++ {
		items[index] = raw.Index(index).Interface()
	}
	return items, true
}

func schemaStringSlice(value interface{}) []string {
	items, ok := schemaSlice(value)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, text)
		}
	}
	return result
}

func containsSchemaValue(values []interface{}, candidate interface{}) bool {
	for _, value := range values {
		left, leftNumber := schemaNumber(value)
		right, rightNumber := schemaNumber(candidate)
		if leftNumber && rightNumber && left == right {
			return true
		}
		if reflect.DeepEqual(value, candidate) {
			return true
		}
	}
	return false
}

func schemaNumber(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		number := float64(typed)
		return number, !math.IsNaN(number) && !math.IsInf(number, 0)
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return 0, false
	}
}

func schemaNonNegativeInteger(value interface{}) (int, bool) {
	number, ok := schemaNumber(value)
	return int(number), ok && number >= 0 && math.Trunc(number) == number
}
