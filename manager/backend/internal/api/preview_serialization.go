package api

import (
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
	wireTable := *table
	var err error
	wireTable.Rows, err = datatype.RowsForJSON(table.Fields, table.Rows)
	if err != nil {
		return nil, err
	}
	wireResult := *result
	wireResult.Data = &wireTable
	return &wireResult, nil
}
