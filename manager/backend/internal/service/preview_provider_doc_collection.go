package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/manager/internal/models"
)

type docCollectionPreviewProvider struct {
	priority int
}

func NewDocCollectionPreviewProvider() PreviewProvider {
	return &docCollectionPreviewProvider{
		priority: 100,
	}
}

func (p *docCollectionPreviewProvider) Name() string {
	return "builtin:doc-collection"
}

func (p *docCollectionPreviewProvider) Priority() int {
	return p.priority
}

func (p *docCollectionPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	if req.Schema == "" || req.Table == "" {
		return false
	}

	// 检查是否有对应的 DocCollectionParser
	parser, err := format.GetDocCollectionParser(req.Engine.EngineType)
	return err == nil && parser != nil
}

func (p *docCollectionPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 1. 获取插件
	p_, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	nosqlPlugin, ok := p_.(plugin.NoSQLPlugin)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement NoSQLPlugin", req.Engine.EngineType)
	}

	// 2. 获取 Parser
	parser, err := format.GetDocCollectionParser(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("no parser found for engine type: %s", req.Engine.EngineType)
	}

	// 3. 创建客户端
	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	client, err := nosqlPlugin.CreateClient(ctx, connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer nosqlPlugin.CloseClient(ctx, client)

	// 4. 解析 Schema 和 Table
	// req.Schema 是数据库名，req.Table 可能是 "database.collection" 或只是 "collection"
	database := req.Schema
	collectionName := req.Table

	// 去掉 "database." 前缀，只保留 collection 名称
	if strings.HasPrefix(req.Table, req.Schema+".") {
		collectionName = strings.TrimPrefix(req.Table, req.Schema+".")
	}

	// 5. 获取 Schema 信息（优先从 Meta 模块获取，如果失败则实时解析）
	var tableInfo *format.TableInfo

	// 实时解析 Schema（简化版，不从 Meta 缓存）
	options := format.DefaultParseOptions()
	options.SampleSize = 100
	tableInfo, err = parser.ParseTableInfo(ctx, client, database, collectionName, options)
	if err != nil {
		// Schema 解析失败时，仍然可以读取数据，只是没有字段信息
		tableInfo = &format.TableInfo{
			Name:   collectionName,
			Fields: []format.FieldInfo{},
		}
	}

	// 6. 计算分页参数
	skip := (req.Page - 1) * req.PageSize
	limit := req.PageSize

	// 限制最大返回行数
	const maxRows = 50
	if limit > maxRows {
		limit = maxRows
	}

	// 7. 读取预览数据
	records, err := parser.ReadPreview(ctx, client, database, collectionName, int64(skip), int64(limit), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read preview: %w", err)
	}

	// 8. 提取列名（从 TableInfo 或数据中推断）
	var columns []string
	if len(tableInfo.Fields) > 0 {
		// 使用 TableInfo 中的字段顺序
		columns = make([]string, 0, len(tableInfo.Fields))
		for _, field := range tableInfo.Fields {
			columns = append(columns, field.Name)
		}
	} else if len(records) > 0 {
		// 从数据中提取字段名
		columnsSet := make(map[string]bool)
		for _, record := range records {
			for key := range record {
				columnsSet[key] = true
			}
		}

		// _id 放在最前面
		columns = make([]string, 0, len(columnsSet))
		if columnsSet["_id"] {
			columns = append(columns, "_id")
			delete(columnsSet, "_id")
		}
		for col := range columnsSet {
			columns = append(columns, col)
		}
	}

	// 9. 转换为行数据（确保字段顺序一致）
	rows := make([]map[string]interface{}, 0, len(records))
	for _, record := range records {
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

	// 10. 获取总数
	total := int64(0)
	if tableInfo.RowCount != nil {
		total = *tableInfo.RowCount
	}

	// 11. 构建预览结果
	preview := &models.TablePreview{
		Mode:         PreviewModeTable,
		Columns:      columns,
		Rows:         rows,
		Total:        int(total),
		Page:         req.Page,
		PageSize:     req.PageSize,
		EngineID:     req.Engine.ID,
		Schema:       req.Schema,
		Table:        req.Table,
		EngineType:   req.Engine.EngineType,
	}

	// 12. 添加文档集合特有信息
	if docInfo := tableInfo.GetDocCollectionInfo(); docInfo != nil {
		// 可以在预览中添加额外的元数据字段
		// preview.Metadata = map[string]interface{}{
		// 	"is_sampled":      docInfo.IsSampled,
		// 	"sample_size":     docInfo.SampleSize,
		// 	"schema_type":     docInfo.SchemaType,
		// 	"total_documents": docInfo.TotalDocuments,
		// }
	}

	return preview, nil
}
