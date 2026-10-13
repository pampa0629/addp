package api

import (
	"net/http"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/logger"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/gin-gonic/gin"
)

// writePreviewResult applies protection before serializing the public preview.
func writePreviewResult(c *gin.Context, result *preview.PreviewResult, rules []dataprotection.Rule, subject dataprotection.SubjectReference, projections []dataprotection.Projection) {
	// Native MongoDB and other non-query previews also retain their original
	// projection contract through the final response boundary.
	now := time.Now().UTC()
	for _, projection := range projections {
		if err := projection.Validate(now); err != nil {
			protectionRequired(c)
			return
		}
	}
	if err := applyPreviewProtection(result, rules, subject); err != nil {
		protectionRequired(c)
		return
	}
	wireResult, err := previewResultForJSON(result)
	if err != nil {
		logger.L().Error("数据预览序列化失败", "error_type", "bigint_representation")
		managerError(c, http.StatusInternalServerError, manageri18n.MsgPreviewFailed)
		return
	}
	logger.L().Info("数据预览成功", "locator", c.Query("locator"), "preview_type", result.PreviewType)
	c.JSON(http.StatusOK, wireResult)
}

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
