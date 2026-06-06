package metaenrich

import (
	"context"
	"fmt"
	"sync"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/meta/internal/metaitem"
)

var tableFileAuxiliaryFileNames = map[string]bool{
	"_success":         true,
	"_metadata":        true,
	"_common_metadata": true,
}

// TableFileResolver 识别 common/dataitem 判定出的表格文件 item，并补充 schema 等 Meta 落库信息。
type tableFileItemResolver struct{}

var registerItemResolversOnce sync.Once

func RegisterItemResolvers() {
	registerItemResolversOnce.Do(func() {
		metaitem.RegisterResolver(&tableFileItemResolver{})
	})
}

func tableFileItemRules() []dataitem.FormatRule {
	rules := []dataitem.FormatRule{}
	for _, rule := range dataitem.BuiltinSingleResourceRules() {
		if tableFileRuleCanRead(rule) {
			rules = append(rules, rule)
		}
	}
	for _, rule := range dataitem.BuiltinWholeScopeRules() {
		if tableFileRuleCanRead(rule) {
			rules = append(rules, rule)
		}
	}
	return rules
}

func tableFileRuleCanRead(rule dataitem.FormatRule) bool {
	if rule.DataType != datatype.Table {
		return false
	}
	switch rule.Layout {
	case format.LayoutSingle:
		return hasSingleTableProvider(rule.Format)
	case format.LayoutWhole:
		return hasScopeTableProvider(rule.Format)
	default:
		return false
	}
}

func (d *tableFileItemResolver) Priority() int {
	return 80
}

func (d *tableFileItemResolver) Rules() []dataitem.FormatRule {
	return tableFileItemRules()
}

func (d *tableFileItemResolver) ResolveItems(
	ctx context.Context,
	input metaitem.DirectoryResolveInput,
) (*metaitem.DetectionResult, error) {
	files := input.Files
	subdirs := input.Subdirs
	if len(input.RecursiveFiles) > 0 {
		files = input.RecursiveFiles
		subdirs = input.RecursiveSubdirs
	}
	if !d.Detect(ctx, files, subdirs) {
		return nil, nil
	}
	info, err := d.extractTableDatasetInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.DirPath, files, subdirs, input.CatalogPathFor)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	totalSize := int64(0)
	if info.SizeBytes != nil {
		totalSize = *info.SizeBytes
	}
	item := &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:             info.Layout,
			DataType:           info.DataType,
			Format:             info.Format,
			PrimaryContentPath: info.PrimaryContentPath,
			ScopePath:          info.ScopePath,
			RefList:            metaitem.ItemRefsFromPaths(info.RefFiles),
			SizeBytes:          &totalSize,
		},
		PhysicalPath: input.DirPath,
		Fields:       info.Fields,
		Attributes:   info.Attributes,
	}
	claims := metaitem.ResourceClaimSet{}
	for _, path := range item.RefFilePaths() {
		claims[path] = true
	}
	return &metaitem.DetectionResult{
		Items:     []*metaitem.DetectedItem{item},
		Claims:    claims,
		Exclusive: item.Layout == format.LayoutWhole,
	}, nil
}

func (d *tableFileItemResolver) Detect(ctx context.Context, files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef) bool {
	_, ok, err := resolveTableFileDataItem(inferScopePath(files, subdirs), files)
	return err == nil && ok
}

func (d *tableFileItemResolver) extractTableDatasetInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
	catalogPathFor func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	files, err := validateTableFiles(files, dirPath)
	if err != nil {
		return nil, err
	}

	firstReadableFile := firstReadableTableFile(files, dirPath)
	if firstReadableFile == nil {
		return tableDatasetInfoWithoutSchema(files, subdirs, dirPath), nil
	}
	if contentReader == nil {
		return tableDatasetInfoWithoutSchema(files, subdirs, dirPath), nil
	}

	formatName := detectFormat(files)
	reader := tableFileContentReader{
		contentReader:  contentReader,
		connInfo:       connInfo,
		engineID:       engineID,
		catalogPathFor: catalogPathFor,
		files:          files,
		subdirs:        subdirs,
	}
	tableInfo, err := describeTableFileScope(ctx, formatName, reader, dirPath)
	if err != nil {
		rc, openErr := contentReader.OpenContent(ctx, connInfo, resolveTableFileCatalogPath(engineID, firstReadableFile.Path, catalogPathFor), plugin.ReadOptions{})
		if openErr != nil {
			return nil, fmt.Errorf("failed to read table file %s: %w", firstReadableFile.Path, openErr)
		}
		tableInfo, err = describeTableFile(ctx, fileFormatName(firstReadableFile.Name), rc)
		closeErr := rc.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to parse table info from %s: %w", firstReadableFile.Path, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close table file %s: %w", firstReadableFile.Path, closeErr)
		}
	}

	resolved, _, _ := resolveTableFileDataItem(dirPath, files)
	totalSize := tableFileSize(files)
	layout := tableFileLayout(files, subdirs, dirPath)
	primaryContentPath := tableFilePrimaryContentPath(files, subdirs, dirPath)
	scopePath := ""
	if resolved != nil {
		formatName = resolved.Format
		layout = resolved.Layout
		primaryContentPath = resolved.PrimaryContentPath
		scopePath = resolved.ScopePath
		if resolved.SizeBytes != nil {
			totalSize = *resolved.SizeBytes
		}
	}
	if layout == format.LayoutWhole && scopePath == "" {
		scopePath = dirPath
	}
	fields := tableFieldsFromDescribeResult(tableInfo)
	return &metaitem.CompositeItemInfo{
		Fields:             fields,
		Layout:             layout,
		DataType:           datatype.Table,
		Format:             formatName,
		PrimaryContentPath: primaryContentPath,
		ScopePath:          scopePath,
		RefFiles:           filePaths(files),
		SizeBytes:          &totalSize,
		Attributes:         tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), files, dirPath, totalSize, tableInfo, false),
	}, nil
}

func extractTableFileWholeScopeInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []metaitem.StorageFileRef,
	subdirs []metaitem.StorageDirectoryRef,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	resolver := &tableFileItemResolver{}
	if !resolver.Detect(ctx, files, subdirs) {
		return nil, fmt.Errorf("directory is not a table file dataset: %s", dirPath)
	}
	return resolver.extractTableDatasetInfo(ctx, contentReader, connInfo, engineID, dirPath, files, subdirs, firstCatalogPathResolver(catalogPathFor))
}
