package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/models"
)

// ScopeTablePreviewProvider 目录型表格预览 Provider。
// 当前主要服务 organization=whole 的 Parquet/ORC/Avro 表格资源。
type ScopeTablePreviewProvider struct{}

func NewScopeTablePreviewProvider() PreviewProvider {
	return &ScopeTablePreviewProvider{}
}

func (p *ScopeTablePreviewProvider) Name() string {
	return "builtin:scope-table"
}

func (p *ScopeTablePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	plug, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", req.Engine.EngineType)
	}

	contentReader, _ := plug.(plugin.ContentReadableProvider)
	catalogProvider, _ := plug.(plugin.CatalogProvider)

	connInfo := plugin.ConnectionInfo(req.Engine.ConnectionInfo)
	reader, err := scopeTableResourceReader(req, contentReader, catalogProvider, connInfo)
	if err != nil {
		return nil, err
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

	formatType := resolveScopeTableFormat(req)
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("no table provider for %s: %w", formatType, err)
	}
	tableProvider, ok := provider.(format.ScopeTableProvider)
	if !ok {
		return nil, fmt.Errorf("%s provider does not implement scope table provider", formatType)
	}

	var tableInfo *format.TableInfo
	var rows []map[string]interface{}

	if req.PhysicalPath != "" {
		if contentReader == nil {
			err = fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
		} else {
			ref := resource.NewResourceRef(req.PhysicalPath, resource.ResourceRoleMain)
			input, openErr := reader.Open(ctx, ref)
			if openErr != nil {
				err = openErr
			} else {
				tableInfo, err = provider.DescribeTable(ctx, input, nil)
				input.Close()
			}
			if err == nil {
				input, openErr := reader.Open(ctx, ref)
				if openErr != nil {
					err = openErr
				} else {
					rows, err = provider.SampleTable(ctx, input, offset, limit, nil)
					input.Close()
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read scope table preview: %w", err)
		}
	} else {
		dirPath := req.ScopePath
		if dirPath == "" {
			dirPath = nfsPhysicalPath(req.Schema, req.Table)
		}
		if catalogProvider == nil || contentReader == nil {
			err = fmt.Errorf("engine %s does not implement CatalogProvider and ContentReadableProvider", req.Engine.EngineType)
		} else {
			scope := resource.NewResourceRef(dirPath, resource.ResourceRoleScope)
			tableInfo, err = scopeTableInfoFromAttributes(req.Attributes)
			if err != nil {
				return nil, fmt.Errorf("failed to read scope table attributes: %w", err)
			}
			sampleOptions := scopeTableSampleOptionsFromAttributes(req.Attributes)
			if tableInfo == nil {
				tableInfo, err = tableProvider.DescribeTableScope(ctx, reader, scope, nil)
			}
			if err == nil {
				rows, err = tableProvider.SampleTableScope(ctx, reader, scope, offset, limit, sampleOptions)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read scope table preview: %w", err)
		}
	}

	// 构建列名和列元数据
	fields := tableInfo.Fields
	total := int64(len(rows))
	if tableInfo.RowCount != nil {
		total = *tableInfo.RowCount
	}
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
		Total:          int(total),
	}, nil
}

func scopeTableInfoFromAttributes(attrs map[string]interface{}) (*format.TableInfo, error) {
	tableAttrs := commonJSON.Section(attrs, "type_info.table")
	if len(tableAttrs) == 0 {
		return nil, nil
	}
	fields := fieldsFromAttribute(tableAttrs["fields"])
	rowCount := commonJSON.InterfaceInt64(tableAttrs["row_count"])
	if len(fields) == 0 {
		return nil, nil
	}
	info := &format.TableInfo{
		Name:   "table",
		Fields: fields,
	}
	if rowCount > 0 {
		info.RowCount = &rowCount
	}
	return info, nil
}

func scopeTableSampleOptionsFromAttributes(attrs map[string]interface{}) *format.ParseOptions {
	formatName := strings.TrimSpace(commonJSON.InterfaceString(attrs["format"]))
	if formatName == "" {
		return nil
	}
	provider, err := format.GetTableProvider(format.FormatType(formatName))
	if err != nil {
		return nil
	}
	optionsProvider, ok := provider.(interface {
		SampleOptionsFromAttributes(map[string]interface{}) *format.ParseOptions
	})
	if !ok {
		return nil
	}
	return optionsProvider.SampleOptionsFromAttributes(attrs)
}

func scopeTableResourceReader(req *PreviewRequest, contentReader plugin.ContentReadableProvider, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo) (resource.ResourceReader, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	if previewRequestCatalogItemTerm(req) == "object" {
		bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, err
		}
		return newObjectCatalogResourceReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket), nil
	}
	return newFileCatalogResourceReader(contentReader, catalogProvider, connInfo, req.Engine.ID), nil
}

func resolveScopeTableFormat(req *PreviewRequest) format.FormatType {
	if req == nil {
		return format.FormatUnknown
	}
	if formatName := strings.TrimSpace(stringAttribute(req.Attributes, "format")); formatName != "" {
		return normalizeFileTableFormat(formatName)
	}
	if req.PhysicalPath != "" {
		return format.DetectFormat(req.PhysicalPath, nil)
	}
	return format.FormatUnknown
}
