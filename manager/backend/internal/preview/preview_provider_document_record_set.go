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
	if page > 1000 {
		return nil, fmt.Errorf("record preview page exceeds limit")
	}
	conn := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	facts, err := factsProvider.DescribeEngineCatalogFacts(ctx, conn, req.ProviderPath, plugin.EngineCatalogFactsOptions{SampleSize: 100, IncludeStatistics: req.ItemRowCount == nil})
	if err != nil {
		return nil, err
	}
	table := plugin.EngineCatalogFactsTableInfo(facts)
	if table == nil {
		return nil, fmt.Errorf("record provider returned no field facts")
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
		for len(rows) < pageSize {
			batch, readErr := session.ReadBatch(ctx, pageSize-len(rows))
			if readErr != nil {
				return nil, readErr
			}
			if len(batch.Records) == 0 {
				break
			}
			rows = append(rows, batch.Records...)
		}
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
	total := int64(len(rows))
	if req.ItemRowCount != nil && *req.ItemRowCount >= 0 {
		total = *req.ItemRowCount
	} else if table.RowCount != nil {
		total = *table.RowCount
	} else if table.EstimatedRowCount != nil {
		total = *table.EstimatedRowCount
	}
	return &models.TablePreview{Mode: PreviewModeTable, PreviewKind: "dynamic_schema_record_set", Columns: columns, Fields: table.Fields, ColumnMetadata: buildDynamicSchemaColumnMetadata(table.Fields), Rows: rows, Total: models.ExactPreviewTotal(int(total)), Page: page, PageSize: pageSize, EngineID: req.Engine.ID, Schema: req.Schema, Table: req.Table, EngineType: req.Engine.EngineType}, nil
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
