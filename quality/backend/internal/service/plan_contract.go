package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/addp/common/dataquality"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
)

const (
	planSchemaVersion          = "addp.quality.plan-rules/v1"
	planExecutionConfigVersion = "addp.quality.plan-execution-config/v2"
	planResultVersion          = "addp.quality.plan-result/v1"
)

var planNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type PlanTableBinding = models.PlanTableBinding

type PlanRuleDocument struct {
	SchemaVersion string     `json:"schema_version"`
	Rules         []PlanRule `json:"rules"`
}

type PlanRule struct {
	RuleID     int64           `json:"rule_id,omitempty"`
	RevisionNo int64           `json:"revision_no,omitempty"`
	Disabled   bool            `json:"disabled,omitempty"`
	Name       string          `json:"name,omitempty"`
	Source     *RuleSource     `json:"source,omitempty"`
	RuleKey    string          `json:"rule_key"`
	Type       string          `json:"type"`
	Severity   string          `json:"severity"`
	Params     json.RawMessage `json:"params"`
}

type planNotNullParams struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

type planAllowedValuesParams struct {
	Table  string   `json:"table"`
	Column string   `json:"column"`
	Values []string `json:"values"`
}

type planUniqueKeyParams struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

type planForeignKeyParams struct {
	Table            string   `json:"table"`
	Columns          []string `json:"columns"`
	ReferenceTable   string   `json:"reference_table"`
	ReferenceColumns []string `json:"reference_columns"`
}

type planCondition struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value,omitempty"`
}

type planPredicateImplicationParams struct {
	Table string        `json:"table"`
	When  planCondition `json:"when"`
	Then  planCondition `json:"then"`
}

type planRowCountParams struct {
	Table string `json:"table"`
	Exact *int64 `json:"exact,omitempty"`
	Min   *int64 `json:"min,omitempty"`
	Max   *int64 `json:"max,omitempty"`
}

func validatePlanContract(bindings []PlanTableBinding, raw json.RawMessage) (*PlanRuleDocument, error) {
	if err := models.ValidateTableBindings(bindings, false); err != nil {
		return nil, err
	}
	aliases := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		aliases[binding.Alias] = struct{}{}
	}

	var document PlanRuleDocument
	if err := decodeStrictJSON(raw, &document); err != nil {
		return nil, fmt.Errorf("rules document is invalid: %w", err)
	}
	if document.SchemaVersion != planSchemaVersion || len(document.Rules) == 0 || len(document.Rules) > 500 {
		return nil, fmt.Errorf("rules document version or size is invalid")
	}
	keys := make(map[string]struct{}, len(document.Rules))
	for index := range document.Rules {
		rule := &document.Rules[index]
		parsedKey, err := uuid.Parse(rule.RuleKey)
		if err != nil || parsedKey.String() != rule.RuleKey {
			return nil, fmt.Errorf("rule_key must be a lowercase canonical UUID")
		}
		if _, exists := keys[rule.RuleKey]; exists {
			return nil, fmt.Errorf("rule_key is duplicated")
		}
		keys[rule.RuleKey] = struct{}{}
		if len(rule.Name) > 500 {
			return nil, fmt.Errorf("rule name is too long")
		}
		if rule.Source != nil {
			key, err := uuid.Parse(rule.Source.RuleKey)
			if err != nil || key.String() != rule.Source.RuleKey || rule.Source.ElementID <= 0 || rule.Source.ElementRevisionID <= 0 {
				return nil, fmt.Errorf("invalid standard rule source")
			}
		}
		if rule.Severity != "error" && rule.Severity != "warning" && rule.Severity != "info" {
			return nil, fmt.Errorf("rule severity is invalid")
		}
		if err := validatePlanRule(*rule, aliases); err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.RuleKey, err)
		}
	}
	return &document, nil
}

func validatePlanRule(rule PlanRule, aliases map[string]struct{}) error {
	requireTable := func(table string) error {
		if _, exists := aliases[table]; !exists {
			return fmt.Errorf("table alias %q is not bound", table)
		}
		return nil
	}
	requireColumns := func(columns []string) error {
		if len(columns) == 0 || len(columns) > 100 {
			return fmt.Errorf("columns must be non-empty")
		}
		seen := make(map[string]struct{}, len(columns))
		for _, column := range columns {
			if strings.TrimSpace(column) == "" || len(column) > 200 || strings.TrimSpace(column) != column {
				return fmt.Errorf("column %q is invalid", column)
			}
			if _, exists := seen[column]; exists {
				return fmt.Errorf("column %q is duplicated", column)
			}
			seen[column] = struct{}{}
		}
		return nil
	}
	switch rule.Type {
	case "format", "length", "value_range":
		var params planValueParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns([]string{params.Column}) != nil {
			return fmt.Errorf("value constraint target is invalid")
		}
		valueRule := dataquality.Rule{RuleKey: rule.RuleKey, Type: rule.Type, Enabled: true, Severity: rule.Severity, Params: params.Constraint}
		if err := valueRule.Validate(); err != nil {
			return err
		}
	case "not_null":
		var params planNotNullParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns([]string{params.Column}) != nil {
			return fmt.Errorf("not_null params are invalid")
		}
	case "allowed_values":
		var params planAllowedValuesParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns([]string{params.Column}) != nil || validateAllowedValues(params.Values) != nil {
			return fmt.Errorf("allowed_values params are invalid")
		}
	case "unique_key":
		var params planUniqueKeyParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns(params.Columns) != nil {
			return fmt.Errorf("unique_key params are invalid")
		}
	case "foreign_key":
		var params planForeignKeyParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || requireTable(params.ReferenceTable) != nil || requireColumns(params.Columns) != nil || requireColumns(params.ReferenceColumns) != nil || len(params.Columns) != len(params.ReferenceColumns) {
			return fmt.Errorf("foreign_key params are invalid")
		}
	case "predicate_implication":
		var params planPredicateImplicationParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil || validatePlanCondition(params.When) != nil || validatePlanCondition(params.Then) != nil {
			return fmt.Errorf("predicate_implication params are invalid")
		}
	case "row_count":
		var params planRowCountParams
		if err := decodeStrictJSON(rule.Params, &params); err != nil || requireTable(params.Table) != nil {
			return fmt.Errorf("row_count params are invalid")
		}
		if params.Exact != nil {
			if params.Min != nil || params.Max != nil || *params.Exact < 0 {
				return fmt.Errorf("row_count exact is invalid")
			}
		} else if (params.Min == nil && params.Max == nil) || (params.Min != nil && *params.Min < 0) || (params.Max != nil && *params.Max < 0) || (params.Min != nil && params.Max != nil && *params.Min > *params.Max) {
			return fmt.Errorf("row_count range is invalid")
		}
	default:
		return fmt.Errorf("rule type %q is unsupported", rule.Type)
	}
	return nil
}

func validateAllowedValues(values []string) error {
	if len(values) == 0 || len(values) > 1000 {
		return fmt.Errorf("values must contain between 1 and 1000 items")
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if value == "" {
			return fmt.Errorf("value at index %d is empty", index)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("value %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validatePlanCondition(condition planCondition) error {
	if strings.TrimSpace(condition.Column) == "" || len(condition.Column) > 200 || strings.TrimSpace(condition.Column) != condition.Column {
		return fmt.Errorf("condition column is invalid")
	}
	switch condition.Operator {
	case "eq", "not_eq":
		if condition.Value == nil {
			return fmt.Errorf("condition value is required")
		}
		switch condition.Value.(type) {
		case string, float64, bool:
		default:
			return fmt.Errorf("condition value must be a JSON scalar")
		}
	case "is_null", "is_not_null", "is_true", "is_false":
		if condition.Value != nil {
			return fmt.Errorf("condition value is not allowed")
		}
	default:
		return fmt.Errorf("condition op is unsupported")
	}
	return nil
}

func decodeStrictJSON(raw []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func normalizeGateText(value string) string { return strings.TrimSpace(value) }
