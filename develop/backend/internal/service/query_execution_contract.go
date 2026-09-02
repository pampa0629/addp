package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	commonquery "github.com/addp/common/query"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/taskprovider"
	"github.com/addp/develop/backend/internal/models"
)

var allowedQueryParameterTypes = map[string]struct{}{
	"relation": {},
	"string":   {},
	"integer":  {},
	"number":   {},
	"boolean":  {},
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
	return buildQueryExecutionContract(content, true)
}

func buildQueryExecutionContract(content map[string]interface{}, includeResultTarget bool) (*taskprovider.ExecutionContract, error) {
	definitions, err := queryParameterDefinitions(content)
	if err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: err}
	}
	query, _ := content["query"].(string)
	language, _ := content["query_type"].(string)
	relationBindings, hasRelationParameters, err := relationParameterBindings(definitions)
	if err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: err}
	}
	if hasRelationParameters {
		if strings.ToLower(strings.TrimSpace(language)) != "sql" {
			return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("relation 查询参数仅支持 SQL 查询任务")}
		}
		if err := validateRelationResultSource(query, relationBindings); err != nil {
			return nil, &QueryParameterDefinitionsError{Cause: err}
		}
	}
	references, err := commonquery.References(language, query)
	if err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("查询参数引用无效: %w", err)}
	}
	definitionValues := make(map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		if definition.Type == "relation" {
			continue
		}
		definitionValues[definition.Name] = definition.Default
	}
	if err := commonquery.ValidateDefinitions(references, definitionValues); err != nil {
		return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("查询参数定义与查询不一致: %w", err)}
	}

	contract := taskprovider.EmptyExecutionContract()
	properties := contract.InputSchema["properties"].(map[string]interface{})
	required := make([]interface{}, 0, len(relationBindings)+1)
	for index, definition := range definitions {
		field := map[string]interface{}{"type": definition.Type}
		if definition.Description != "" {
			field["description"] = definition.Description
		}
		if definition.Type == "relation" {
			field["type"] = "object"
			field["properties"] = map[string]interface{}{
				"locator": map[string]interface{}{"type": "string", "format": "resource-locator", "minLength": float64(1)},
			}
			field["required"] = []interface{}{"locator"}
			field["additionalProperties"] = false
			properties[definition.Name] = field
			if definition.Default == nil {
				required = append(required, definition.Name)
			} else {
				contract.InputDefaults[definition.Name] = definition.Default
			}
			contract.InputUISchema[definition.Name] = map[string]interface{}{
				"control": "resource_tree_picker",
				"order":   index,
				"resource_binding": map[string]interface{}{
					"mode": "existing", "locator_param": "locator",
				},
				"api_base_url":          "/api/v1/meta",
				"engine_families":       []interface{}{"tabular"},
				"selectable_node_types": []interface{}{"table"},
			}
			continue
		}
		properties[definition.Name] = field
		if definition.Default == nil {
			required = append(required, definition.Name)
		} else {
			contract.InputDefaults[definition.Name] = definition.Default
		}
		contract.InputUISchema[definition.Name] = map[string]interface{}{"order": index}
	}
	if hasRelationParameters && includeResultTarget {
		if _, exists := properties["target_locator"]; exists {
			return nil, &QueryParameterDefinitionsError{Cause: fmt.Errorf("查询参数名称 target_locator 与运行时目标冲突")}
		}
		properties["target_locator"] = map[string]interface{}{"type": "string", "minLength": float64(1)}
		contract.InputUISchema["target_locator"] = map[string]interface{}{"order": len(definitions)}
		required = append(required, "target_locator")
		contract.InputSchema["required"] = required
		contract.OutputSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"execution_id":   map[string]interface{}{"type": "string"},
				"target_locator": map[string]interface{}{"type": "string"},
				"row_count":      map[string]interface{}{"type": "integer", "minimum": float64(0)},
			},
			"required":             []interface{}{"execution_id", "target_locator", "row_count"},
			"additionalProperties": false,
		}
	}
	if len(required) > 0 {
		contract.InputSchema["required"] = required
	}
	return &contract, nil
}

func resolveQueryPreviewParameters(
	content map[string]interface{},
	overrides map[string]interface{},
) (*taskprovider.ExecutionContract, map[string]interface{}, map[string]interface{}, error) {
	return resolveQueryParameters(content, overrides, false)
}

func resolveQueryOrchestrationParameters(
	content map[string]interface{},
	overrides map[string]interface{},
) (*taskprovider.ExecutionContract, map[string]interface{}, map[string]interface{}, error) {
	return resolveQueryParameters(content, overrides, true)
}

func resolveQueryParameters(
	content map[string]interface{},
	overrides map[string]interface{},
	includeResultTarget bool,
) (*taskprovider.ExecutionContract, map[string]interface{}, map[string]interface{}, error) {
	contract, err := buildQueryExecutionContract(content, includeResultTarget)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := taskprovider.ValidateExecutionParameters(
		contract.InputSchema,
		overrides,
		taskprovider.ParameterValidationOptions{},
	); err != nil {
		return nil, nil, nil, err
	}
	definitions, err := queryParameterDefinitions(content)
	if err != nil {
		return nil, nil, nil, err
	}
	var runtimeValues map[string]interface{}
	var effectiveInputs map[string]interface{}
	if len(definitions) > 0 || includeResultTarget {
		effectiveInputs = make(map[string]interface{}, len(definitions)+1)
	}
	for _, definition := range definitions {
		value := definition.Default
		if override, exists := overrides[definition.Name]; exists {
			value = override
		}
		if definition.Type == "relation" {
			normalized, normalizeErr := normalizeRelationParameterValue(value)
			if normalizeErr != nil {
				return nil, nil, nil, fmt.Errorf("查询参数 %s 无效: %w", definition.Name, normalizeErr)
			}
			effectiveInputs[definition.Name] = normalized
			continue
		}
		normalized, err := normalizeQueryParameterValue(definition.Type, value)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("查询参数 %s 无效: %w", definition.Name, err)
		}
		if runtimeValues == nil {
			runtimeValues = make(map[string]interface{})
		}
		runtimeValues[definition.Name] = normalized
		effectiveInputs[definition.Name] = normalized
	}
	if includeResultTarget {
		if target, exists := overrides["target_locator"]; exists {
			effectiveInputs["target_locator"] = target
		}
	}
	if len(effectiveInputs) == 0 {
		effectiveInputs = nil
	}
	return contract, runtimeValues, effectiveInputs, nil
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
			"name": {}, "type": {}, "default": {}, "description": {},
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
		defaultRaw, hasDefault := item["default"]
		if hasDefault && !bytes.Equal(bytes.TrimSpace(defaultRaw), []byte("null")) {
			defaultValue, decodeErr := decodeJSONValue(defaultRaw)
			if decodeErr != nil {
				return nil, fmt.Errorf("content.query_parameters[%d].default 无效: %w", index, decodeErr)
			}
			if definition.Type == "relation" {
				definition.Default, err = normalizeRelationParameterValue(defaultValue)
			} else {
				definition.Default, err = normalizeQueryParameterValue(definition.Type, defaultValue)
			}
			if err != nil {
				return nil, fmt.Errorf("content.query_parameters[%d].default 无效: %w", index, err)
			}
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

func normalizeRelationParameterValue(value interface{}) (map[string]interface{}, error) {
	object, ok := mapValue(value)
	if !ok {
		return nil, fmt.Errorf("必须是包含 locator 的数据表资源")
	}
	if len(object) != 1 {
		return nil, fmt.Errorf("只能包含 locator")
	}
	locatorText, ok := object["locator"].(string)
	if !ok || strings.TrimSpace(locatorText) == "" {
		return nil, fmt.Errorf("locator 必须是非空字符串")
	}
	locator, err := resourcetree.ParseURI(strings.TrimSpace(locatorText))
	if err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeTable || len(locator.Path) != 2 {
		return nil, fmt.Errorf("locator 必须指向已有数据表")
	}
	return map[string]interface{}{"locator": locator.ToURI()}, nil
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
