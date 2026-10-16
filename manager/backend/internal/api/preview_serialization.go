package api

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
)

// previewResultForJSON changes only the public representation after protection.
// Native rows remain available to internal readers without a string conversion.
func previewResultForJSON(result *preview.PreviewResult) (*preview.PreviewResult, error) {
	if result == nil {
		return nil, nil
	}
	table, ok := result.Data.(*models.TablePreview)
	if !ok || table == nil || table.Mode != preview.PreviewModeTable {
		return result, nil
	}
	bigints := make(map[string]bool)
	for _, field := range table.Fields {
		if field.Type == datatype.FieldTypeBigInt {
			bigints[field.Name] = true
		}
	}
	wireTable := *table
	wireTable.Rows = make([]map[string]interface{}, len(table.Rows))
	for i, row := range table.Rows {
		if row == nil {
			continue
		}
		wireRow := make(map[string]interface{}, len(row))
		for name, value := range row {
			if bigints[name] && value != nil {
				converted, err := preview.BigintJSONText(value)
				if err != nil {
					return nil, fmt.Errorf("serialize bigint field %q: %w", name, err)
				}
				value = converted
			}
			wireRow[name] = value
		}
		wireTable.Rows[i] = wireRow
	}
	wireResult := *result
	wireResult.Data = &wireTable
	return &wireResult, nil
}
