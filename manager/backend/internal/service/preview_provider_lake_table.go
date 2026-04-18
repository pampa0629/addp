package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	parquetformat "github.com/addp/common/format/parquet"
	"github.com/addp/manager/internal/models"
)

// LakeTablePreviewProvider 湖表预览 Provider
// 支持 item_type="lake_table" 的数据项预览（Parquet/ORC/Avro 目录）
type LakeTablePreviewProvider struct {
	priority int
}

func NewLakeTablePreviewProvider() PreviewProvider {
	return &LakeTablePreviewProvider{
		priority: 95, // 高于 file-table(90)，优先处理湖表
	}
}

func (p *LakeTablePreviewProvider) Name() string {
	return "builtin:lake-table"
}

func (p *LakeTablePreviewProvider) Priority() int {
	return p.priority
}

// Supports 检测是否为湖表预览请求
// 条件：ItemType == "lake_table" 且引擎为对象存储类型
func (p *LakeTablePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	return req.ItemType == "lake_table" && isObjectStorageType(req.Engine.EngineType)
}

func (p *LakeTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	// 获取 FileSystemPlugin
	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	fsPlugin, ok := plug.(plugin.FileSystemPlugin)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement FileSystemPlugin", req.Engine.EngineType)
	}

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)

	// 构建目录路径：schema 是 bucket，table 是目录路径
	dirPath := req.Schema
	if req.Table != "" {
		dirPath = req.Schema + "/" + strings.TrimPrefix(req.Table, "/")
	}

	// 分页参数
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	// 读取预览数据
	fields, rows, err := parquetformat.ReadFirstParquetPreview(ctx, fsPlugin, connInfo, dirPath, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to read lake table preview: %w", err)
	}

	// 构建列名和列元数据
	columns := make([]string, 0, len(fields))
	columnMeta := make([]models.ColumnMetadata, 0, len(fields))
	for _, f := range fields {
		columns = append(columns, f.Name)
		columnMeta = append(columnMeta, models.ColumnMetadata{
			ColumnName: f.Name,
			DataType:   string(f.Type),
			IsNullable: f.Nullable,
		})
	}

	return &models.TablePreview{
		Mode:           "table",
		Columns:        columns,
		ColumnMetadata: columnMeta,
		Rows:           rows,
		Page:           page,
		PageSize:       pageSize,
		Total:          len(rows),
	}, nil
}

