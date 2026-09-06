package service

import (
	"fmt"
	"strings"

	commonquery "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
)

const maxQueryServiceNamedParameters = 32

func validateQueryServiceNamedParameters(configType, query string, definitions []models.QueryServiceNamedParameter) ([]models.QueryServiceNamedParameter, error) {
	if configType != "sql" {
		if len(definitions) > 0 {
			return nil, fmt.Errorf("named_parameters are only valid in sql mode")
		}
		return []models.QueryServiceNamedParameter{}, nil
	}
	if len(definitions) > maxQueryServiceNamedParameters {
		return nil, fmt.Errorf("named_parameters must contain at most %d items", maxQueryServiceNamedParameters)
	}
	references, err := commonquery.SQLReferences(query)
	if err != nil {
		return nil, fmt.Errorf("parse sql named parameters: %w", err)
	}
	result := make([]models.QueryServiceNamedParameter, 0, len(definitions))
	defined := make(map[string]struct{}, len(definitions))
	for index, definition := range definitions {
		definition.Name = strings.TrimSpace(definition.Name)
		definition.Description = strings.TrimSpace(definition.Description)
		if definition.Name == "" || len(definition.Name) > 64 || !commonquery.ValidName(definition.Name) {
			return nil, fmt.Errorf("named_parameters[%d].name is invalid", index)
		}
		if len([]rune(definition.Description)) > 500 {
			return nil, fmt.Errorf("named_parameters[%d].description is too long", index)
		}
		if !isStableOrderFieldType(definition.Type) {
			return nil, fmt.Errorf("named_parameters[%d].type %s is not a supported scalar type", index, definition.Type)
		}
		if _, duplicate := defined[definition.Name]; duplicate {
			return nil, fmt.Errorf("named_parameters contains duplicate name %s", definition.Name)
		}
		defined[definition.Name] = struct{}{}
		if definition.Required && definition.Default != nil {
			return nil, fmt.Errorf("required named parameter %s must not declare a default", definition.Name)
		}
		if !definition.Required && definition.Default == nil {
			return nil, fmt.Errorf("optional named parameter %s requires a default", definition.Name)
		}
		if definition.Default != nil {
			normalized, normalizeErr := normalizeBoundValue(definition.Default, definition.Type)
			if normalizeErr != nil {
				return nil, fmt.Errorf("named parameter %s default is invalid: %w", definition.Name, normalizeErr)
			}
			definition.Default = normalized
		}
		result = append(result, definition)
	}
	referenceSet := make(map[string]struct{}, len(references))
	for _, reference := range references {
		referenceSet[reference] = struct{}{}
		if _, exists := defined[reference]; !exists {
			return nil, fmt.Errorf("sql references undeclared named parameter %s", reference)
		}
	}
	for name := range defined {
		if _, exists := referenceSet[name]; !exists {
			return nil, fmt.Errorf("named parameter %s is not referenced by sql", name)
		}
	}
	return result, nil
}

func bindQueryServiceNamedParameters(service *models.QueryService, engineType, baseSQL string, provided map[string]interface{}) (string, []interface{}, map[string]interface{}, error) {
	if service == nil {
		return "", nil, nil, fmt.Errorf("%w: query service is missing", ErrInvalidStructuredQuery)
	}
	if !service.IsSQLMode() {
		if len(provided) > 0 {
			return "", nil, nil, fmt.Errorf("%w: table mode does not accept named parameters", ErrInvalidStructuredQuery)
		}
		return baseSQL, nil, map[string]interface{}{}, nil
	}
	definitions, err := validateQueryServiceNamedParameters(service.ConfigType, service.SqlQuery, service.NamedParameters)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: %v", ErrInvalidStructuredQuery, err)
	}
	definitionByName := make(map[string]models.QueryServiceNamedParameter, len(definitions))
	for _, definition := range definitions {
		definitionByName[definition.Name] = definition
	}
	for name := range provided {
		if _, exists := definitionByName[name]; !exists {
			return "", nil, nil, fmt.Errorf("%w: unknown named parameter %s", ErrInvalidStructuredQuery, name)
		}
	}
	resolved := make(map[string]interface{}, len(definitions))
	for _, definition := range definitions {
		value, exists := provided[definition.Name]
		if !exists {
			if definition.Required {
				return "", nil, nil, fmt.Errorf("%w: required named parameter %s is missing", ErrInvalidStructuredQuery, definition.Name)
			}
			value = definition.Default
		}
		normalized, normalizeErr := normalizeBoundValue(value, definition.Type)
		if normalizeErr != nil {
			return "", nil, nil, fmt.Errorf("%w: named parameter %s is invalid: %v", ErrInvalidStructuredQuery, definition.Name, normalizeErr)
		}
		resolved[definition.Name] = normalized
	}
	dialect, dialectErr := queryPlanDialect(engineType)
	if dialectErr != nil {
		return "", nil, nil, dialectErr
	}
	boundSQL, args, err := commonquery.BindSQL(baseSQL, resolved, commonquery.SQLPlaceholderStyleForDialect(dialect.Name()))
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: bind named parameters: %v", ErrInvalidStructuredQuery, err)
	}
	return boundSQL, args, resolved, nil
}
