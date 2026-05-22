package format

import (
	"fmt"

	"github.com/addp/common/datatype"
)

func ApplyFieldSelectionToTableInfo(info *TableInfo, selection *FieldSelectionOptions) (*TableInfo, error) {
	if info == nil || selection == nil || len(selection.Include) == 0 {
		return info, nil
	}
	copied := info.Clone()
	fieldByName := make(map[string]datatype.FieldInfo, len(info.Fields))
	for _, field := range info.Fields {
		fieldByName[field.Name] = field
	}
	fields := make([]datatype.FieldInfo, 0, len(selection.Include))
	seen := map[string]bool{}
	for _, name := range selection.Include {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		field, ok := fieldByName[name]
		if !ok {
			if selection.EffectiveMissingFieldPolicy() == MissingFieldIgnore {
				continue
			}
			return nil, fmt.Errorf("field selection references missing field %q", name)
		}
		fields = append(fields, field)
	}
	copied.Fields = fields
	copied.PrimaryKey = selectedPrimaryKeys(copied.PrimaryKey, seen)
	if copied.SpatialInfo != nil {
		geometry := copied.SpatialInfo.PrimaryGeometry()
		if geometry == nil || !seen[geometry.Name] {
			copied.SpatialInfo = nil
		}
	}
	return copied, nil
}

func ApplyFieldSelectionToTableDescribeResult(result *datatype.TableDescribeResult, selection *FieldSelectionOptions) (*datatype.TableDescribeResult, error) {
	if result == nil || selection == nil || len(selection.Include) == 0 {
		return result, nil
	}
	if result.Table == nil {
		return result, nil
	}
	copied := &datatype.TableDescribeResult{
		Table:        result.Table.Clone(),
		Spatial:      result.Spatial.Clone(),
		ContentIndex: result.ContentIndex.Clone(),
		FormatInfo:   cloneInterfaceMap(result.FormatInfo),
	}
	fieldByName := make(map[string]datatype.FieldInfo, len(result.Table.Fields))
	for _, field := range result.Table.Fields {
		fieldByName[field.Name] = field
	}
	fields := make([]datatype.FieldInfo, 0, len(selection.Include))
	seen := map[string]bool{}
	for _, name := range selection.Include {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		field, ok := fieldByName[name]
		if !ok {
			if selection.EffectiveMissingFieldPolicy() == MissingFieldIgnore {
				continue
			}
			return nil, fmt.Errorf("field selection references missing field %q", name)
		}
		fields = append(fields, field)
	}
	copied.Table.Fields = fields
	copied.Table.PrimaryKey = selectedPrimaryKeys(copied.Table.PrimaryKey, seen)
	if copied.Spatial != nil {
		geometry := copied.Spatial.PrimaryGeometry()
		if geometry == nil || !seen[geometry.Name] {
			copied.Spatial = nil
		}
	}
	return copied, nil
}

func ApplyFieldSelectionToRows(rows []map[string]interface{}, selection *FieldSelectionOptions) []map[string]interface{} {
	if selection == nil || len(selection.Include) == 0 || len(rows) == 0 {
		return rows
	}
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		selected := make(map[string]interface{}, len(selection.Include))
		for _, name := range selection.Include {
			if name == "" {
				continue
			}
			if value, ok := row[name]; ok {
				selected[name] = value
			}
		}
		result = append(result, selected)
	}
	return result
}

func selectedPrimaryKeys(primaryKeys []string, selected map[string]bool) []string {
	if len(primaryKeys) == 0 || len(selected) == 0 {
		return nil
	}
	result := make([]string, 0, len(primaryKeys))
	for _, key := range primaryKeys {
		if selected[key] {
			result = append(result, key)
		}
	}
	return result
}
