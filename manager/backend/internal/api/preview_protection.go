package api

import (
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
)

// applyPreviewProtection is the Manager response-boundary executor. Providers
// continue returning native decoded rows; protected rows are transformed only
// immediately before serialization.
func applyPreviewProtection(result *preview.PreviewResult, rules []dataprotection.Rule, subject dataprotection.SubjectReference) error {
	if len(rules) == 0 {
		return nil
	}
	if result == nil {
		return managerprotection.ErrRequired
	}
	table, ok := result.Data.(*models.TablePreview)
	if !ok || table == nil || table.KeyValue != nil {
		return managerprotection.ErrRequired
	}
	for _, row := range table.Rows {
		if row == nil {
			return managerprotection.ErrRequired
		}
	}
	removedColumns := make(map[string]struct{}, len(table.Columns))
	for _, column := range table.Columns {
		removedColumns[column] = struct{}{}
	}
	queryResult := &plugin.QueryResult{Columns: table.Columns, Rows: table.Rows}
	if err := dataprotection.ProtectQueryResultSource(queryResult, plugin.QueryOutputSource{IdentityOutput: true}, managerprotection.ActionPreview, rules, subject); err != nil {
		return managerprotection.ErrRequired
	}
	table.Columns = queryResult.Columns
	for _, column := range table.Columns {
		delete(removedColumns, column)
	}
	metadata := table.ColumnMetadata[:0]
	for _, column := range table.ColumnMetadata {
		if _, removed := removedColumns[column.ColumnName]; !removed {
			metadata = append(metadata, column)
		}
	}
	table.ColumnMetadata = metadata
	return nil
}
