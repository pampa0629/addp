package shared

import (
	"database/sql"
	"database/sql/driver"
	"reflect"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

var _ plugin.TableWriteDefinitionChecker = MySQLCompatibleTableWriter{}

// CheckTableWriteDefinition uses the same physical rules as preparation. It
// accepts saved facts only and never connects, invokes a Valuer, or reads rows.
func (w MySQLCompatibleTableWriter) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(name, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: name, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	spatial := map[string]bool{}
	if definition.SpatialInfo != nil && definition.SpatialInfo.IsSpatial() {
		for _, column := range definition.SpatialInfo.GeometryColumns {
			spatial[strings.TrimSpace(column.Name)] = true
		}
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
		if spatial[field.Name] {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
			continue
		}
		physical, err := w.sqlTypeForField(field)
		if err != nil {
			code := "field_definition_unsupported"
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
			status, code := mysqlCompatibleDefinitionConstant(binding.Constant.Value, field)
			add(field.Name, "constant_encoding", status, code, physical)
		}
		if binding.Source != nil {
			source := datatype.ParseFieldType(string(binding.Source.Type))
			status, code := plugin.DefinitionUnknown, "value_conversion_not_checked"
			switch {
			case source == "" || source == datatype.FieldTypeUnknown || source == datatype.FieldTypeMixed:
				code = "source_type_unknown"
			case source == field.Type:
				status, code = plugin.DefinitionSupported, "source_assignment_supported"
			case field.Type == datatype.FieldTypeJSON:
				if source == datatype.FieldTypeArray || source == datatype.FieldTypeBytes || source == datatype.FieldTypeString || datatype.IsSpatialFieldType(source) {
					code = "json_representation_not_proven"
				} else {
					status, code = plugin.DefinitionSupported, "json_encoding_supported"
				}
			}
			add(field.Name, "source_assignment", status, code, physical)
		}
		if definition.Existing != nil {
			status, code := w.mysqlCompatibleExistingDefinition(field, definition.Existing)
			add(field.Name, "existing_target", status, code, physical)
		}
	}
	if definition.SpatialInfo != nil {
		for _, column := range definition.SpatialInfo.GeometryColumns {
			name := strings.TrimSpace(column.Name)
			if !seen[name] {
				add(name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
				seen[name] = true
			}
		}
	}
	return report
}

func mysqlCompatibleDefinitionConstant(value interface{}, field datatype.FieldInfo) (plugin.DefinitionCheckStatus, string) {
	if !IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	encoded, err := EncodeTableWriteValue(value, field.Type)
	if err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	if encoded != nil {
		// Custom encoders may perform I/O. The pure check must not invoke them.
		if _, custom := encoded.(driver.Valuer); custom || reflect.TypeOf(encoded).Kind() == reflect.Pointer {
			return plugin.DefinitionUnknown, "constant_encoding_not_checked"
		}
		converted, err := driver.DefaultParameterConverter.ConvertValue(encoded)
		if err != nil {
			return plugin.DefinitionUnsupported, "constant_encoding_invalid"
		}
		if bytes, ok := converted.([]byte); ok && bytes == nil {
			encoded = nil
		}
	}
	if encoded == nil {
		if !field.Nullable {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	switch field.Type {
	case datatype.FieldTypeJSON:
		return plugin.DefinitionSupported, "constant_encoding_supported"
	case datatype.FieldTypeString:
		if _, ok := encoded.(string); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	case datatype.FieldTypeBool:
		if _, ok := encoded.(bool); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	case datatype.FieldTypeBytes:
		if _, ok := encoded.([]byte); ok {
			return plugin.DefinitionSupported, "constant_encoding_supported"
		}
	}
	// Driver acceptance does not prove SQL casts, numeric bounds or dates.
	return plugin.DefinitionUnknown, "constant_value_not_checked"
}

func (w MySQLCompatibleTableWriter) mysqlCompatibleExistingDefinition(field datatype.FieldInfo, existing *datatype.TableInfo) (plugin.DefinitionCheckStatus, string) {
	if field.Type == "" || field.Type == datatype.FieldTypeUnknown || field.Type == datatype.FieldTypeMixed {
		return plugin.DefinitionUnknown, "existing_column_type_not_proven"
	}
	observed := existing.GetField(field.Name)
	if observed == nil {
		if field.PrimaryKey || (!field.Nullable && strings.TrimSpace(field.DefaultExpression) == "") {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	native := strings.ToLower(strings.TrimSpace(observed.NativeType))
	if native == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	column := mysqlCompatibleColumnInfo{Name: observed.Name, NativeType: native, DataType: strings.SplitN(native, "(", 2)[0], NumericPrecision: sql.NullInt64{Int64: int64(observed.Precision), Valid: observed.Precision > 0}, NumericScale: sql.NullInt64{Int64: int64(observed.Scale), Valid: observed.Precision > 0}}
	// Native categories can disprove compatibility without auxiliary precision facts.
	if field.Type != datatype.FieldTypeUUID && mysqlCompatibleCommonFieldType(column) != field.Type {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	if field.Type == datatype.FieldTypeDecimal && observed.Precision <= 0 {
		return plugin.DefinitionUnknown, "existing_numeric_facts_missing"
	}
	if field.Type == datatype.FieldTypeTimestamp || field.Type == datatype.FieldTypeTime {
		open := strings.IndexByte(native, '(')
		if open < 0 || !strings.HasSuffix(native, ")") {
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
		precision, err := strconv.Atoi(native[open+1 : len(native)-1])
		if err != nil || precision < 0 || precision > 6 {
			return plugin.DefinitionUnknown, "existing_column_type_not_proven"
		}
		column.TemporalPrecision = sql.NullInt64{Int64: int64(precision), Valid: true}
	}
	if !mysqlCompatibleColumnMatchesField(column, field) {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	return plugin.DefinitionSupported, "existing_column_compatible"
}
