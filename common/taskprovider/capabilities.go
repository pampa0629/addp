package taskprovider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const CapabilitiesSchemaVersion = "task.capabilities/v2"

var taskTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var allowedTopLevelCapabilityFields = map[string]struct{}{
	"schema_version":    {},
	"task_capabilities": {},
}

var allowedTaskCapabilityFields = map[string]struct{}{
	"type":                      {},
	"display_name":              {},
	"description":               {},
	"definition_schema":         {},
	"supports_schedule":         {},
	"supports_cancel":           {},
	"supports_inline_execution": {},
	"create_url":                {},
	"edit_url":                  {},
	"deprecated":                {},
}

var allowedJSONSchemaKeywords = map[string]struct{}{
	"type":                 {},
	"title":                {},
	"description":          {},
	"properties":           {},
	"required":             {},
	"enum":                 {},
	"default":              {},
	"additionalProperties": {},
	"items":                {},
	"minimum":              {},
	"maximum":              {},
	"minLength":            {},
	"maxLength":            {},
	"minItems":             {},
	"maxItems":             {},
	"format":               {},
}

var unsupportedJSONSchemaKeywords = map[string]struct{}{
	"$ref":  {},
	"oneOf": {},
	"anyOf": {},
	"allOf": {},
	"not":   {},
}

var allowedJSONSchemaTypes = map[string]struct{}{
	"object":  {},
	"array":   {},
	"string":  {},
	"number":  {},
	"integer": {},
	"boolean": {},
	"null":    {},
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type Capabilities struct {
	SchemaVersion    string
	TaskCapabilities []TaskCapability
	Extensions       map[string]interface{}
}

type TaskCapability struct {
	Type                    string
	DisplayName             string
	Description             string
	DefinitionSchema        map[string]interface{}
	SupportsSchedule        bool
	SupportsCancel          bool
	SupportsInlineExecution bool
	CreateURL               string
	EditURL                 string
	Deprecated              bool
}

func ParseCapabilities(raw string) (*Capabilities, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, validationError("capabilities is required")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, validationError("invalid capabilities JSON: %v", err)
	}
	if got := strings.TrimSpace(asString(payload["schema_version"])); got != CapabilitiesSchemaVersion {
		return nil, validationError("capabilities.schema_version must be %q", CapabilitiesSchemaVersion)
	}

	result := &Capabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		Extensions:    map[string]interface{}{},
	}
	for key, value := range payload {
		if _, allowed := allowedTopLevelCapabilityFields[key]; allowed {
			continue
		}
		if strings.HasPrefix(key, "x_") && len(key) > len("x_") {
			result.Extensions[key] = value
			continue
		}
		return nil, validationError("capabilities.%s must be a standard field or x_ private extension", key)
	}

	rawTaskCapabilities, ok := payload["task_capabilities"].([]interface{})
	if !ok || len(rawTaskCapabilities) == 0 {
		return nil, validationError("capabilities.task_capabilities must be a non-empty array")
	}
	seen := map[string]struct{}{}
	for i, rawItem := range rawTaskCapabilities {
		item, err := parseTaskCapability(rawItem, i)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[item.Type]; exists {
			return nil, validationError("duplicate task_type %q in capabilities.task_capabilities", item.Type)
		}
		seen[item.Type] = struct{}{}
		result.TaskCapabilities = append(result.TaskCapabilities, item)
	}
	return result, nil
}

func ValidateCapabilities(raw string) error {
	_, err := ParseCapabilities(raw)
	return err
}

func (c *Capabilities) CapabilityFor(taskType string) *TaskCapability {
	if c == nil {
		return nil
	}
	for i := range c.TaskCapabilities {
		if c.TaskCapabilities[i].Type == taskType {
			return &c.TaskCapabilities[i]
		}
	}
	return nil
}

func (c *Capabilities) HasCancelableTaskType() bool {
	if c == nil {
		return false
	}
	for _, item := range c.TaskCapabilities {
		if item.SupportsCancel {
			return true
		}
	}
	return false
}

func parseTaskCapability(raw interface{}, index int) (TaskCapability, error) {
	capability, ok := raw.(map[string]interface{})
	if !ok {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d] must be an object", index)
	}
	for key := range capability {
		if _, allowed := allowedTaskCapabilityFields[key]; !allowed {
			return TaskCapability{}, validationError("capabilities.task_capabilities[%d].%s is not allowed in task.capabilities/v2", index, key)
		}
	}

	typeName := strings.TrimSpace(asString(capability["type"]))
	if typeName == "" {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].type is required", index)
	}
	if !taskTypePattern.MatchString(typeName) {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].type must match ^[a-z][a-z0-9_]*$", index)
	}

	displayName := strings.TrimSpace(asString(capability["display_name"]))
	if displayName == "" {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].display_name is required", index)
	}
	description := strings.TrimSpace(asString(capability["description"]))
	if description == "" {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].description is required", index)
	}

	createURL := strings.TrimSpace(asString(capability["create_url"]))
	if createURL == "" {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].create_url is required", index)
	}
	if !isConsoleRouteURL(createURL) {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].create_url must be a Console route starting with /", index)
	}
	editURL := strings.TrimSpace(asString(capability["edit_url"]))
	if editURL == "" {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].edit_url is required", index)
	}
	if !isConsoleRouteURL(editURL) {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].edit_url must be a Console route starting with /", index)
	}

	definitionSchema, err := requiredObjectSchema(capability, "definition_schema", index)
	if err != nil {
		return TaskCapability{}, err
	}
	for _, field := range []string{"supports_schedule", "supports_cancel", "supports_inline_execution", "deprecated"} {
		if _, ok := capability[field].(bool); !ok {
			return TaskCapability{}, validationError("capabilities.task_capabilities[%d].%s must be boolean", index, field)
		}
	}
	supportsInlineExecution, _ := capability["supports_inline_execution"].(bool)
	if supportsInlineExecution {
		return TaskCapability{}, validationError("capabilities.task_capabilities[%d].supports_inline_execution must be false in task.capabilities/v2", index)
	}
	supportsSchedule, _ := capability["supports_schedule"].(bool)
	supportsCancel, _ := capability["supports_cancel"].(bool)
	deprecated, _ := capability["deprecated"].(bool)
	return TaskCapability{
		Type:                    typeName,
		DisplayName:             displayName,
		Description:             description,
		DefinitionSchema:        definitionSchema,
		SupportsSchedule:        supportsSchedule,
		SupportsCancel:          supportsCancel,
		SupportsInlineExecution: supportsInlineExecution,
		CreateURL:               createURL,
		EditURL:                 editURL,
		Deprecated:              deprecated,
	}, nil
}

func requiredObjectSchema(capability map[string]interface{}, field string, index int) (map[string]interface{}, error) {
	rawSchema, exists := capability[field]
	if !exists {
		return nil, validationError("capabilities.task_capabilities[%d].%s is required", index, field)
	}
	schema, ok := rawSchema.(map[string]interface{})
	if !ok {
		return nil, validationError("capabilities.task_capabilities[%d].%s must be an object schema", index, field)
	}
	if schemaType := strings.TrimSpace(asString(schema["type"])); schemaType != "object" {
		return nil, validationError("capabilities.task_capabilities[%d].%s.type must be object", index, field)
	}
	if err := validateJSONSchema(schema, fmt.Sprintf("capabilities.task_capabilities[%d].%s", index, field)); err != nil {
		return nil, err
	}
	return schema, nil
}

func validateJSONSchema(schema map[string]interface{}, path string) error {
	for key, value := range schema {
		if _, unsupported := unsupportedJSONSchemaKeywords[key]; unsupported {
			return validationError("%s.%s is not supported in task.capabilities/v2", path, key)
		}
		if _, allowed := allowedJSONSchemaKeywords[key]; !allowed {
			return validationError("%s.%s is not allowed in task.capabilities/v2", path, key)
		}
		switch key {
		case "type":
			typeName, ok := value.(string)
			if !ok {
				return validationError("%s.type must be string", path)
			}
			if _, allowed := allowedJSONSchemaTypes[typeName]; !allowed {
				return validationError("%s.type %q is not supported in task.capabilities/v2", path, typeName)
			}
		case "title", "description", "format":
			if _, ok := value.(string); !ok {
				return validationError("%s.%s must be string", path, key)
			}
		case "properties":
			properties, ok := value.(map[string]interface{})
			if !ok {
				return validationError("%s.properties must be object", path)
			}
			for propertyName, propertySchema := range properties {
				propertyObject, ok := propertySchema.(map[string]interface{})
				if !ok {
					return validationError("%s.properties.%s must be object schema", path, propertyName)
				}
				if err := validateJSONSchema(propertyObject, fmt.Sprintf("%s.properties.%s", path, propertyName)); err != nil {
					return err
				}
			}
		case "required":
			items, ok := value.([]interface{})
			if !ok {
				return validationError("%s.required must be array", path)
			}
			seen := map[string]struct{}{}
			for i, item := range items {
				name := strings.TrimSpace(asString(item))
				if name == "" {
					return validationError("%s.required[%d] must be non-empty string", path, i)
				}
				if _, exists := seen[name]; exists {
					return validationError("%s.required contains duplicate field %q", path, name)
				}
				seen[name] = struct{}{}
			}
		case "enum":
			items, ok := value.([]interface{})
			if !ok || len(items) == 0 {
				return validationError("%s.enum must be non-empty array", path)
			}
		case "additionalProperties":
			switch typed := value.(type) {
			case bool:
			case map[string]interface{}:
				if err := validateJSONSchema(typed, path+".additionalProperties"); err != nil {
					return err
				}
			default:
				return validationError("%s.additionalProperties must be boolean or object schema", path)
			}
		case "items":
			itemSchema, ok := value.(map[string]interface{})
			if !ok {
				return validationError("%s.items must be object schema", path)
			}
			if err := validateJSONSchema(itemSchema, path+".items"); err != nil {
				return err
			}
		case "minimum", "maximum":
			if !isJSONNumber(value) {
				return validationError("%s.%s must be number", path, key)
			}
		case "minLength", "maxLength", "minItems", "maxItems":
			if !isJSONNonNegativeInteger(value) {
				return validationError("%s.%s must be non-negative integer", path, key)
			}
		}
	}
	return nil
}

func isConsoleRouteURL(url string) bool {
	return strings.HasPrefix(url, "/") && !strings.HasPrefix(url, "//")
}

func asString(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func isJSONNumber(value interface{}) bool {
	_, ok := value.(float64)
	return ok
}

func isJSONNonNegativeInteger(value interface{}) bool {
	number, ok := value.(float64)
	return ok && number >= 0 && number == float64(int64(number))
}

func validationError(format string, args ...interface{}) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
