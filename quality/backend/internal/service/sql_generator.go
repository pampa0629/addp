package service

import (
	"fmt"
	"strings"
)

// SQLGenerator 根据规则类型生成质量检查 SQL
type SQLGenerator struct{}

func NewSQLGenerator() *SQLGenerator {
	return &SQLGenerator{}
}

// GenerateCheckSQL 为单条规则生成检查 SQL，返回 (checkSQL, countSQL, error)
// checkSQL: 返回失败行数的 SQL
// countSQL: 返回总行数的 SQL
func (g *SQLGenerator) GenerateCheckSQL(schemaName, tableName, columnName string, ruleType string, ruleConfig map[string]interface{}) (string, string, error) {
	table := tableName
	if schemaName != "" {
		table = fmt.Sprintf("%s.%s", schemaName, tableName)
	}

	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)

	var failSQL string
	switch ruleType {
	case "not_null":
		failSQL = fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL", table, columnName)

	case "unique":
		failSQL = fmt.Sprintf(
			"SELECT COUNT(*) - COUNT(DISTINCT %s) FROM %s WHERE %s IS NOT NULL",
			columnName, table, columnName,
		)

	case "format":
		pattern, ok := getStringParam(ruleConfig, "pattern")
		if !ok {
			return "", "", fmt.Errorf("rule 'format' requires 'pattern' param")
		}
		failSQL = fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND %s !~ '%s'",
			table, columnName, columnName, escapeSQLString(pattern),
		)

	case "length":
		minLen, hasMin := getIntParam(ruleConfig, "min_length")
		maxLen, hasMax := getIntParam(ruleConfig, "max_length")
		conditions := []string{}
		if hasMin {
			conditions = append(conditions, fmt.Sprintf("LENGTH(%s::text) < %d", columnName, minLen))
		}
		if hasMax {
			conditions = append(conditions, fmt.Sprintf("LENGTH(%s::text) > %d", columnName, maxLen))
		}
		if len(conditions) == 0 {
			return "", "", fmt.Errorf("rule 'length' requires 'min_length' or 'max_length'")
		}
		failSQL = fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND (%s)",
			table, columnName, strings.Join(conditions, " OR "),
		)

	case "value_range":
		minVal, hasMin := getFloatParam(ruleConfig, "min_value")
		maxVal, hasMax := getFloatParam(ruleConfig, "max_value")
		conditions := []string{}
		if hasMin {
			conditions = append(conditions, fmt.Sprintf("%s::numeric < %v", columnName, minVal))
		}
		if hasMax {
			conditions = append(conditions, fmt.Sprintf("%s::numeric > %v", columnName, maxVal))
		}
		if len(conditions) == 0 {
			return "", "", fmt.Errorf("rule 'value_range' requires 'min_value' or 'max_value'")
		}
		failSQL = fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND (%s)",
			table, columnName, strings.Join(conditions, " OR "),
		)

	case "allowed_values":
		vals, ok := getStringSliceParam(ruleConfig, "values")
		if !ok || len(vals) == 0 {
			return "", "", fmt.Errorf("rule 'allowed_values' requires non-empty 'values' list")
		}
		quoted := make([]string, len(vals))
		for i, v := range vals {
			quoted[i] = fmt.Sprintf("'%s'", escapeSQLString(v))
		}
		failSQL = fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND %s NOT IN (%s)",
			table, columnName, columnName, strings.Join(quoted, ","),
		)

	default:
		return "", "", fmt.Errorf("unsupported rule type: %s", ruleType)
	}

	return failSQL, countSQL, nil
}

func getStringParam(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getIntParam(m map[string]interface{}, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func getFloatParam(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	}
	return 0, false
}

func getStringSliceParam(m map[string]interface{}, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result, true
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
