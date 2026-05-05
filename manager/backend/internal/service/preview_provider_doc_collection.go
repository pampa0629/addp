package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
)

type docCollectionPreviewProvider struct{}

func NewDocCollectionPreviewProvider() PreviewProvider {
	return &docCollectionPreviewProvider{}
}

func (p *docCollectionPreviewProvider) Name() string {
	return "builtin:doc-collection"
}

func (p *docCollectionPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 获取插件
	p_, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	documentRuntime, ok := p_.(plugin.DocumentQueryRuntimeProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement DocumentQueryRuntimeProvider", req.Engine.EngineType)
	}
	metadataProvider, _ := p_.(plugin.ItemMetadataProvider)

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
	command, err := buildDocumentFindCommand(collectionName, skip, limit)
	if err != nil {
		return nil, err
	}
	queryResult, err := documentRuntime.ExecuteDocumentQuery(ctx, runtimeConnInfo, command, plugin.QueryOptions{
		EngineID:   req.Engine.ID,
		EngineType: req.Engine.EngineType,
		Limit:      limit,
		ReadOnly:   true,
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

	// 6. 获取集合统计信息。失败不阻断预览。
	total := int64(len(rows))
	if metadataProvider != nil {
		if item, err := metadataProvider.DescribeItem(ctx, connInfo, documentCollectionCatalogPath(req.Engine.ID, database, collectionName), plugin.MetadataOptions{IncludeStatistics: true}); err == nil {
			if count := int64Stat(item.Stats, "document_count"); count > 0 {
				total = count
			}
		}
	}

	columnMetadata := buildDocumentColumnMetadata(columns, rows)

	// 7. 构建预览结果
	preview := &models.TablePreview{
		Mode:           PreviewModeTable,
		Columns:        columns,
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

func buildDocumentFindCommand(collection string, skip, limit int) (string, error) {
	command := map[string]interface{}{
		"find":   collection,
		"filter": map[string]interface{}{},
		"skip":   skip,
		"limit":  limit,
	}
	bytes, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("failed to build document preview query: %w", err)
	}
	return string(bytes), nil
}

func documentCollectionCatalogPath(engineID uint, database, collection string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: database},
			{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: collection},
		},
	}
}

func buildDocumentColumnMetadata(columns []string, rows []map[string]interface{}) []models.ColumnMetadata {
	metadata := make([]models.ColumnMetadata, 0, len(columns))
	for _, column := range columns {
		metadata = append(metadata, models.ColumnMetadata{
			ColumnName:   column,
			DataType:     inferDocumentColumnType(column, rows),
			IsNullable:   true,
			IsPrimaryKey: column == "_id",
		})
	}
	return metadata
}

func inferDocumentColumnType(column string, rows []map[string]interface{}) string {
	for _, row := range rows {
		value, ok := row[column]
		if !ok || value == nil {
			continue
		}
		switch value.(type) {
		case string:
			return "string"
		case bool:
			return "bool"
		case int, int8, int16, int32, int64:
			return "integer"
		case uint, uint8, uint16, uint32, uint64:
			return "integer"
		case float32, float64:
			return "number"
		case []interface{}:
			return "array"
		case map[string]interface{}:
			return "object"
		default:
			return fmt.Sprintf("%T", value)
		}
	}
	return "mixed"
}
