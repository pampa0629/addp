package profilefilter

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataprofile"
	"github.com/addp/common/datatype"
	"github.com/addp/common/sqldialect"
)

const (
	maxConditions = 8
	maxSetValues  = 20
)

var ErrInvalidScope = errors.New("invalid data profile scope")

var commonOperators = map[string]bool{
	"eq": true, "ne": true, "is_null": true, "is_not_null": true,
}

// Normalize validates a profiling data scope against the canonical Meta fields
// and returns a stable representation suitable for hashing and execution.
func Normalize(scope dataprofile.DataScope, fields []datatype.FieldInfo) (dataprofile.DataScope, error) {
	scope.Kind = strings.ToLower(strings.TrimSpace(scope.Kind))
	if scope.Kind == "" || scope.Kind == dataprofile.DataScopeKindAll {
		if strings.TrimSpace(scope.Logic) != "" || len(scope.Conditions) > 0 {
			return dataprofile.DataScope{}, fmt.Errorf("%w: all scope cannot contain conditions", ErrInvalidScope)
		}
		return dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, nil
	}
	if scope.Kind != dataprofile.DataScopeKindCondition {
		return dataprofile.DataScope{}, fmt.Errorf("%w: unsupported scope kind", ErrInvalidScope)
	}
	scope.Logic = strings.ToLower(strings.TrimSpace(scope.Logic))
	if scope.Logic != dataprofile.DataScopeLogicAnd && scope.Logic != dataprofile.DataScopeLogicOr {
		return dataprofile.DataScope{}, fmt.Errorf("%w: condition logic must be and or or", ErrInvalidScope)
	}
	if len(scope.Conditions) == 0 || len(scope.Conditions) > maxConditions {
		return dataprofile.DataScope{}, fmt.Errorf("%w: condition count must be between 1 and %d", ErrInvalidScope, maxConditions)
	}

	fieldsByName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		fieldsByName[field.Name] = field
	}
	normalized := make([]dataprofile.DataScopeCondition, 0, len(scope.Conditions))
	for _, condition := range scope.Conditions {
		field, ok := fieldsByName[strings.TrimSpace(condition.Field)]
		if !ok {
			return dataprofile.DataScope{}, fmt.Errorf("%w: condition references an unknown field", ErrInvalidScope)
		}
		item, err := normalizeCondition(condition, field)
		if err != nil {
			return dataprofile.DataScope{}, err
		}
		normalized = append(normalized, item)
	}

	sort.Slice(normalized, func(i, j int) bool {
		left, _ := json.Marshal(normalized[i])
		right, _ := json.Marshal(normalized[j])
		return string(left) < string(right)
	})
	deduplicated := normalized[:0]
	var previous string
	for _, condition := range normalized {
		payload, _ := json.Marshal(condition)
		key := string(payload)
		if key == previous {
			continue
		}
		deduplicated = append(deduplicated, condition)
		previous = key
	}
	return dataprofile.DataScope{
		Kind:       dataprofile.DataScopeKindCondition,
		Logic:      scope.Logic,
		Conditions: deduplicated,
	}, nil
}

func normalizeCondition(condition dataprofile.DataScopeCondition, field datatype.FieldInfo) (dataprofile.DataScopeCondition, error) {
	condition.Field = strings.TrimSpace(condition.Field)
	condition.Operator = strings.ToLower(strings.TrimSpace(condition.Operator))
	if !operatorAllowed(field.Type, condition.Operator) {
		return dataprofile.DataScopeCondition{}, fmt.Errorf("%w: operator is not available for field type %s", ErrInvalidScope, field.Type)
	}

	result := dataprofile.DataScopeCondition{Field: condition.Field, Operator: condition.Operator}
	switch condition.Operator {
	case "is_null", "is_not_null":
		if condition.Value != nil || len(condition.Values) > 0 {
			return dataprofile.DataScopeCondition{}, fmt.Errorf("%w: null operators do not accept values", ErrInvalidScope)
		}
	case "between":
		if condition.Value != nil || len(condition.Values) != 2 {
			return dataprofile.DataScopeCondition{}, fmt.Errorf("%w: between requires two values", ErrInvalidScope)
		}
		values, err := normalizeValues(condition.Values, field.Type)
		if err != nil {
			return dataprofile.DataScopeCondition{}, err
		}
		result.Values = values
	case "in", "not_in":
		if condition.Value != nil || len(condition.Values) == 0 || len(condition.Values) > maxSetValues {
			return dataprofile.DataScopeCondition{}, fmt.Errorf("%w: set operator requires between 1 and %d values", ErrInvalidScope, maxSetValues)
		}
		values, err := normalizeValues(condition.Values, field.Type)
		if err != nil {
			return dataprofile.DataScopeCondition{}, err
		}
		result.Values = deduplicateValues(values)
	default:
		if condition.Value == nil || len(condition.Values) > 0 {
			return dataprofile.DataScopeCondition{}, fmt.Errorf("%w: operator requires one value", ErrInvalidScope)
		}
		value, err := normalizeValue(condition.Value, field.Type)
		if err != nil {
			return dataprofile.DataScopeCondition{}, err
		}
		result.Value = value
	}
	return result, nil
}

func operatorAllowed(fieldType datatype.FieldType, operator string) bool {
	if operator == "is_null" || operator == "is_not_null" {
		return true
	}
	if commonOperators[operator] {
		return !datatype.IsSpatialFieldType(fieldType) && fieldType != datatype.FieldTypeBytes && !datatype.IsSemiStructuredFieldType(fieldType)
	}
	if operator == "in" || operator == "not_in" {
		return datatype.IsNumericFieldType(fieldType) || datatype.IsTemporalFieldType(fieldType) || fieldType == datatype.FieldTypeString || fieldType == datatype.FieldTypeUUID || fieldType == datatype.FieldTypeBool
	}
	if operator == "between" || operator == "gt" || operator == "gte" || operator == "lt" || operator == "lte" {
		return datatype.IsNumericFieldType(fieldType) || datatype.IsTemporalFieldType(fieldType)
	}
	if operator == "contains" || operator == "starts_with" {
		return fieldType == datatype.FieldTypeString
	}
	return false
}

func normalizeValues(values []interface{}, fieldType datatype.FieldType) ([]interface{}, error) {
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeValue(value, fieldType)
		if err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeValue(value interface{}, fieldType datatype.FieldType) (interface{}, error) {
	switch {
	case datatype.IsNumericFieldType(fieldType):
		number, ok := numericValue(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%w: numeric field requires a finite number", ErrInvalidScope)
		}
		if fieldType == datatype.FieldTypeInt || fieldType == datatype.FieldTypeBigInt {
			if math.Trunc(number) != number {
				return nil, fmt.Errorf("%w: integer field requires an integer value", ErrInvalidScope)
			}
			return int64(number), nil
		}
		return number, nil
	case datatype.IsTemporalFieldType(fieldType):
		text, ok := value.(string)
		if !ok || !validTemporalValue(strings.TrimSpace(text), fieldType) {
			return nil, fmt.Errorf("%w: temporal field requires an ISO value", ErrInvalidScope)
		}
		return strings.TrimSpace(text), nil
	case fieldType == datatype.FieldTypeBool:
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%w: boolean field requires a boolean value", ErrInvalidScope)
		}
		return boolean, nil
	case fieldType == datatype.FieldTypeString || fieldType == datatype.FieldTypeUUID || fieldType == datatype.FieldTypeUnknown || fieldType == datatype.FieldTypeMixed:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: field requires a string value", ErrInvalidScope)
		}
		return text, nil
	default:
		return nil, fmt.Errorf("%w: field type does not accept values", ErrInvalidScope)
	}
}

func numericValue(value interface{}) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func validTemporalValue(value string, fieldType datatype.FieldType) bool {
	layouts := []string{time.RFC3339Nano}
	switch fieldType {
	case datatype.FieldTypeDate:
		layouts = append(layouts, time.DateOnly)
	case datatype.FieldTypeTime:
		layouts = append(layouts, "15:04:05.999999999", "15:04:05")
	case datatype.FieldTypeTimestamp:
		layouts = append(layouts, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05")
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

func deduplicateValues(values []interface{}) []interface{} {
	seen := map[string]bool{}
	result := make([]interface{}, 0, len(values))
	for _, value := range values {
		payload, _ := json.Marshal(value)
		key := string(payload)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

// SQL compiles a normalized condition scope into a parameterized WHERE clause.
func SQL(scope dataprofile.DataScope, dialect sqldialect.Dialect, tableAlias string) (string, []interface{}, error) {
	if scope.Kind == "" || scope.Kind == dataprofile.DataScopeKindAll {
		return "", nil, nil
	}
	if scope.Kind != dataprofile.DataScopeKindCondition || len(scope.Conditions) == 0 {
		return "", nil, fmt.Errorf("%w: condition scope is empty", ErrInvalidScope)
	}
	joiner := " AND "
	if scope.Logic == dataprofile.DataScopeLogicOr {
		joiner = " OR "
	} else if scope.Logic != dataprofile.DataScopeLogicAnd {
		return "", nil, fmt.Errorf("%w: unsupported condition logic", ErrInvalidScope)
	}

	clauses := make([]string, 0, len(scope.Conditions))
	args := make([]interface{}, 0, len(scope.Conditions)*2)
	for _, condition := range scope.Conditions {
		column := dialect.QuoteIdentifier(condition.Field)
		if tableAlias != "" {
			column = dialect.QuoteIdentifier(tableAlias) + "." + column
		}
		switch condition.Operator {
		case "eq", "ne", "gt", "gte", "lt", "lte":
			symbols := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
			clauses = append(clauses, column+" "+symbols[condition.Operator]+" ?")
			args = append(args, condition.Value)
		case "is_null":
			clauses = append(clauses, column+" IS NULL")
		case "is_not_null":
			clauses = append(clauses, column+" IS NOT NULL")
		case "between":
			clauses = append(clauses, column+" BETWEEN ? AND ?")
			args = append(args, condition.Values[0], condition.Values[1])
		case "in", "not_in":
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(condition.Values)), ",")
			keyword := " IN "
			if condition.Operator == "not_in" {
				keyword = " NOT IN "
			}
			clauses = append(clauses, column+keyword+"("+placeholders+")")
			args = append(args, condition.Values...)
		case "contains", "starts_with":
			text := escapeLikeValue(fmt.Sprint(condition.Value))
			if condition.Operator == "contains" {
				text = "%" + text + "%"
			} else {
				text += "%"
			}
			clauses = append(clauses, column+" LIKE ? ESCAPE '!'")
			args = append(args, text)
		default:
			return "", nil, fmt.Errorf("%w: unsupported condition operator", ErrInvalidScope)
		}
	}
	return "(" + strings.Join(clauses, joiner) + ")", args, nil
}

func escapeLikeValue(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}
