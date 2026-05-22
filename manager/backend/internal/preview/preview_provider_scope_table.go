package preview

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/models"
)

// ScopeTablePreviewProvider 目录型表格预览 Provider。
// 当前主要服务 layout=whole 的 Parquet/ORC/Avro 表格资源。
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
	reader, err := scopeTableContentReader(req, contentReader, catalogProvider, connInfo)
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
	infoProvider, err := format.GetTableInfoProvider(formatType)
	if err != nil && req.PhysicalPath != "" {
		return nil, fmt.Errorf("no table info provider for %s: %w", formatType, err)
	}
	sampleReader, err := format.GetTableSampleReader(formatType)
	if err != nil && req.PhysicalPath != "" {
		return nil, fmt.Errorf("no table sample reader for %s: %w", formatType, err)
	}
	scopeInfoProvider, err := format.GetScopeTableInfoProvider(formatType)
	if err != nil && req.PhysicalPath == "" {
		return nil, fmt.Errorf("no scope table info provider for %s: %w", formatType, err)
	}
	scopeSampleReader, err := format.GetScopeTableSampleReader(formatType)
	if err != nil && req.PhysicalPath == "" {
		return nil, fmt.Errorf("no scope table sample reader for %s: %w", formatType, err)
	}

	var tableInfo *format.TableInfo
	var rows []map[string]interface{}

	if req.PhysicalPath != "" {
		if contentReader == nil {
			err = fmt.Errorf("engine %s does not implement ContentReadableProvider", req.Engine.EngineType)
		} else {
			ref := contentio.NewRef(req.PhysicalPath, contentio.RoleMain)
			input, openErr := reader.Open(ctx, ref)
			if openErr != nil {
				err = openErr
			} else {
				result, describeErr := infoProvider.DescribeTable(ctx, input, nil)
				if describeErr == nil {
					tableInfo = format.TableSchemaFromDescribeResult(result)
				}
				err = describeErr
				input.Close()
			}
			if err == nil {
				input, openErr := reader.Open(ctx, ref)
				if openErr != nil {
					err = openErr
				} else {
					rows, err = sampleReader.SampleTable(ctx, input, offset, limit, nil)
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
			scope := contentio.NewRef(dirPath, contentio.RoleScope)
			tableInfo, err = scopeTableInfoFromAttributes(req.Attributes)
			if err != nil {
				return nil, fmt.Errorf("failed to read scope table attributes: %w", err)
			}
			sampleOptions := scopeTableSampleOptionsFromAttributes(req.Attributes)
			if tableInfo == nil {
				result, describeErr := scopeInfoProvider.DescribeTableScope(ctx, reader, scope, nil)
				if describeErr == nil {
					tableInfo = format.TableSchemaFromDescribeResult(result)
				}
				err = describeErr
			}
			if err == nil {
				rows, err = scopeSampleReader.SampleTableScope(ctx, reader, scope, offset, limit, sampleOptions)
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
			Type:       string(f.Type),
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
	plugin, err := format.GetFormatPlugin(format.FormatType(formatName))
	if err == nil {
		if optionsProvider, ok := plugin.(interface {
			SampleOptionsFromAttributes(map[string]interface{}) *format.ParseOptions
		}); ok {
			return optionsProvider.SampleOptionsFromAttributes(attrs)
		}
	}
	infoProvider, err := format.GetScopeTableInfoProvider(format.FormatType(formatName))
	if err != nil {
		return nil
	}
	optionsProvider, ok := infoProvider.(interface {
		SampleOptionsFromAttributes(map[string]interface{}) *format.ParseOptions
	})
	if !ok {
		return nil
	}
	return optionsProvider.SampleOptionsFromAttributes(attrs)
}

func scopeTableContentReader(req *PreviewRequest, contentReader plugin.ContentReadableProvider, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo) (contentio.Reader, error) {
	if req == nil || req.Engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	if previewRequestCatalogItemTerm(req) == "object" {
		bucket, err := resolveBucket(plugin.GetString(connInfo, "bucket"), req.Schema)
		if err != nil {
			return nil, err
		}
		return newObjectCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID, bucket), nil
	}
	return newFileCatalogContentReader(contentReader, catalogProvider, connInfo, req.Engine.ID), nil
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
