package dameng

import (
	"context"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

var _ plugin.TableWriteDefinitionChecker = (*Plugin)(nil)

// CheckTableWriteDefinition only consumes supplied facts. Prepare uses this
// same report before any DDL, including when evolving an existing table.
func (p *Plugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	seen := map[string]bool{}
	for _, binding := range definition.Fields {
		field := binding.Target
		name := strings.TrimSpace(field.Name)
		if name == "" || name != field.Name || seen[strings.ToUpper(name)] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[strings.ToUpper(name)] = true
		physical, err := dmSQLType(field)
		if err != nil {
			code := "field_definition_unsupported"
			if field.Type == datatype.FieldTypeDecimal {
				code = "decimal_definition_invalid"
			}
			add(name, "target_definition", plugin.DefinitionUnsupported, code, "")
			continue
		}
		add(name, "target_definition", plugin.DefinitionSupported, "field_definition_supported", physical)
		if binding.Constant != nil {
			status, code := damengDefinitionConstant(field, binding.Constant.Value)
			add(name, "constant_encoding", status, code, physical)
		}
		if binding.Source != nil {
			source := datatype.ParseFieldType(string(binding.Source.Type))
			status, code := plugin.DefinitionUnknown, "value_conversion_not_checked"
			switch {
			case source == datatype.FieldTypeUnknown || source == datatype.FieldTypeMixed:
				code = "source_type_unknown"
			case source == field.Type:
				status, code = plugin.DefinitionSupported, "source_assignment_supported"
			case field.Type == datatype.FieldTypeJSON:
				code = "json_representation_not_proven"
				if source == datatype.FieldTypeBool || source == datatype.FieldTypeInt || source == datatype.FieldTypeBigInt || source == datatype.FieldTypeFloat || source == datatype.FieldTypeDouble {
					status, code = plugin.DefinitionSupported, "json_encoding_supported"
				}
			}
			add(name, "source_assignment", status, code, physical)
		}
		if definition.Existing != nil {
			status, code := damengExistingDefinition(field, definition.Existing)
			add(name, "existing_target", status, code, physical)
		}
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	if spatial := definition.SpatialInfo; spatial != nil {
		for _, column := range spatial.GeometryColumns {
			add(column.Name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
		}
		if len(spatial.GeometryColumns) == 0 && spatial.HasSpatialIndex != nil && *spatial.HasSpatialIndex {
			add("", "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
		}
	}
	return report
}

func damengDefinitionConstant(field datatype.FieldInfo, value interface{}) (plugin.DefinitionCheckStatus, string) {
	if !shared.IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	// Encode through the real batch kernel; inspect SQL NULL separately so the
	// report can locate the non-nullability error without executing callbacks.
	encodingField := field
	encodingField.Nullable = true
	encodingField.PrimaryKey = false
	rows, err := damengWriteRows(context.Background(), []datatype.FieldInfo{encodingField}, []map[string]interface{}{{field.Name: value}})
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	encoded := rows[0][0]
	if encoded == nil {
		if !field.Nullable || field.PrimaryKey {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	if field.Type == datatype.FieldTypeJSON {
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	if field.Type == datatype.FieldTypeBytes {
		if _, ok := encoded.([]byte); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	}
	// Other native casts, ranges, string capacities and temporal representations
	// remain runtime checks; database/sql conversion alone cannot prove them.
	return plugin.DefinitionUnknown, "constant_value_not_checked"
}

func damengExistingDefinition(field datatype.FieldInfo, existing *datatype.TableInfo) (plugin.DefinitionCheckStatus, string) {
	observed := existing.GetField(field.Name)
	if observed == nil {
		// SQL identifiers are quoted. A case-folded name is not proof that the
		// exact column referenced by INSERT exists.
		for _, candidate := range existing.Fields {
			if strings.EqualFold(candidate.Name, field.Name) {
				return plugin.DefinitionUnsupported, "existing_column_incompatible"
			}
		}
		if !field.Nullable || field.PrimaryKey {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	native := strings.ToUpper(strings.TrimSpace(observed.NativeType))
	if native == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	if field.Nullable && !field.PrimaryKey && !observed.Nullable || field.PrimaryKey && !observed.PrimaryKey {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	compatible := false
	switch field.Type {
	case datatype.FieldTypeJSON:
		compatible = native == "CLOB"
	case datatype.FieldTypeBytes:
		compatible = native == "BLOB"
	case datatype.FieldTypeInt:
		compatible = native == "INTEGER" || native == "INT"
	case datatype.FieldTypeBigInt:
		compatible = native == "BIGINT"
	case datatype.FieldTypeBool:
		compatible = native == "BOOLEAN" || native == "BIT"
	case datatype.FieldTypeFloat:
		compatible = native == "REAL"
	case datatype.FieldTypeDouble:
		compatible = native == "DOUBLE" || native == "DOUBLE PRECISION"
	case datatype.FieldTypeDecimal:
		if commonFieldType(native) == datatype.FieldTypeDecimal {
			if observed.Precision <= 0 {
				return plugin.DefinitionUnknown, "existing_numeric_facts_missing"
			}
			compatible = observed.Precision == field.Precision && observed.Scale == field.Scale
		}
	case datatype.FieldTypeString, datatype.FieldTypeUUID:
		if commonFieldType(native) == datatype.FieldTypeString {
			// USER_TAB_COLUMNS DATA_LENGTH is a byte count, not a complete
			// statement of character capacity or length semantics.
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
	case datatype.FieldTypeDate:
		compatible = native == "DATE"
	case datatype.FieldTypeTime, datatype.FieldTypeTimestamp:
		if commonFieldType(native) == field.Type {
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
	}
	if !compatible {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	return plugin.DefinitionSupported, "existing_column_compatible"
}
