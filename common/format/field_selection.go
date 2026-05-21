package format

import "fmt"

func ApplyFieldSelectionToTableInfo(info *TableInfo, selection *FieldSelectionOptions) (*TableInfo, error) {
	if info == nil || selection == nil || len(selection.Include) == 0 {
		return info, nil
	}
	fieldByName := make(map[string]FieldInfo, len(info.Fields))
	for _, field := range info.Fields {
		fieldByName[field.Name] = field
	}
	fields := make([]FieldInfo, 0, len(selection.Include))
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
	copied := *info
	copied.Fields = fields
	copied.PrimaryKey = selectedPrimaryKeys(info.PrimaryKey, seen)
	if copied.SpatialInfo != nil && !seen[copied.SpatialInfo.GeometryColumn] {
		copied.SpatialInfo = nil
	}
	return &copied, nil
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
