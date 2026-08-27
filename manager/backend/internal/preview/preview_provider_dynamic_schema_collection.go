package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

type dynamicSchemaCollectionPreviewProvider struct{}

func NewDynamicSchemaCollectionPreviewProvider() PreviewProvider {
	return &dynamicSchemaCollectionPreviewProvider{}
}

func (p *dynamicSchemaCollectionPreviewProvider) Name() string {
	return "builtin:dynamic-schema-collection"
}

func (p *dynamicSchemaCollectionPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 获取插件
	p_, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	queryRuntime, ok := p_.(plugin.QueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement QueryRuntimeProvider", req.Engine.EngineType)
	}
	factsProvider, ok := p_.(plugin.EngineCatalogFactsProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement EngineCatalogFactsProvider", req.Engine.EngineType)
	}

	// 2. 解析 Schema 和 Table
	// req.Schema 是数据库名，req.Table 可能是 "database.collection" 或只是 "collection"
	database := req.Schema
	collectionName := req.Table

	// 去掉 "database." 前缀，只保留 collection 名称
	if strings.HasPrefix(req.Table, req.Schema+".") {
		collectionName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	// 3. 计算分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	skip := (page - 1) * pageSize
	limit := pageSize

	// 限制最大返回行数
	const maxRows = 50
	if limit > maxRows {
		limit = maxRows
	}

	// 4. 读取预览数据。MongoDB runtime 使用 connInfo.database，因此这里按请求的 database 覆盖副本。
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	runtimeConnInfo := cloneConnectionInfo(connInfo)
	runtimeConnInfo["database"] = database
	command, err := buildDynamicSchemaFindCommand(collectionName, skip, limit)
	if err != nil {
		return nil, err
	}
	queryResult, err := queryRuntime.ExecuteRuntimeQuery(ctx, runtimeConnInfo, plugin.QueryRequest{
		EngineID: req.Engine.ID,
		Language: "mql",
		Query:    command,
		Options: plugin.QueryOptions{
			EngineID:   req.Engine.ID,
			EngineType: req.Engine.EngineType,
			Limit:      limit,
			ReadOnly:   true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read preview: %w", err)
	}

	columns := queryResult.Columns

	// 5. 转换为行数据（确保字段顺序一致）
	rows := make([]map[string]interface{}, 0, len(queryResult.Rows))
	for _, record := range queryResult.Rows {
		row := make(map[string]interface{})
		for _, col := range columns {
			if val, ok := record[col]; ok {
				row[col] = val
			} else {
				row[col] = nil
			}
		}
		rows = append(rows, row)
	}

	// 6. 字段结构统一来自 Provider 动态 schema 采样，预览行只负责展示。
	catalogFacts, err := factsProvider.DescribeEngineCatalogFacts(ctx, connInfo, req.ProviderPath, plugin.EngineCatalogFactsOptions{
		SampleSize:        100,
		IncludeStatistics: req.ItemRowCount == nil,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe dynamic schema: %w", err)
	}
	tableInfo := plugin.EngineCatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil {
		return nil, fmt.Errorf("dynamic schema provider returned no table facts")
	}

	// 7. 优先使用 Meta 已扫描的精确行数；缺失时使用本次 Provider 事实。
	total := int64(len(rows))
	if req.ItemRowCount != nil && *req.ItemRowCount >= 0 {
		total = *req.ItemRowCount
	} else if tableInfo.RowCount != nil && *tableInfo.RowCount >= 0 {
		total = *tableInfo.RowCount
	} else if tableInfo.EstimatedRowCount != nil && *tableInfo.EstimatedRowCount >= 0 {
		total = *tableInfo.EstimatedRowCount
	}

	columnMetadata := buildDynamicSchemaColumnMetadata(tableInfo.Fields)

	// 8. 构建预览结果
	preview := &models.TablePreview{
		Mode:           PreviewModeTable,
		PreviewKind:    "dynamic_schema_record_set",
		Columns:        columns,
		Fields:         tableInfo.Fields,
		ColumnMetadata: columnMetadata,
		Rows:           rows,
		Total:          int(total),
		Page:           page,
		PageSize:       pageSize,
		EngineID:       req.Engine.ID,
		Schema:         req.Schema,
		Table:          req.Table,
		EngineType:     req.Engine.EngineType,
	}

	return preview, nil
}

func cloneConnectionInfo(connInfo plugin.ConnectionInfo) plugin.ConnectionInfo {
	cloned := make(plugin.ConnectionInfo, len(connInfo))
	for key, value := range connInfo {
		cloned[key] = value
	}
	return cloned
}

func buildDynamicSchemaFindCommand(collection string, skip, limit int) (string, error) {
	command := map[string]interface{}{
		"find":   collection,
		"filter": map[string]interface{}{},
		"skip":   skip,
		"limit":  limit,
	}
	bytes, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("failed to build dynamic schema collection preview query: %w", err)
	}
	return string(bytes), nil
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
