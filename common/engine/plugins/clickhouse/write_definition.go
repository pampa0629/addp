package clickhouse

import (
	"context"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

var _ plugin.TableWriteDefinitionChecker = (*ClickHousePlugin)(nil)

// CheckTableWriteDefinition consumes saved facts only. The same field and
// column compatibility rules are used by the real table preparation path.
func (p *ClickHousePlugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	seen := map[string]bool{}
	for _, binding := range definition.Fields {
		field := binding.Target
		if field.Generated {
			continue
		}
		field.Name = strings.TrimSpace(field.Name)
		field.Type = datatype.ParseFieldType(string(field.Type))
		key := field.Name
		if field.Name == "" || seen[key] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[key] = true
		check := clickhouseFieldDefinitionCheck(field)
		report.Checks = append(report.Checks, check)
		if check.Status == plugin.DefinitionUnsupported {
			continue
		}
		if binding.Constant != nil {
			status, code := clickhouseDefinitionConstant(field, binding.Constant.Value)
			add(field.Name, "constant_encoding", status, code, check.PhysicalType)
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
				if source != datatype.FieldTypeBool && source != datatype.FieldTypeInt && source != datatype.FieldTypeBigInt && source != datatype.FieldTypeFloat && source != datatype.FieldTypeDouble {
					code = "json_representation_not_proven"
				} else {
					status, code = plugin.DefinitionSupported, "json_encoding_supported"
				}
			}
			add(field.Name, "source_assignment", status, code, check.PhysicalType)
		}
		if definition.Existing != nil {
			status, code := clickhouseExistingDefinition(field, definition.Existing)
			add(field.Name, "existing_target", status, code, check.PhysicalType)
		}
	}
	if len(report.Checks) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	if definition.SpatialInfo != nil {
		for _, column := range definition.SpatialInfo.GeometryColumns {
			add(strings.TrimSpace(column.Name), "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
		}
		if len(definition.SpatialInfo.GeometryColumns) == 0 && definition.SpatialInfo.HasSpatialIndex != nil && *definition.SpatialInfo.HasSpatialIndex {
			add("", "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
		}
	}
	return report
}

func clickhouseFieldDefinitionCheck(field datatype.FieldInfo) plugin.TableWriteDefinitionCheck {
	check := plugin.TableWriteDefinitionCheck{Field: field.Name, Check: "target_definition", Status: plugin.DefinitionSupported, Code: "field_definition_supported"}
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeArray, datatype.FieldTypeGeometry:
		check.Status, check.Code = plugin.DefinitionUnsupported, "field_definition_unsupported"
		return check
	case datatype.FieldTypeDecimal:
		if err := plugin.ValidateExplicitDecimalFieldDefinition("clickhouse", field, clickhouseDecimalMaxPrecision, clickhouseDecimalMaxScale); err != nil {
			check.Status, check.Code = plugin.DefinitionUnsupported, "decimal_definition_invalid"
			return check
		}
	case datatype.FieldTypeUnknown, datatype.FieldTypeMixed:
		check.Status, check.Code = plugin.DefinitionUnknown, "target_type_unknown"
	}
	check.PhysicalType = clickhouseSQLTypeForField(field)
	return check
}

func clickhouseDefinitionConstant(field datatype.FieldInfo, value interface{}) (plugin.DefinitionCheckStatus, string) {
	if !shared.IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	// Reuse real JSON and binary String argument encoding after excluding callbacks.
	args, err := clickhouseTableWriteArgs(context.Background(), []string{field.Name}, map[string]datatype.FieldType{field.Name: field.Type}, []map[string]interface{}{{field.Name: value}})
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	encoded := args[0]
	// Exec uses the native driver's formatter, not database/sql's default
	// converter or column.AppendRow. Certify only explicit encodings below;
	// numeric and container-to-String casts remain unknown.
	if encoded == nil {
		if !field.Nullable {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	switch field.Type {
	case datatype.FieldTypeJSON:
		return plugin.DefinitionSupported, "constant_encoding_supported"
	case datatype.FieldTypeString, datatype.FieldTypeBytes:
		if _, ok := encoded.(string); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	case datatype.FieldTypeBool:
		if _, ok := encoded.(bool); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	}
	return plugin.DefinitionUnknown, "constant_value_not_checked"
}

func clickhouseExistingDefinition(field datatype.FieldInfo, existing *datatype.TableInfo) (plugin.DefinitionCheckStatus, string) {
	if field.Type == datatype.FieldTypeUnknown || field.Type == datatype.FieldTypeMixed {
		return plugin.DefinitionUnknown, "existing_column_type_not_proven"
	}
	observed := existing.GetField(field.Name)
	if observed == nil {
		if field.PrimaryKey || !clickhouseMissingColumnCanBeAdded(field) {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	if strings.TrimSpace(observed.NativeType) == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	column := clickhouseWriteColumnInfo{Name: observed.Name, NativeType: observed.NativeType}
	if field.Type == datatype.FieldTypeDecimal && clickhouseCommonFieldType(column.NativeType) == datatype.FieldTypeDecimal {
		if _, _, ok := clickhouseDecimalPrecisionScale(column.NativeType); !ok {
			return plugin.DefinitionUnknown, "existing_numeric_facts_missing"
		}
	}
	if !clickhouseColumnCompatibleWithField(column, field) {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	return plugin.DefinitionSupported, "existing_column_compatible"
}
