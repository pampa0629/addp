package service

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonFormat "github.com/addp/common/format"
	parquetformat "github.com/addp/common/format/parquet"
	"github.com/addp/manager/internal/models"
)

// LakeTablePreviewProvider 湖表预览 Provider
// 支持 item_type="lake_table" 的数据项预览（Parquet/ORC/Avro 目录）
type LakeTablePreviewProvider struct{}

func NewLakeTablePreviewProvider() PreviewProvider {
	return &LakeTablePreviewProvider{}
}

func (p *LakeTablePreviewProvider) Name() string {
	return "builtin:lake-table"
}

func (p *LakeTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	contentReader, _ := plug.(plugin.ContentReadableProvider)
	catalogProvider, _ := plug.(plugin.CatalogProvider)

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)

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

	var fields []commonFormat.FieldInfo
	var rows []map[string]interface{}

	if req.PhysicalPath != "" {
		// 单文件模式：直接用 physical_path 读取（NFS/MinIO 单 parquet 文件）
		var err error
		if contentReader == nil {
			err = fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
		} else {
			fields, rows, err = parquetformat.ReadParquetFilePreviewWithProvider(ctx, contentReader, connInfo, req.Engine.ID, req.PhysicalPath, offset, limit)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read lake table preview: %w", err)
		}
	} else {
		// 目录模式：列举目录找第一个 parquet 文件
		// 目录模式：NFS 物理路径 = "/" + schema + "/" + table
		dirPath := nfsPhysicalPath(req.Schema, req.Table)
		var err error
		if catalogProvider == nil || contentReader == nil {
			err = fmt.Errorf("engine %s does not implement CatalogProvider and ContentReadableProvider", req.Engine.EngineType)
		} else {
			fields, rows, err = parquetformat.ReadFirstParquetPreviewWithProviders(ctx, catalogProvider, contentReader, connInfo, req.Engine.ID, dirPath, offset, limit)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read lake table preview: %w", err)
		}
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
