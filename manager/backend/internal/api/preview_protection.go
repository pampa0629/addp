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
	var table *models.TablePreview
	if result != nil {
		table, _ = result.Data.(*models.TablePreview)
	}
	if len(rules) == 0 && (table == nil || table.PreparedProtection == nil) {
		return nil
	}
	if result == nil {
		return managerprotection.ErrRequired
	}
	if table == nil || table.KeyValue != nil || table.Keyspace != nil {
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
	var err error
	if table.PreparedProtection != nil {
		if table.PreparedProtection.Apply == nil {
			return managerprotection.ErrRequired
		}
		err = table.PreparedProtection.Apply(queryResult)
	} else {
		err = dataprotection.ProtectQueryResultSource(queryResult, plugin.QueryOutputSource{IdentityOutput: true}, managerprotection.ActionPreview, rules, subject)
	}
	if err != nil {
		return managerprotection.ErrRequired
	}
	table.Columns = queryResult.Columns
	table.Rows = queryResult.Rows
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
	fields := table.Fields[:0]
	for _, field := range table.Fields {
		if _, removed := removedColumns[field.Name]; !removed {
			fields = append(fields, field)
		}
	}
	table.Fields = fields
	geometry := table.GeometryColumns[:0]
	for _, column := range table.GeometryColumns {
		if _, removed := removedColumns[column]; !removed {
			geometry = append(geometry, column)
		}
	}
	table.GeometryColumns = geometry
	if _, removed := removedColumns[table.GeometryColumn]; removed {
		table.GeometryColumn = ""
		table.SourceSRID, table.SRID = 0, 0
		table.SourceCRS, table.TransformStatus, table.TransformEngine, table.PreviewHint = "", "", "", ""
		table.SourceCRSDefinition, table.Extent = nil, nil
	}
	return nil
}
