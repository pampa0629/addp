package mysql

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared"
)

var _ plugin.TableWriteDefinitionChecker = (*MySQLPlugin)(nil)

// CheckTableWriteDefinition composes the existing nonspatial kernel with
// MySQL's own geometry rules. It only consumes the supplied saved facts.
func (p *MySQLPlugin) CheckTableWriteDefinition(definition plugin.TableWriteDefinition) plugin.TableWriteDefinitionReport {
	report := plugin.TableWriteDefinitionReport{Checks: []plugin.TableWriteDefinitionCheck{}}
	add := func(field, check string, status plugin.DefinitionCheckStatus, code, physical string) {
		report.Checks = append(report.Checks, plugin.TableWriteDefinitionCheck{Field: field, Check: check, Status: status, Code: code, PhysicalType: physical})
	}
	if len(definition.Fields) == 0 {
		add("", "target_definition", plugin.DefinitionUnsupported, "fields_required", "")
	}
	seen := map[string]datatype.FieldType{}
	for _, binding := range definition.Fields {
		field := binding.Target
		field.Name = strings.TrimSpace(field.Name)
		field.Type = datatype.ParseFieldType(string(field.Type))
		key := strings.ToLower(field.Name)
		if _, exists := seen[key]; field.Name == "" || exists {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_name_invalid", "")
			continue
		}
		seen[key] = field.Type
		if !datatype.IsSpatialFieldType(field.Type) {
			binding.Target = field
			checks := p.nonSpatialTableWriter().CheckTableWriteDefinition(plugin.TableWriteDefinition{Fields: []plugin.TableWriteFieldDefinition{binding}, Existing: definition.Existing})
			report.Checks = append(report.Checks, checks.Checks...)
			continue
		}
		physical, err := mysqlSQLTypeForField(field, definition.SpatialInfo)
		if err != nil {
			add(field.Name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
			continue
		}
		add(field.Name, "target_definition", plugin.DefinitionSupported, "field_definition_supported", physical)
		if binding.Constant != nil {
			status, code := mysqlSpatialDefinitionConstant(field, definition.SpatialInfo, binding.Constant.Value)
			add(field.Name, "constant_encoding", status, code, physical)
		}
		if binding.Source != nil {
			status, code := plugin.DefinitionUnknown, "value_conversion_not_checked"
			source := datatype.ParseFieldType(string(binding.Source.Type))
			if source == datatype.FieldTypeGeometry {
				status, code = plugin.DefinitionSupported, "source_assignment_supported"
			} else if source == datatype.FieldTypeUnknown || source == datatype.FieldTypeMixed {
				code = "source_type_unknown"
			}
			add(field.Name, "source_assignment", status, code, physical)
		}
		if definition.Existing != nil {
			status, code := mysqlSpatialExistingDefinition(field, definition)
			add(field.Name, "existing_target", status, code, physical)
		}
	}
	if definition.SpatialInfo != nil {
		declared := map[string]bool{}
		for _, column := range definition.SpatialInfo.GeometryColumns {
			name := strings.TrimSpace(column.Name)
			key := strings.ToLower(name)
			if name == "" || declared[key] || seen[key] != datatype.FieldTypeGeometry {
				add(name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
			}
			declared[key] = true
		}
		if _, err := mysqlSpatialIndexField(tableWriteDefinitionFields(definition), definition.SpatialInfo); err != nil {
			name := ""
			if primary := definition.SpatialInfo.PrimaryGeometry(); primary != nil {
				name = primary.Name
			}
			add(name, "target_definition", plugin.DefinitionUnsupported, "field_definition_unsupported", "")
		}
	}
	return report
}

func tableWriteDefinitionFields(definition plugin.TableWriteDefinition) []datatype.FieldInfo {
	fields := make([]datatype.FieldInfo, 0, len(definition.Fields))
	for _, binding := range definition.Fields {
		fields = append(fields, binding.Target)
	}
	return fields
}

func mysqlSpatialDefinitionConstant(field datatype.FieldInfo, spatial *datatype.SpatialInfo, value interface{}) (plugin.DefinitionCheckStatus, string) {
	if !shared.IsPureTableWriteConstant(value) {
		return plugin.DefinitionUnknown, "constant_encoding_not_checked"
	}
	if value == nil {
		if !field.Nullable {
			return plugin.DefinitionUnsupported, "null_constant_not_nullable"
		}
		return plugin.DefinitionSupported, "constant_encoding_supported"
	}
	srid := 0
	if column := mysqlSpatialColumnForField(spatial, field.Name); column != nil && column.SRID != nil {
		srid = *column.SRID
	}
	if _, _, err := mysqlGeometryWriteValue(value, srid); err != nil {
		return plugin.DefinitionUnsupported, "constant_encoding_invalid"
	}
	// Encoding does not establish instance SRS availability or geometry validity.
	return plugin.DefinitionUnknown, "constant_value_not_checked"
}

func mysqlSpatialExistingDefinition(field datatype.FieldInfo, definition plugin.TableWriteDefinition) (plugin.DefinitionCheckStatus, string) {
	observed := definition.Existing.GetField(field.Name)
	if observed == nil {
		if field.PrimaryKey || !mysqlMissingColumnCanBeAdded(field) {
			return plugin.DefinitionUnsupported, "column_addition_unsupported"
		}
		return plugin.DefinitionSupported, "nullable_column_addition_supported"
	}
	if strings.TrimSpace(observed.NativeType) == "" {
		return plugin.DefinitionUnknown, "existing_native_type_missing"
	}
	if !mysqlSpatialNativeTypeMatches(observed.NativeType, field, definition.SpatialInfo) {
		return plugin.DefinitionUnsupported, "existing_column_incompatible"
	}
	// TableInfo contains no independent column SRS_ID. The target declaration
	// cannot be used as evidence of the existing column's SRID restriction.
	return plugin.DefinitionUnknown, "existing_column_type_not_proven"
}
