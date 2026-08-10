package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	commonquery "github.com/addp/common/query"
	"github.com/addp/common/taskprovider"
	"github.com/addp/develop/backend/internal/models"
)

var allowedQueryParameterTypes = map[string]struct{}{
	"string":  {},
	"integer": {},
	"number":  {},
	"boolean": {},
}

type QueryParameterDefinitionsError struct {
	Cause error
}

func (e *QueryParameterDefinitionsError) Error() string {
	if e == nil || e.Cause == nil {
		return "查询参数定义无效"
	}
	return e.Cause.Error()
}

func (e *QueryParameterDefinitionsError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func BuildQueryExecutionContract(content map[string]interface{}) (*taskprovider.ExecutionContract, error) {
	definitions, err := queryParameterDefinitions(content)
	if err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: err}
	}
	query, _ := content["query"].(string)
	language, _ := content["query_type"].(string)
	references, err := commonquery.References(language, query)
	if err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("查询参数引用无效: %w", err)}
	}
	definitionValues := make(map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		definitionValues[definition.Name] = definition.Default
	}
	if err := commonquery.ValidateDefinitions(references, definitionValues); err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("查询参数定义与查询不一致: %w", err)}
	}

	contract := taskprovider.EmptyExecutionContract()
	properties := contract.InputSchema["properties"].(map[string]interface{})
	for index, definition := range definitions {
		field := map[string]interface{}{
			"type": definition.Type,
		}
		if definition.Title != "" {
			field["title"] = definition.Title
		}
		if definition.Description != "" {
			field["description"] = definition.Description
		}
		properties[definition.Name] = field
		contract.InputDefaults[definition.Name] = definition.Default
		contract.InputUISchema[definition.Name] = map[string]interface{}{
			"order": index,
		}
	}
	return &contract, nil
}

func resolveQueryExecutionParameters(
	content map[string]interface{},
	overrides map[string]interface{},
) (*taskprovider.ExecutionContract, map[string]interface{}, error) {
	contract, err := BuildQueryExecutionContract(content)
	if err != nil {
		return nil, nil, err
	}
	if err := taskprovider.ValidateExecutionParameters(
		contract.InputSchema,
		overrides,
		taskprovider.ParameterValidationOptions{},
	); err != nil {
		return nil, nil, err
	}
	definitions, err := queryParameterDefinitions(content)
	if err != nil {
		return nil, nil, err
	}
	if len(definitions) == 0 {
		return contract, nil, nil
	}
	effective := make(map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		value := definition.Default
		if override, exists := overrides[definition.Name]; exists {
			value = override
		}
		normalized, err := normalizeQueryParameterValue(definition.Type, value)
		if err != nil {
			return nil, nil, fmt.Errorf("查询参数 %s 无效: %w", definition.Name, err)
		}
		effective[definition.Name] = normalized
	}
	return contract, effective, nil
}

func queryParameterDefinitions(content map[string]interface{}) ([]models.QueryParameterDefinition, error) {
	if content == nil {
		return nil, nil
	}
	raw, exists := content["query_parameters"]
	if !exists || raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("content.query_parameters 无效: %w", err)
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &items); err != nil {
		return nil, fmt.Errorf("content.query_parameters 必须是数组: %w", err)
	}
	definitions := make([]models.QueryParameterDefinition, 0, len(items))
	seen := map[string]struct{}{}
	for index, item := range items {
		allowed := map[string]struct{}{
			"name": {}, "type": {}, "default": {}, "title": {}, "description": {},
		}
		unknown := make([]string, 0)
		for field := range item {
			if _, ok := allowed[field]; !ok {
				unknown = append(unknown, field)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return nil, fmt.Errorf("content.query_parameters[%d] 包含未知字段: %s", index, strings.Join(unknown, ", "))
		}
		var definition models.QueryParameterDefinition
		if err := decodeRequiredString(item, "name", &definition.Name); err != nil {
			return nil, fmt.Errorf("content.query_parameters[%d].name %w", index, err)
		}
		if !commonquery.ValidName(definition.Name) {
			return nil, fmt.Errorf("content.query_parameters[%d].name 必须是字母或下划线开头的标识符", index)
		}
		if _, duplicate := seen[definition.Name]; duplicate {
			return nil, fmt.Errorf("content.query_parameters 参数名重复: %s", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		if err := decodeRequiredString(item, "type", &definition.Type); err != nil {
			return nil, fmt.Errorf("content.query_parameters[%d].type %w", index, err)
		}
		definition.Type = strings.ToLower(definition.Type)
		if _, ok := allowedQueryParameterTypes[definition.Type]; !ok {
			return nil, fmt.Errorf("content.query_parameters[%d].type 不支持: %s", index, definition.Type)
		}
		defaultRaw, ok := item["default"]
		if !ok || bytes.Equal(bytes.TrimSpace(defaultRaw), []byte("null")) {
			return nil, fmt.Errorf("content.query_parameters[%d].default 必须提供非空默认值", index)
		}
		defaultValue, err := decodeJSONValue(defaultRaw)
		if err != nil {
			return nil, fmt.Errorf("content.query_parameters[%d].default 无效: %w", index, err)
		}
		definition.Default, err = normalizeQueryParameterValue(definition.Type, defaultValue)
		if err != nil {
			return nil, fmt.Errorf("content.query_parameters[%d].default 无效: %w", index, err)
		}
		if rawTitle, ok := item["title"]; ok {
			if err := json.Unmarshal(rawTitle, &definition.Title); err != nil {
				return nil, fmt.Errorf("content.query_parameters[%d].title 必须是字符串", index)
			}
			definition.Title = strings.TrimSpace(definition.Title)
		}
		if rawDescription, ok := item["description"]; ok {
			if err := json.Unmarshal(rawDescription, &definition.Description); err != nil {
				return nil, fmt.Errorf("content.query_parameters[%d].description 必须是字符串", index)
			}
			definition.Description = strings.TrimSpace(definition.Description)
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func decodeRequiredString(item map[string]json.RawMessage, field string, target *string) error {
	raw, ok := item[field]
	if !ok {
		return fmt.Errorf("不能为空")
	}
	if err := json.Unmarshal(raw, target); err != nil || strings.TrimSpace(*target) == "" {
		return fmt.Errorf("必须是非空字符串")
	}
	*target = strings.TrimSpace(*target)
	return nil
}

func decodeJSONValue(raw json.RawMessage) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func normalizeQueryParameterValue(parameterType string, value interface{}) (interface{}, error) {
	switch parameterType {
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("必须是字符串")
		}
		return text, nil
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("必须是布尔值")
		}
		return boolean, nil
	case "integer":
		number, ok := queryParameterNumber(value)
		if !ok || math.Trunc(number) != number || number > math.MaxInt64 || number < math.MinInt64 {
			return nil, fmt.Errorf("必须是整数")
		}
		return int64(number), nil
	case "number":
		number, ok := queryParameterNumber(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("必须是有限数字")
		}
		return number, nil
	default:
		return nil, fmt.Errorf("不支持的参数类型: %s", parameterType)
	}
}

func queryParameterNumber(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	default:
		return 0, false
	}
}
