package doris

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

var _ plugin.TableWriteDefinitionChecker = (*DorisPlugin)(nil)

// CheckTableWriteDefinition consumes saved facts only. The same field, key and
// column compatibility rules are used by the real table preparation path.
func (p *DorisPlugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	fields := make([]datatype.FieldInfo, 0, len(definition.Fields))
	seen := map[string]bool{}
	for _, binding := range definition.Fields {
		field := binding.Target
		field.Name = strings.TrimSpace(field.Name)
		field.Type = datatype.ParseFieldType(string(field.Type))
		key := strings.ToLower(field.Name)
		if field.Name == "" || seen[key] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[key] = true
		check := dorisFieldDefinitionCheck(field)
		report.Checks = append(report.Checks, check)
		if check.Status == plugin.DefinitionUnsupported {
			continue
		}
		fields = append(fields, field)
		if binding.Constant != nil {
			status, code := dorisDefinitionConstant(field, binding.Constant.Value)
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
				if source == datatype.FieldTypeArray || source == datatype.FieldTypeString || source == datatype.FieldTypeBytes || datatype.IsSpatialFieldType(source) {
					code = "json_representation_not_proven"
				} else {
					status, code = plugin.DefinitionSupported, "json_encoding_supported"
				}
			}
			add(field.Name, "source_assignment", status, code, check.PhysicalType)
		}
		if definition.Existing != nil {
			status, code := dorisExistingDefinition(field, definition.Existing)
			add(field.Name, "existing_target", status, code, check.PhysicalType)
		}
	}
	if len(fields) > 0 {
		if _, err := dorisDuplicateKeyField(fields); err != nil {
			add("", "target_definition", plugin.DefinitionUnsupported, "key_definition_unsupported", "")
		}
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

func dorisFieldDefinitionCheck(field datatype.FieldInfo) plugin.TableWriteDefinitionCheck {
	check := plugin.TableWriteDefinitionCheck{Field: field.Name, Check: "target_definition", Status: plugin.DefinitionSupported, Code: "field_definition_supported"}
	switch datatype.ParseFieldType(string(field.Type)) {
	case datatype.FieldTypeArray, datatype.FieldTypeGeometry:
		check.Status, check.Code = plugin.DefinitionUnsupported, "field_definition_unsupported"
		return check
	case datatype.FieldTypeDecimal:
		if err := plugin.ValidateExplicitDecimalFieldDefinition("doris", field, dorisDecimalMaxPrecision, dorisDecimalMaxScale); err != nil {
			check.Status, check.Code = plugin.DefinitionUnsupported, "decimal_definition_invalid"
			return check
		}
	case datatype.FieldTypeUnknown, datatype.FieldTypeMixed:
		check.Status, check.Code = plugin.DefinitionUnknown, "target_type_unknown"
	}
	check.PhysicalType = dorisSQLTypeForField(field)
	return check
}

func dorisDefinitionConstant(field datatype.FieldInfo, value interface{}) (plugin.DefinitionCheckStatus, string) {
	if !shared.IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	// Reuse real JSON and UNHEX argument encoding after excluding callbacks.
	args, err := dorisTableWriteArgs(context.Background(), []string{field.Name}, map[string]datatype.FieldType{field.Name: field.Type}, []map[string]interface{}{{field.Name: value}})
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	encoded, err := driver.DefaultParameterConverter.ConvertValue(args[0])
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	if bytes, ok := encoded.([]byte); ok && bytes == nil {
		encoded = nil
	}
	if encoded == nil {
		if !field.Nullable {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	switch field.Type {
	case datatype.FieldTypeJSON, datatype.FieldTypeBytes:
		return plugin.DefinitionSupported, "constant_encoding_supported"
	case datatype.FieldTypeString:
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

func dorisExistingDefinition(field datatype.FieldInfo, existing *datatype.TableInfo) (plugin.DefinitionCheckStatus, string) {
	if field.Type == datatype.FieldTypeUnknown || field.Type == datatype.FieldTypeMixed {
		return plugin.DefinitionUnknown, "existing_column_type_not_proven"
	}
	observed := existing.GetField(field.Name)
	if observed == nil {
		if field.PrimaryKey || !dorisMissingColumnCanBeAdded(field) {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	if strings.TrimSpace(observed.NativeType) == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	column := dorisColumnInfo{Name: observed.Name, NativeType: observed.NativeType, NumericPrecision: sql.NullInt64{Int64: int64(observed.Precision), Valid: observed.Precision > 0}, NumericScale: sql.NullInt64{Int64: int64(observed.Scale), Valid: observed.Precision > 0}}
	if field.Type == datatype.FieldTypeDecimal && dorisCommonFieldType(column) == datatype.FieldTypeDecimal && observed.Precision <= 0 {
		return plugin.DefinitionUnknown, "existing_numeric_facts_missing"
	}
	if !dorisColumnCompatibleWithField(column, field) {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	return plugin.DefinitionSupported, "existing_column_compatible"
}
