package preview

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

type documentRecordSetPreviewProvider struct{}

func NewDocumentRecordSetPreviewProvider() PreviewProvider {
	return &documentRecordSetPreviewProvider{}
}
func (*documentRecordSetPreviewProvider) Name() string { return "builtin:document-record-set" }
func (*documentRecordSetPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (result *models.TablePreview, err error) {
	provider, ok := req.EnginePlugin.(plugin.RecordReadSessionProvider)
	if !ok {
		return nil, fmt.Errorf("engine does not implement RecordReadSessionProvider")
	}
	factsProvider, ok := req.EnginePlugin.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return nil, fmt.Errorf("engine does not implement EngineCatalogFactsProvider")
	}
	page := max(1, req.Page)
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	pageSize = min(pageSize, 50)
	// Preview is bounded independently of a query language and native cursor batch size.
	const maxPreviewPage = 1000
	if page > maxPreviewPage {
		return nil, fmt.Errorf("record preview page exceeds limit")
	}
	conn := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	facts, err := factsProvider.DescribeEngineCatalogFacts(ctx, conn, req.ProviderPath, plugin.EngineCatalogFactsOptions{SampleSize: 100})
	if err != nil {
		return nil, err
	}
	table := plugin.EngineCatalogFactsTableInfo(facts)
	if table == nil {
		return nil, fmt.Errorf("record provider returned no field facts")
	}
	rowCount := req.ItemRowCount
	if rowCount == nil || *rowCount < 0 {
		rowCount = table.RowCount
	}
	var total *int
	if rowCount != nil && *rowCount >= 0 {
		total = models.ExactPreviewTotal(int(*rowCount))
	}
	readLimit := pageSize
	if total == nil && page < maxPreviewPage {
		readLimit++
	}
	session, err := provider.OpenRecordReadSession(ctx, conn, req.ProviderPath, plugin.RecordReadSessionOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if closeErr := session.Close(cleanup); err == nil && closeErr != nil {
			result = nil
			err = closeErr
		}
	}()
	skip := (page - 1) * pageSize
	for skip > 0 {
		batch, readErr := session.ReadBatch(ctx, min(skip, 1000))
		if readErr != nil {
			return nil, readErr
		}
		if len(batch.Records) == 0 {
			break
		}
		skip -= len(batch.Records)
	}
	rows := []map[string]interface{}{}
	if skip == 0 {
		for len(rows) < readLimit {
			batch, readErr := session.ReadBatch(ctx, readLimit-len(rows))
			if readErr != nil {
				return nil, readErr
			}
			if len(batch.Records) == 0 {
				break
			}
			rows = append(rows, batch.Records...)
		}
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	keys := map[string]bool{}
	for _, row := range rows {
		for name := range row {
			keys[name] = true
		}
	}
	columns := make([]string, 0, len(keys))
	for name := range keys {
		columns = append(columns, name)
	}
	sort.Strings(columns)
	return &models.TablePreview{
		Mode: PreviewModeTable, PreviewKind: "dynamic_schema_record_set",
		Columns: columns, Fields: table.Fields, ColumnMetadata: buildDynamicSchemaColumnMetadata(table.Fields),
		Rows: rows, Total: total, HasMore: hasMore, Page: page, PageSize: pageSize,
		EngineID: req.Engine.ID, Schema: req.Schema, Table: req.Table, EngineType: req.Engine.EngineType,
	}, nil
}

func buildDynamicSchemaColumnMetadata(fields []datatype.FieldInfo) []models.ColumnMetadata {
	metadata := make([]models.ColumnMetadata, 0, len(fields))
	for _, field := range fields {
		metadata = append(metadata, models.ColumnMetadata{
			ColumnName:   field.Name,
			Path:         append([]string(nil), field.Path...),
			Type:         field.NativeType,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
		})
	}
	return metadata
}
