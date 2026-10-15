package oracle

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
	oraclemapping "github.com/addp/common/format/mappers/oracle"
)

var _ plugin.TableWriteDefinitionChecker = (*OraclePlugin)(nil)

// CheckTableWriteDefinition consumes supplied facts only. All real write
// preparation paths reuse this report before connecting or executing DDL.
func (p *OraclePlugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	seen := map[string]bool{}
	for _, binding := range definition.Fields {
		field := binding.Target
		field.Name = strings.TrimSpace(field.Name)
		field.Type = datatype.ParseFieldType(string(field.Type))
		if field.Name == "" || len(field.Name) > 128 || strings.ContainsRune(field.Name, 0) || seen[field.Name] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[field.Name] = true
		physical, err := oracleSQLTypeForField(field)
		if err != nil {
			code := "field_definition_unsupported"
			if field.Type == datatype.FieldTypeDecimal {
				code = "decimal_definition_invalid"
			}
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, code, "")
			continue
		}
		if field.Type == datatype.FieldTypeGeometry {
			column := oracleSpatialColumnForField(definition.SpatialInfo, field.Name)
			if column == nil || column.Dimension == nil || *column.Dimension != 2 {
				add(field.Name, "target_definition", plugin.DefinitionUnsupported, "spatial_definition_invalid", physical)
				continue
			}
			if _, _, _, _, _, err := oracleSpatialBounds(definition.SpatialInfo); err != nil {
				add(field.Name, "target_definition", plugin.DefinitionUnsupported, "spatial_definition_invalid", physical)
				continue
			}
		}
		if field.Type == datatype.FieldTypeJSON && field.PrimaryKey {
			add(field.Name, "target_definition", plugin.DefinitionUnknown, "primary_key_type_not_proven", physical)
		} else {
			add(field.Name, "target_definition", plugin.DefinitionSupported, "field_definition_supported", physical)
		}
		if binding.Constant != nil {
			status, code := oracleDefinitionConstant(field, binding.Constant.Value)
			add(field.Name, "constant_encoding", status, code, physical)
		}
		if binding.Source != nil {
			source := datatype.ParseFieldType(string(binding.Source.Type))
			status, code := plugin.DefinitionUnknown, "value_conversion_not_checked"
			switch {
			case source == datatype.FieldTypeUnknown || source == datatype.FieldTypeMixed:
				code = "source_type_unknown"
			case field.Type == datatype.FieldTypeGeometry:
				code = "spatial_value_not_checked"
			case source == field.Type:
				status, code = plugin.DefinitionSupported, "source_assignment_supported"
			case field.Type == datatype.FieldTypeJSON:
				code = "json_representation_not_proven"
				if source == datatype.FieldTypeBool || source == datatype.FieldTypeInt || source == datatype.FieldTypeBigInt || source == datatype.FieldTypeFloat || source == datatype.FieldTypeDouble {
					status, code = plugin.DefinitionSupported, "json_encoding_supported"
				}
			}
			add(field.Name, "source_assignment", status, code, physical)
		}
		if definition.Existing != nil {
			status, code := oracleExistingDefinition(field, definition.Existing)
			add(field.Name, "existing_target", status, code, physical)
		}
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	if definition.SpatialInfo != nil {
		if len(definition.SpatialInfo.GeometryColumns) == 0 && definition.SpatialInfo.HasSpatialIndex != nil && *definition.SpatialInfo.HasSpatialIndex {
			add("", "target_definition", plugin.DefinitionUnsupported, "spatial_definition_invalid", "")
		}
		for _, column := range definition.SpatialInfo.GeometryColumns {
			found := false
			for _, binding := range definition.Fields {
				if strings.EqualFold(strings.TrimSpace(binding.Target.Name), column.Name) && datatype.IsSpatialFieldType(binding.Target.Type) {
					found = true
					break
				}
			}
			if !found {
				add(column.Name, "target_definition", plugin.DefinitionUnsupported, "spatial_definition_invalid", "")
			}
		}
	}
	return report
}

func oracleDefinitionConstant(field datatype.FieldInfo, value interface{}) (plugin.DefinitionCheckStatus, string) {
	if !shared.IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	// Only certify actual JSON / BLOB encodings and SQL NULL. Native casts,
	// numeric ranges and temporal representations still require runtime checks.
	if value == nil {
		if !field.Nullable || field.PrimaryKey {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	if field.Type != datatype.FieldTypeJSON && field.Type != datatype.FieldTypeBytes {
		return plugin.DefinitionUnknown, "constant_value_not_checked"
	}
	encodingField := field
	encodingField.Nullable = true
	encoded, err := oracleScalarWriteValue(encodingField, value)
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	if encoded == nil && (!field.Nullable || field.PrimaryKey) {
		return plugin.DefinitionUnsupported, "null_constant_not_nullable"
	}
	if field.Type == datatype.FieldTypeBytes {
		if bytes, ok := value.([]byte); ok && len(bytes) == 0 {
			// The native LOB driver's nil/empty distinction is not established by
			// pure parameter construction; do not certify an unobserved SQL NULL.
			return plugin.DefinitionUnknown, "constant_value_not_checked"
		}
	}
	return plugin.DefinitionSupported, "constant_encoding_supported"
}

func oracleExistingDefinition(field datatype.FieldInfo, existing *datatype.TableInfo) (plugin.DefinitionCheckStatus, string) {
	observed := existing.GetField(field.Name)
	if observed == nil {
		if !field.Nullable || field.PrimaryKey {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	if observed.Generated || field.Nullable && !field.PrimaryKey && !observed.Nullable || field.PrimaryKey && !observed.PrimaryKey {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	native := strings.ToUpper(strings.TrimSpace(observed.NativeType))
	if native == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	compatible := false
	switch field.Type {
	case datatype.FieldTypeJSON:
		compatible = native == "JSON"
	case datatype.FieldTypeBytes:
		compatible = native == "BLOB"
	case datatype.FieldTypeBool:
		compatible = native == "BOOLEAN"
	case datatype.FieldTypeFloat:
		compatible = native == "BINARY_FLOAT"
	case datatype.FieldTypeDouble:
		compatible = native == "BINARY_DOUBLE"
	case datatype.FieldTypeDate:
		compatible = native == "DATE"
	case datatype.FieldTypeTimestamp:
		if native == "TIMESTAMP" {
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
		compatible = native == "TIMESTAMP(3)"
	case datatype.FieldTypeGeometry:
		if (&oraclemapping.TypeMapper{}).ToCommon(native) == datatype.FieldTypeGeometry {
			return plugin.DefinitionUnknown, "existing_spatial_facts_not_proven"
		}
	case datatype.FieldTypeString, datatype.FieldTypeUUID:
		if (&oraclemapping.TypeMapper{}).ToCommon(native) == datatype.FieldTypeString {
			if field.Type == datatype.FieldTypeUUID && observed.Size > 0 && observed.Size != 36 {
				break
			}
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeDecimal:
		if native == "NUMBER" || strings.HasPrefix(native, "NUMBER(") || strings.HasPrefix(native, "DECIMAL(") || strings.HasPrefix(native, "NUMERIC(") {
			if observed.Precision <= 0 {
				return plugin.DefinitionUnknown, "existing_numeric_facts_missing"
			}
			precision, scale := field.Precision, field.Scale
			if field.Type == datatype.FieldTypeInt {
				precision, scale = 9, 0
			}
			if field.Type == datatype.FieldTypeBigInt {
				precision, scale = 18, 0
			}
			compatible = observed.Precision == precision && observed.Scale == scale
		}
	}
	if !compatible {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	return plugin.DefinitionSupported, "existing_column_compatible"
}
