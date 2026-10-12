package postgresql

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

var _ plugin.TableWriteDefinitionChecker = (*PostgreSQLPlugin)(nil)

func (p *PostgreSQLPlugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	seen := map[string]bool{}
	for _, binding := range definition.Fields {
		field := binding.Target
		field.Name = strings.TrimSpace(field.Name)
		field.Type = datatype.ParseFieldType(string(field.Type))
		if field.Name == "" || seen[field.Name] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[field.Name] = true
		physical, err := postgresSQLTypeForField(field, definition.SpatialInfo)
		if err != nil {
			code := "field_definition_unsupported"
			if field.Type == datatype.FieldTypeArray {
				code = "array_element_type_unsupported"
			}
			if field.Type == datatype.FieldTypeDecimal {
				code = "decimal_definition_invalid"
			}
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, code, "")
			continue
		}
		status, code := plugin.DefinitionSupported, "field_definition_supported"
		if field.Type == "" || field.Type == datatype.FieldTypeUnknown || field.Type == datatype.FieldTypeMixed {
			status, code = plugin.DefinitionUnknown, "target_type_unknown"
		}
		add(field.Name, "target_definition", status, code, physical)
		if binding.Constant != nil {
			encoded, err := postgresWriteValue(binding.Constant.Value, field.Type)
			constantStatus, constantCode := plugin.DefinitionSupported, "constant_encoding_supported"
			if err == nil && encoded != nil {
				// Configuration constants contain JSON values. Do not call custom
				// driver.Valuer implementations from this I/O-free contract.
				kind := reflect.TypeOf(encoded).Kind()
				if _, custom := encoded.(driver.Valuer); custom || kind == reflect.Pointer {
					constantStatus, constantCode = plugin.DefinitionUnknown, "constant_encoding_not_checked"
				} else {
					_, err = driver.DefaultParameterConverter.ConvertValue(encoded)
				}
			}
			if err == nil && encoded != nil && constantStatus == plugin.DefinitionSupported {
				switch field.Type {
				case datatype.FieldTypeJSON, datatype.FieldTypeArray:
					// The shared write encoder checked the declared representation.
				case datatype.FieldTypeString:
					text, ok := encoded.(string)
					if !ok {
						constantStatus, constantCode = plugin.DefinitionUnknown, "constant_value_not_checked"
					} else if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
						err = fmt.Errorf("invalid PostgreSQL text constant")
					}
				case datatype.FieldTypeBool:
					if _, ok := encoded.(bool); !ok {
						constantStatus, constantCode = plugin.DefinitionUnknown, "constant_value_not_checked"
					}
				default:
					// database/sql acceptance alone does not establish SQL casts,
					// numeric bounds, temporal or geometry validity.
					constantStatus, constantCode = plugin.DefinitionUnknown, "constant_value_not_checked"
				}
			}
			if err != nil {
				add(field.Name, "constant_encoding", plugin.DefinitionUnsupported, "constant_encoding_invalid", physical)
			} else if encoded == nil && !field.Nullable {
				add(field.Name, "constant_encoding", plugin.DefinitionUnsupported, "null_constant_not_nullable", physical)
			} else {
				add(field.Name, "constant_encoding", constantStatus, constantCode, physical)
			}
		}
		if binding.Source != nil {
			status, code := postgresDefinitionAssignment(*binding.Source, field)
			add(field.Name, "source_assignment", status, code, physical)
		}
		if definition.Existing != nil {
			if field.Type == "" || field.Type == datatype.FieldTypeUnknown || field.Type == datatype.FieldTypeMixed {
				add(field.Name, "existing_target", plugin.DefinitionUnknown, "existing_column_type_not_proven", physical)
				continue
			}
			existing := definition.Existing.GetField(field.Name)
			if existing == nil {
				status, code := plugin.DefinitionSupported, "nullable_column_addition_supported"
				if field.PrimaryKey || !postgresMissingColumnCanBeAdded(field) {
					status, code = plugin.DefinitionUnsupported, "column_addition_unsupported"
				}
				add(field.Name, "existing_target", status, code, physical)
			} else if strings.TrimSpace(existing.NativeType) == "" {
				add(field.Name, "existing_target", plugin.DefinitionUnknown, "existing_native_type_missing", physical)
			} else if field.Type == datatype.FieldTypeDecimal && strings.Contains(existing.NativeType, "(") && existing.Precision <= 0 {
				add(field.Name, "existing_target", plugin.DefinitionUnknown, "existing_numeric_facts_missing", physical)
			} else {
				column := postgresDefinitionColumn(*existing)
				status, code := plugin.DefinitionSupported, "existing_column_compatible"
				if !postgresColumnCompatibleWithField(column, field, definition.SpatialInfo) {
					status, code = plugin.DefinitionUnsupported, "existing_column_incompatible"
				}
				add(field.Name, "existing_target", status, code, physical)
			}
		}
	}
	return report
}

func postgresDefinitionAssignment(source, target datatype.FieldInfo) (plugin.DefinitionCheckStatus, string) {
	source.Type = datatype.ParseFieldType(string(source.Type))
	if source.Type == "" || source.Type == datatype.FieldTypeUnknown || source.Type == datatype.FieldTypeMixed {
		return plugin.DefinitionUnknown, "source_type_unknown"
	}
	if target.Type == datatype.FieldTypeArray {
		if source.Type != datatype.FieldTypeArray {
			return plugin.DefinitionUnknown, "array_representation_not_proven"
		}
		if source.ElementType == "" || source.ElementType == datatype.FieldTypeUnknown || source.ElementType == datatype.FieldTypeMixed {
			return plugin.DefinitionUnknown, "array_element_type_unknown"
		}
		if source.ElementType != datatype.FieldTypeString {
			return plugin.DefinitionUnsupported, "array_element_type_unsupported"
		}
	}
	if source.Type == target.Type {
		return plugin.DefinitionSupported, "source_assignment_supported"
	}
	if target.Type == datatype.FieldTypeJSON {
		// Native arrays are returned as PostgreSQL literals, not Go containers or
		// JSON documents. A target type annotation does not convert their syntax.
		if source.Type == datatype.FieldTypeArray || source.Type == datatype.FieldTypeBytes || source.Type == datatype.FieldTypeString || datatype.IsSpatialFieldType(source.Type) {
			return plugin.DefinitionUnknown, "json_representation_not_proven"
		}
		return plugin.DefinitionSupported, "json_encoding_supported"
	}
	return plugin.DefinitionUnknown, "value_conversion_not_checked"
}

// Reconstruct only catalog properties consumed by the existing compatibility
// validator. Missing native facts remain unknown in the public report.
func postgresDefinitionColumn(field datatype.FieldInfo) postgresColumnInfo {
	native := strings.ToLower(strings.TrimSpace(field.NativeType))
	udt := native
	if native == "text[]" {
		udt = "_text"
	}
	if strings.HasPrefix(native, "geometry(") {
		udt = "geometry"
	}
	if strings.HasPrefix(native, "geography(") {
		udt = "geography"
	}
	return postgresColumnInfo{
		Name: field.Name, DataType: native, UDTName: udt, NativeType: field.NativeType,
		NumericPrecision: sql.NullInt64{Int64: int64(field.Precision), Valid: field.Precision > 0},
		NumericScale:     sql.NullInt64{Int64: int64(field.Scale), Valid: field.Precision > 0},
	}
}
