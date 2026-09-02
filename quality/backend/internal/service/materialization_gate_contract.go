package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/google/uuid"
)

const (
	materializationGateSchemaVersion          = "addp.quality.materialization-gate/v1"
	materializationGateExecutionConfigVersion = "addp.quality.materialization-gate-execution-config/v1"
	materializationGateResultVersion          = "addp.quality.materialization-gate-result/v1"
)

var materializationGateNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type MaterializationGateTableBinding struct {
	Alias          string `json:"alias"`
	LogicalTableID int64  `json:"logical_table_id"`
}

type MaterializationGateAssertionDocument struct {
	SchemaVersion string                         `json:"schema_version"`
	Assertions    []MaterializationGateAssertion `json:"assertions"`
}

type MaterializationGateAssertion struct {
	AssertionKey string          `json:"assertion_key"`
	Type         string          `json:"type"`
	Severity     string          `json:"severity"`
	Params       json.RawMessage `json:"params"`
}

type gateNotNullParams struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

type gateAllowedValuesParams struct {
	Table  string   `json:"table"`
	Column string   `json:"column"`
	Values []string `json:"values"`
}

type gateUniqueKeyParams struct {
	Table   string   `json:"table"`
	Columns []string `json:"columns"`
}

type gateForeignKeyParams struct {
	Table            string   `json:"table"`
	Columns          []string `json:"columns"`
	ReferenceTable   string   `json:"reference_table"`
	ReferenceColumns []string `json:"reference_columns"`
}

type gateCondition struct {
	Column   string      `json:"column"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value,omitempty"`
}

type gatePredicateImplicationParams struct {
	Table string        `json:"table"`
	When  gateCondition `json:"when"`
	Then  gateCondition `json:"then"`
}

type gateRowCountParams struct {
	Table string `json:"table"`
	Exact *int64 `json:"exact,omitempty"`
	Min   *int64 `json:"min,omitempty"`
	Max   *int64 `json:"max,omitempty"`
}

func validateMaterializationGateContract(bindings []MaterializationGateTableBinding, raw json.RawMessage) (*MaterializationGateAssertionDocument, error) {
	if len(bindings) == 0 || len(bindings) > 100 {
		return nil, fmt.Errorf("table_bindings must contain between 1 and 100 items")
	}
	aliases := make(map[string]struct{}, len(bindings))
	logicalTables := make(map[int64]struct{}, len(bindings))
	for _, binding := range bindings {
		if !materializationGateNamePattern.MatchString(binding.Alias) || binding.LogicalTableID <= 0 {
			return nil, fmt.Errorf("table binding is invalid")
		}
		if _, exists := aliases[binding.Alias]; exists {
			return nil, fmt.Errorf("table binding alias is duplicated")
		}
		if _, exists := logicalTables[binding.LogicalTableID]; exists {
			return nil, fmt.Errorf("logical table binding is duplicated")
		}
		aliases[binding.Alias] = struct{}{}
		logicalTables[binding.LogicalTableID] = struct{}{}
	}

	var document MaterializationGateAssertionDocument
	if err := decodeStrictJSON(raw, &document); err != nil {
		return nil, fmt.Errorf("assertions document is invalid: %w", err)
	}
	if document.SchemaVersion != materializationGateSchemaVersion || len(document.Assertions) == 0 || len(document.Assertions) > 500 {
		return nil, fmt.Errorf("assertions document version or size is invalid")
	}
	keys := make(map[string]struct{}, len(document.Assertions))
	for index := range document.Assertions {
		assertion := &document.Assertions[index]
		parsedKey, err := uuid.Parse(assertion.AssertionKey)
		if err != nil || parsedKey.String() != assertion.AssertionKey {
			return nil, fmt.Errorf("assertion_key must be a lowercase canonical UUID")
		}
		if _, exists := keys[assertion.AssertionKey]; exists {
			return nil, fmt.Errorf("assertion_key is duplicated")
		}
		keys[assertion.AssertionKey] = struct{}{}
		if assertion.Severity != "error" && assertion.Severity != "warning" && assertion.Severity != "info" {
			return nil, fmt.Errorf("assertion severity is invalid")
		}
		if err := validateGateAssertion(*assertion, aliases); err != nil {
			return nil, fmt.Errorf("assertion %s: %w", assertion.AssertionKey, err)
		}
	}
	return &document, nil
}

func validateGateAssertion(assertion MaterializationGateAssertion, aliases map[string]struct{}) error {
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
	switch assertion.Type {
	case "not_null":
		var params gateNotNullParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns([]string{params.Column}) != nil {
			return fmt.Errorf("not_null params are invalid")
		}
	case "allowed_values":
		var params gateAllowedValuesParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns([]string{params.Column}) != nil || validateAllowedValues(params.Values) != nil {
			return fmt.Errorf("allowed_values params are invalid")
		}
	case "unique_key":
		var params gateUniqueKeyParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil || requireColumns(params.Columns) != nil {
			return fmt.Errorf("unique_key params are invalid")
		}
	case "foreign_key":
		var params gateForeignKeyParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil || requireTable(params.ReferenceTable) != nil || requireColumns(params.Columns) != nil || requireColumns(params.ReferenceColumns) != nil || len(params.Columns) != len(params.ReferenceColumns) {
			return fmt.Errorf("foreign_key params are invalid")
		}
	case "predicate_implication":
		var params gatePredicateImplicationParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil || validateGateCondition(params.When) != nil || validateGateCondition(params.Then) != nil {
			return fmt.Errorf("predicate_implication params are invalid")
		}
	case "row_count":
		var params gateRowCountParams
		if err := decodeStrictJSON(assertion.Params, &params); err != nil || requireTable(params.Table) != nil {
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
		return fmt.Errorf("assertion type %q is unsupported", assertion.Type)
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

func validateGateCondition(condition gateCondition) error {
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

func validateGateGroup(group *commonClient.MaterializationGroup, bindings []MaterializationGateTableBinding, expectedVersion int64) error {
	if group == nil || (expectedVersion > 0 && group.Version != expectedVersion) || len(group.Members) != len(bindings) {
		return fmt.Errorf("materialization group version or members changed")
	}
	members := make(map[int64]struct{}, len(group.Members))
	for _, member := range group.Members {
		members[member.LogicalTableID] = struct{}{}
	}
	for _, binding := range bindings {
		if _, exists := members[binding.LogicalTableID]; !exists {
			return fmt.Errorf("table bindings do not match materialization group")
		}
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
