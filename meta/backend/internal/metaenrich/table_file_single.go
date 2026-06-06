package metaenrich

import (
	"bufio"
	"context"
	"fmt"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
)

// ExtractSingleTableFileItem 提取单个表格文件 item 主事实（模式 B：文件即表）。
func ExtractSingleTableFileItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
	includeAccessIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	formatName := fileFormatName(filePath)
	return extractSingleTableFileItemWithFormat(ctx, contentReader, connInfo, engineID, filePath, fileSize, includeAccessIndex, formatName, catalogPathFor...)
}

func extractSingleTableFileItemWithFormat(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
	includeAccessIndex bool,
	formatName string,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	formatType := format.NormalizeFormat(formatName)
	provider, providerErr := format.GetTableInfoProvider(formatType)
	if providerErr != nil {
		return &metaitem.CompositeItemInfo{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Table,
			Format:             formatName,
			PrimaryContentPath: filePath,
			RefFiles:           []string{filePath},
			SizeBytes:          &fileSize,
			Attributes:         tableFileAttributes(formatName, "single", []metaitem.StorageFileRef{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil, false),
		}, nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, resolveTableFileCatalogPath(engineID, filePath, firstCatalogPathResolver(catalogPathFor)), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read table file %s: %w", filePath, err)
	}
	defer rc.Close()

	tableInfo, err := provider.DescribeTable(ctx, rc, nil)
	if err != nil {
		// Schema 解析失败时返回基础信息，不阻断扫描
		return &metaitem.CompositeItemInfo{
			Layout:             format.LayoutSingle,
			DataType:           datatype.Table,
			Format:             formatName,
			PrimaryContentPath: filePath,
			RefFiles:           []string{filePath},
			SizeBytes:          &fileSize,
			Attributes:         tableFileAttributes(formatName, "single", []metaitem.StorageFileRef{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil, false),
		}, nil
	}

	fields := tableFieldsFromDescribeResult(tableInfo)

	return &metaitem.CompositeItemInfo{
		Fields:             fields,
		Layout:             format.LayoutSingle,
		DataType:           datatype.Table,
		Format:             formatName,
		PrimaryContentPath: filePath,
		RefFiles:           []string{filePath},
		SizeBytes:          &fileSize,
		Attributes:         tableFileAttributes(formatName, "single", []metaitem.StorageFileRef{{Path: filePath, Size: fileSize}}, filePath, fileSize, tableInfo, includeAccessIndex && format.SupportsAccessIndex(formatType)),
	}, nil
}

// ExtractSingleTableFileItemStrict 仅在格式 provider 成功解析出表结构时返回 item 主事实。
// 适用于需要通过内容结构确认是否可作为 table item 的格式。
func ExtractSingleTableFileItemStrict(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
	includeAccessIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	rc, err := contentReader.OpenContent(ctx, connInfo, resolveTableFileCatalogPath(engineID, filePath, firstCatalogPathResolver(catalogPathFor)), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read table file %s: %w", filePath, err)
	}
	defer rc.Close()

	br := bufio.NewReader(rc)
	formatType := format.NormalizeFormat(fileFormatName(filePath))
	if peek, _ := br.Peek(4096); len(peek) > 0 {
		if detected := format.DetectFormat(filePath, peek); detected != format.FormatUnknown {
			formatType = detected
		}
	}
	formatName := string(formatType)
	provider, providerErr := format.GetTableInfoProvider(formatType)
	if providerErr != nil {
		return nil, providerErr
	}

	tableInfo, err := provider.DescribeTable(ctx, br, nil)
	if err != nil {
		return nil, err
	}

	fields := tableFieldsFromDescribeResult(tableInfo)
	return &metaitem.CompositeItemInfo{
		Fields:             fields,
		Layout:             format.LayoutSingle,
		DataType:           datatype.Table,
		Format:             formatName,
		PrimaryContentPath: filePath,
		RefFiles:           []string{filePath},
		SizeBytes:          &fileSize,
		Attributes:         tableFileAttributes(formatName, "single", []metaitem.StorageFileRef{{Path: filePath, Size: fileSize}}, filePath, fileSize, tableInfo, includeAccessIndex && format.SupportsAccessIndex(formatType)),
	}, nil
}

func EnrichSingleTableFileItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	item *metaitem.DetectedItem,
	filePath string,
	fileSize int64,
	includeAccessIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.DetectedItem, bool, error) {
	if item == nil || item.Layout != format.LayoutSingle {
		return item, false, nil
	}
	if IsUnknownFormatName(item.Format) {
		if catalogPath := firstCatalogPathResolver(catalogPathFor); catalogPath != nil {
			detectedFormat, err := DetectSingleFileFormat(ctx, contentReader, connInfo, catalogPath(filePath), filePath)
			if err != nil {
				return item, false, err
			}
			ApplySingleFileFormat(item, detectedFormat)
		}
	}
	if !hasSingleTableProvider(item.Format) {
		return item, false, nil
	}
	if item.DataType == datatype.Unknown {
		item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
	}
	if item.DataType != datatype.Table {
		info, err := ExtractSingleTableFileItemStrict(ctx, contentReader, connInfo, engineID, filePath, fileSize, includeAccessIndex, firstCatalogPathResolver(catalogPathFor))
		if err != nil || info == nil {
			return item, false, err
		}
		return metaitem.DetectedItemFromCompositeInfo(info, filePath, fileSize), true, nil
	}
	info, err := extractSingleTableFileItemWithFormat(ctx, contentReader, connInfo, engineID, filePath, fileSize, includeAccessIndex, item.Format, firstCatalogPathResolver(catalogPathFor))
	if err != nil || info == nil {
		return item, false, err
	}
	return metaitem.DetectedItemFromCompositeInfo(info, filePath, fileSize), true, nil
}
