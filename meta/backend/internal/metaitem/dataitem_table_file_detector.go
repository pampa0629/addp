package metaitem

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/plugins/csv"
	_ "github.com/addp/common/format/plugins/parquet"
	"github.com/addp/meta/internal/dataitem"
)

// tableFileFormats 支持的表格文件格式
var tableFileFormats = map[string]bool{
	".parquet": true,
	".orc":     true,
	".avro":    true,
}

var tableFileAuxiliaryFileNames = map[string]bool{
	"_success":         true,
	"_metadata":        true,
	"_common_metadata": true,
}

var tableFileItemRules = []dataitem.FormatRule{
	{
		Format:       "parquet",
		DataType:     dataitem.DataTypeTable,
		ItemType:     "table",
		Organization: dataitem.OrganizationSingle,
		Priority:     40,
		Entry: dataitem.EntryRule{
			Extensions: []string{".parquet", ".orc", ".avro"},
		},
	},
	{
		Format:       "parquet",
		DataType:     dataitem.DataTypeTable,
		ItemType:     "table",
		Organization: dataitem.OrganizationWhole,
		Priority:     80,
		WholeScope: &dataitem.WholeScopeRule{
			AllowRecursive:       true,
			IgnoredFileNames:     []string{"_SUCCESS", "_metadata", "_common_metadata"},
			RequiresStrongMatch:  true,
			ExclusiveOnStrongHit: true,
		},
	},
}

// TableFileDetector 表格文件检测器。
// 检测条件：目录树内存在 .parquet/.orc/.avro 文件，且参与候选的文件全部为表格数据文件或常见辅助文件。
type tableFileItemDetector struct{}

func (d *tableFileItemDetector) Priority() int {
	return 80
}

func (d *tableFileItemDetector) Rules() []dataitem.FormatRule {
	return tableFileItemRules
}

func (d *tableFileItemDetector) ItemType() string {
	return "table"
}

func (d *tableFileItemDetector) ResolveItems(
	ctx context.Context,
	input DirectoryResolveInput,
) (*DetectionResult, error) {
	files := input.Files
	subdirs := input.Subdirs
	if len(input.RecursiveFiles) > 0 {
		files = input.RecursiveFiles
		subdirs = input.RecursiveSubdirs
	}
	if !d.Detect(ctx, files, subdirs) {
		return nil, nil
	}
	info, err := d.extractTableFileInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.DirPath, files, subdirs)
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
	item := &DetectedItem{
		ItemType:       d.ItemType(),
		Organization:   info.Organization,
		DataType:       info.DataType,
		Format:         info.Format,
		PhysicalPath:   input.DirPath,
		EntryPath:      info.EntryPath,
		ComponentFiles: info.ComponentFiles,
		SizeBytes:      totalSize,
		Fields:         info.Fields,
		Attributes:     info.Attributes,
	}
	claims := ResourceClaimSet{}
	for _, path := range item.ComponentFiles {
		claims[path] = true
	}
	return &DetectionResult{
		Items:     []*DetectedItem{item},
		Claims:    claims,
		Exclusive: item.Organization == dataitem.OrganizationWhole,
	}, nil
}

func (d *tableFileItemDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	tableFileCount := 0
	auxiliaryFileCount := 0
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !tableFileFormats[ext] {
			if isTableFileAuxiliaryFile(f.Name) {
				auxiliaryFileCount++
				continue
			}
			return false
		}
		tableFileCount++
	}
	if tableFileCount == 0 {
		return false
	}
	if len(subdirs) == 0 {
		return tableFileCount == 1 || auxiliaryFileCount > 0 || hasPartLikeTableFiles(files)
	}
	return hasPartitionLikeSubdir(subdirs)
}

func hasPartitionLikeSubdir(subdirs []plugin.DirEntry) bool {
	for _, subdir := range subdirs {
		name := strings.Trim(strings.ToLower(subdir.Name), "/")
		if name == "" {
			continue
		}
		if strings.Contains(name, "=") || strings.HasPrefix(name, "_") {
			return true
		}
	}
	return false
}

func hasPartLikeTableFiles(files []plugin.FileEntry) bool {
	tableFileCount := 0
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !tableFileFormats[ext] {
			continue
		}
		tableFileCount++
		name := strings.ToLower(strings.TrimSpace(filepath.Base(file.Name)))
		if !(strings.HasPrefix(name, "part-") || strings.HasPrefix(name, "part_")) {
			return false
		}
	}
	return tableFileCount > 1
}

func tableFiles(files []plugin.FileEntry) []plugin.FileEntry {
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if tableFileFormats[ext] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func isTableFileAuxiliaryFile(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if normalized == "" {
		return false
	}
	if tableFileAuxiliaryFileNames[normalized] {
		return true
	}
	if strings.HasPrefix(normalized, ".") && strings.Contains(normalized, ".crc") {
		return true
	}
	return strings.HasSuffix(normalized, ".crc")
}

func directTableFiles(files []plugin.FileEntry, dirPath string) []plugin.FileEntry {
	trimmedDir := strings.Trim(dirPath, "/")
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, f := range tableFiles(files) {
		parent := strings.Trim(filepath.ToSlash(filepath.Dir(strings.Trim(f.Path, "/"))), "/")
		if parent == "." {
			parent = ""
		}
		if parent == trimmedDir {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func firstReadableParquetFile(files []plugin.FileEntry, dirPath string) *plugin.FileEntry {
	candidates := directTableFiles(files, dirPath)
	for i := range candidates {
		ext := strings.ToLower(filepath.Ext(candidates[i].Name))
		if ext == ".parquet" {
			return &candidates[i]
		}
	}
	for i := range files {
		ext := strings.ToLower(filepath.Ext(files[i].Name))
		if ext == ".parquet" {
			return &files[i]
		}
	}
	return nil
}

func tableFileEntryPath(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if len(subdirs) > 0 {
		return dirPath
	}
	if directFiles := directTableFiles(files, dirPath); len(directFiles) > 1 {
		return dirPath
	} else if len(directFiles) == 1 {
		return directFiles[0].Path
	}
	if tableDataFiles := tableFiles(files); len(tableDataFiles) == 1 {
		return tableDataFiles[0].Path
	}
	return dirPath
}

func tableFileOrganization(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) dataitem.Organization {
	if len(subdirs) > 0 {
		return dataitem.OrganizationWhole
	}
	if len(directTableFiles(files, dirPath)) > 1 || len(tableFiles(files)) > 1 {
		return dataitem.OrganizationWhole
	}
	return dataitem.OrganizationSingle
}

func validateTableFiles(files []plugin.FileEntry, dirPath string) ([]plugin.FileEntry, error) {
	filtered := tableFiles(files)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no table file files in directory: %s", dirPath)
	}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if tableFileFormats[ext] || isTableFileAuxiliaryFile(f.Name) {
			continue
		}
		return nil, fmt.Errorf("directory contains files outside supported scope table formats: %s", dirPath)
	}
	return filtered, nil
}

func tableFileSize(files []plugin.FileEntry) int64 {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	return totalSize
}

func tableFileFieldAttributes(fields []format.FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":          f.Name,
			"type":          string(f.Type),
			"original_type": f.OriginalType,
			"nullable":      f.Nullable,
		})
	}
	return fieldsData
}

func tableFileAttributes(formatName string, mode string, fieldsData []map[string]interface{}, files []plugin.FileEntry, dirPath string, totalSize int64, tableInfo *format.TableInfo) map[string]interface{} {
	attrs := map[string]interface{}{
		"storage": map[string]interface{}{
			"physical_path": dirPath,
			"total_size":    totalSize,
		},
		"format_info": map[string]interface{}{
			formatName: map[string]interface{}{
				"mode":       mode,
				"file_count": len(files),
			},
		},
	}
	if len(fieldsData) > 0 {
		tableAttrs := map[string]interface{}{
			"fields": fieldsData,
		}
		if tableInfo != nil && tableInfo.RowCount != nil {
			tableAttrs["row_count"] = *tableInfo.RowCount
		}
		attrs["type_info"] = map[string]interface{}{
			"table": tableAttrs,
		}
	}
	if tableInfo != nil {
		if indexInfo := tableInfo.GetContentIndexInfo(); indexInfo != nil && indexInfo.Table != nil {
			if indexInfo.Table.Source == nil {
				indexInfo.Table.Source = map[string]interface{}{
					"size_bytes": totalSize,
				}
			}
			attrs["content_index"] = map[string]interface{}{
				"table": indexInfo.Table,
			}
		}
	}
	return attrs
}

func tableFileMode(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if tableFileOrganization(files, subdirs, dirPath) == dataitem.OrganizationWhole {
		return "whole"
	}
	return "single"
}

func tableFileInfoWithoutSchema(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) *CompositeItemInfo {
	totalSize := tableFileSize(files)
	formatName := detectFormat(files)
	return &CompositeItemInfo{
		Organization:   tableFileOrganization(files, subdirs, dirPath),
		DataType:       dataitem.DataTypeTable,
		Format:         formatName,
		EntryPath:      tableFileEntryPath(files, subdirs, dirPath),
		ComponentFiles: filePaths(files),
		SizeBytes:      &totalSize,
		Attributes:     tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), nil, files, dirPath, totalSize, nil),
	}
}

func (d *tableFileItemDetector) extractTableFileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*CompositeItemInfo, error) {
	files, err := validateTableFiles(files, dirPath)
	if err != nil {
		return nil, err
	}

	firstParquet := firstReadableParquetFile(files, dirPath)
	if firstParquet == nil {
		return tableFileInfoWithoutSchema(files, subdirs, dirPath), nil
	}
	if contentReader == nil {
		return tableFileInfoWithoutSchema(files, subdirs, dirPath), nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, tableFileCatalogPath(engineID, firstParquet.Path), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", firstParquet.Path, err)
	}
	defer rc.Close()

	tableInfo, err := describeParquetTable(ctx, rc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parquet schema from %s: %w", firstParquet.Path, err)
	}

	totalSize := tableFileSize(files)
	formatName := detectFormat(files)
	fieldsData := tableFileFieldAttributes(tableInfo.Fields)
	return &CompositeItemInfo{
		Fields:         tableInfo.Fields,
		Organization:   tableFileOrganization(files, subdirs, dirPath),
		DataType:       dataitem.DataTypeTable,
		Format:         formatName,
		EntryPath:      tableFileEntryPath(files, subdirs, dirPath),
		ComponentFiles: filePaths(files),
		SizeBytes:      &totalSize,
		Attributes:     tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), fieldsData, files, dirPath, totalSize, tableInfo),
	}, nil
}

// ExtractItemInfo 提取表格文件元信息（读取第一个 Parquet 文件获取 Schema）
func (d *tableFileItemDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*CompositeItemInfo, error) {
	return d.extractTableFileInfo(ctx, contentReader, connInfo, engineID, dirPath, files, nil)
}

func extractTableFileWholeScopeInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*CompositeItemInfo, error) {
	detector := &tableFileItemDetector{}
	if !detector.Detect(ctx, files, subdirs) {
		return nil, fmt.Errorf("directory is not a table file dataset: %s", dirPath)
	}
	return detector.extractTableFileInfo(ctx, contentReader, connInfo, engineID, dirPath, files, subdirs)
}

// ExtractTableFileSingleFileInfo 提取单个表格文件的元信息（模式 B：文件即表）。
func ExtractTableFileSingleFileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
) (*CompositeItemInfo, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	formatName := ext[1:] // 去掉点号，如 "parquet"

	provider, providerErr := format.GetTableProvider(format.FormatType(formatName))
	if providerErr != nil {
		return &CompositeItemInfo{
			Organization:   dataitem.OrganizationSingle,
			DataType:       dataitem.DataTypeTable,
			Format:         formatName,
			EntryPath:      filePath,
			ComponentFiles: []string{filePath},
			SizeBytes:      &fileSize,
			Attributes:     tableFileAttributes(formatName, "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil),
		}, nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, tableFileCatalogPath(engineID, filePath), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read table file %s: %w", filePath, err)
	}
	defer rc.Close()

	tableInfo, err := provider.DescribeTable(ctx, rc, nil)
	if err != nil {
		// Schema 解析失败时返回基础信息，不阻断扫描
		return &CompositeItemInfo{
			Organization:   dataitem.OrganizationSingle,
			DataType:       dataitem.DataTypeTable,
			Format:         formatName,
			EntryPath:      filePath,
			ComponentFiles: []string{filePath},
			SizeBytes:      &fileSize,
			Attributes:     tableFileAttributes(formatName, "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil),
		}, nil
	}

	fieldsData := make([]map[string]interface{}, 0, len(tableInfo.Fields))
	for _, f := range tableInfo.Fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":          f.Name,
			"type":          string(f.Type),
			"original_type": f.OriginalType,
			"nullable":      f.Nullable,
		})
	}

	return &CompositeItemInfo{
		Fields:         tableInfo.Fields,
		Organization:   dataitem.OrganizationSingle,
		DataType:       dataitem.DataTypeTable,
		Format:         formatName,
		EntryPath:      filePath,
		ComponentFiles: []string{filePath},
		SizeBytes:      &fileSize,
		Attributes:     tableFileAttributes(formatName, "single", fieldsData, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, tableInfo),
	}, nil
}

// detectFormat 根据文件列表检测主要格式
func detectFormat(files []plugin.FileEntry) string {
	counts := map[string]int{}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		switch ext {
		case ".parquet":
			counts["parquet"]++
		case ".orc":
			counts["orc"]++
		case ".avro":
			counts["avro"]++
		}
	}
	// 返回数量最多的格式
	best := "parquet"
	bestCount := 0
	for fmt, cnt := range counts {
		if cnt > bestCount {
			best = fmt
			bestCount = cnt
		}
	}
	return best
}

func describeParquetTable(ctx context.Context, rc io.Reader) (*format.TableInfo, error) {
	provider, err := format.GetTableProvider(format.FormatParquet)
	if err != nil {
		return nil, err
	}
	return provider.DescribeTable(ctx, rc, nil)
}

func filePaths(files []plugin.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

func tableFileCatalogPath(engineID uint, path string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindFile,
			Name: path,
		}},
	}
}
