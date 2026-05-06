package metaitem

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonParquet "github.com/addp/common/format/parquet"
)

// lakeTableFormats 支持的湖表文件格式
var lakeTableFormats = map[string]bool{
	".parquet": true,
	".orc":     true,
	".avro":    true,
}

var lakeTableAuxiliaryFileNames = map[string]bool{
	"_success":         true,
	"_metadata":        true,
	"_common_metadata": true,
}

var lakeTableItemRules = []dataitem.FormatRule{
	{
		Format:       "parquet",
		DataType:     dataitem.DataTypeTable,
		ItemType:     "lake_table",
		Organization: dataitem.OrganizationSingle,
		Priority:     40,
		Entry: dataitem.EntryRule{
			Extensions: []string{".parquet", ".orc", ".avro"},
		},
	},
	{
		Format:       "parquet",
		DataType:     dataitem.DataTypeTable,
		ItemType:     "lake_table",
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

// LakeTableDetector 湖表检测器。
// 检测条件：目录树内存在 .parquet/.orc/.avro 文件，且参与候选的文件全部为湖表数据文件或常见辅助文件。
type lakeTableItemDetector struct{}

func (d *lakeTableItemDetector) Priority() int {
	return 80
}

func (d *lakeTableItemDetector) Rules() []dataitem.FormatRule {
	return lakeTableItemRules
}

func (d *lakeTableItemDetector) ItemType() string {
	return "lake_table"
}

func (d *lakeTableItemDetector) ResolveItems(
	ctx context.Context,
	input dataitem.DirectoryResolveInput,
) (*dataitem.DetectionResult, error) {
	files := input.Files
	subdirs := input.Subdirs
	if len(input.RecursiveFiles) > 0 {
		files = input.RecursiveFiles
		subdirs = input.RecursiveSubdirs
	}
	if !d.Detect(ctx, files, subdirs) {
		return nil, nil
	}
	info, err := d.extractLakeTableInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.DirPath, files, subdirs)
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
	item := &dataitem.DetectedItem{
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
	claims := dataitem.ResourceClaimSet{}
	for _, path := range item.ComponentFiles {
		claims[path] = true
	}
	return &dataitem.DetectionResult{
		Items:     []*dataitem.DetectedItem{item},
		Claims:    claims,
		Exclusive: item.Organization == dataitem.OrganizationWhole,
	}, nil
}

func (d *lakeTableItemDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	lakeFileCount := 0
	auxiliaryFileCount := 0
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !lakeTableFormats[ext] {
			if isLakeTableAuxiliaryFile(f.Name) {
				auxiliaryFileCount++
				continue
			}
			return false
		}
		lakeFileCount++
	}
	if lakeFileCount == 0 {
		return false
	}
	if len(subdirs) == 0 {
		return lakeFileCount == 1 || auxiliaryFileCount > 0 || hasPartLikeLakeFiles(files)
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

func hasPartLikeLakeFiles(files []plugin.FileEntry) bool {
	lakeFileCount := 0
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !lakeTableFormats[ext] {
			continue
		}
		lakeFileCount++
		name := strings.ToLower(strings.TrimSpace(filepath.Base(file.Name)))
		if !(strings.HasPrefix(name, "part-") || strings.HasPrefix(name, "part_")) {
			return false
		}
	}
	return lakeFileCount > 1
}

func lakeTableFiles(files []plugin.FileEntry) []plugin.FileEntry {
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if lakeTableFormats[ext] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func isLakeTableAuxiliaryFile(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
	if normalized == "" {
		return false
	}
	if lakeTableAuxiliaryFileNames[normalized] {
		return true
	}
	if strings.HasPrefix(normalized, ".") && strings.Contains(normalized, ".crc") {
		return true
	}
	return strings.HasSuffix(normalized, ".crc")
}

func directLakeTableFiles(files []plugin.FileEntry, dirPath string) []plugin.FileEntry {
	trimmedDir := strings.Trim(dirPath, "/")
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, f := range lakeTableFiles(files) {
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
	candidates := directLakeTableFiles(files, dirPath)
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

func lakeTableEntryPath(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if len(subdirs) > 0 {
		return dirPath
	}
	if directFiles := directLakeTableFiles(files, dirPath); len(directFiles) > 1 {
		return dirPath
	} else if len(directFiles) == 1 {
		return directFiles[0].Path
	}
	if lakeFiles := lakeTableFiles(files); len(lakeFiles) == 1 {
		return lakeFiles[0].Path
	}
	return dirPath
}

func lakeTableOrganization(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) dataitem.Organization {
	if len(subdirs) > 0 {
		return dataitem.OrganizationWhole
	}
	if len(directLakeTableFiles(files, dirPath)) > 1 || len(lakeTableFiles(files)) > 1 {
		return dataitem.OrganizationWhole
	}
	return dataitem.OrganizationSingle
}

func validateLakeTableFiles(files []plugin.FileEntry, dirPath string) ([]plugin.FileEntry, error) {
	filtered := lakeTableFiles(files)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no lake table files in directory: %s", dirPath)
	}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if lakeTableFormats[ext] || isLakeTableAuxiliaryFile(f.Name) {
			continue
		}
		return nil, fmt.Errorf("directory contains non-lake-table files: %s", dirPath)
	}
	return filtered, nil
}

func lakeTableSize(files []plugin.FileEntry) int64 {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	return totalSize
}

func lakeTableFieldAttributes(fields []format.FieldInfo) []map[string]interface{} {
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

func lakeTableAttributes(formatName string, mode string, fieldsData []map[string]interface{}, files []plugin.FileEntry, dirPath string, totalSize int64) map[string]interface{} {
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
		attrs["type_info"] = map[string]interface{}{
			"table": map[string]interface{}{
				"fields": fieldsData,
			},
		}
	}
	return attrs
}

func lakeTableMode(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if lakeTableOrganization(files, subdirs, dirPath) == dataitem.OrganizationWhole {
		return "whole"
	}
	return "single"
}

func lakeTableInfoWithoutSchema(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) *dataitem.CompositeItemInfo {
	totalSize := lakeTableSize(files)
	formatName := detectFormat(files)
	return &dataitem.CompositeItemInfo{
		Organization:   lakeTableOrganization(files, subdirs, dirPath),
		DataType:       dataitem.DataTypeTable,
		Format:         formatName,
		EntryPath:      lakeTableEntryPath(files, subdirs, dirPath),
		ComponentFiles: filePaths(files),
		SizeBytes:      &totalSize,
		Attributes:     lakeTableAttributes(formatName, lakeTableMode(files, subdirs, dirPath), nil, files, dirPath, totalSize),
	}
}

func (d *lakeTableItemDetector) extractLakeTableInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*dataitem.CompositeItemInfo, error) {
	files, err := validateLakeTableFiles(files, dirPath)
	if err != nil {
		return nil, err
	}

	firstParquet := firstReadableParquetFile(files, dirPath)
	if firstParquet == nil {
		return lakeTableInfoWithoutSchema(files, subdirs, dirPath), nil
	}
	if contentReader == nil {
		return lakeTableInfoWithoutSchema(files, subdirs, dirPath), nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, lakeTableParquetFileCatalogPath(engineID, firstParquet.Path), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", firstParquet.Path, err)
	}
	defer rc.Close()

	parser := &commonParquet.Parser{}
	tableInfo, err := parser.ParseTableInfo(ctx, rc, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parquet schema from %s: %w", firstParquet.Path, err)
	}

	totalSize := lakeTableSize(files)
	formatName := detectFormat(files)
	fieldsData := lakeTableFieldAttributes(tableInfo.Fields)
	return &dataitem.CompositeItemInfo{
		Fields:         tableInfo.Fields,
		Organization:   lakeTableOrganization(files, subdirs, dirPath),
		DataType:       dataitem.DataTypeTable,
		Format:         formatName,
		EntryPath:      lakeTableEntryPath(files, subdirs, dirPath),
		ComponentFiles: filePaths(files),
		SizeBytes:      &totalSize,
		Attributes:     lakeTableAttributes(formatName, lakeTableMode(files, subdirs, dirPath), fieldsData, files, dirPath, totalSize),
	}, nil
}

// ExtractItemInfo 提取湖表元信息（读取第一个 Parquet 文件获取 Schema）
func (d *lakeTableItemDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*dataitem.CompositeItemInfo, error) {
	return d.extractLakeTableInfo(ctx, contentReader, connInfo, engineID, dirPath, files, nil)
}

func extractLakeTableWholeScopeInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*dataitem.CompositeItemInfo, error) {
	detector := &lakeTableItemDetector{}
	if !detector.Detect(ctx, files, subdirs) {
		return nil, fmt.Errorf("directory is not a lake table dataset: %s", dirPath)
	}
	return detector.extractLakeTableInfo(ctx, contentReader, connInfo, engineID, dirPath, files, subdirs)
}

// ExtractLakeTableSingleFileInfo 提取单个湖表文件的元信息（模式 B：文件即表）
// 目前只有 .parquet 支持 Schema 解析，.orc/.avro 返回基础信息
func ExtractLakeTableSingleFileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
) (*dataitem.CompositeItemInfo, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	format := ext[1:] // 去掉点号，如 "parquet"

	// 非 parquet 格式暂不解析 Schema
	if ext != ".parquet" {
		return &dataitem.CompositeItemInfo{
			Organization:   dataitem.OrganizationSingle,
			DataType:       dataitem.DataTypeTable,
			Format:         format,
			EntryPath:      filePath,
			ComponentFiles: []string{filePath},
			SizeBytes:      &fileSize,
			Attributes:     lakeTableAttributes(format, "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize),
		}, nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, lakeTableParquetFileCatalogPath(engineID, filePath), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", filePath, err)
	}
	defer rc.Close()

	parser := &commonParquet.Parser{}
	tableInfo, err := parser.ParseTableInfo(ctx, rc, nil)
	if err != nil {
		// Schema 解析失败时返回基础信息，不阻断扫描
		return &dataitem.CompositeItemInfo{
			Organization:   dataitem.OrganizationSingle,
			DataType:       dataitem.DataTypeTable,
			Format:         "parquet",
			EntryPath:      filePath,
			ComponentFiles: []string{filePath},
			SizeBytes:      &fileSize,
			Attributes:     lakeTableAttributes("parquet", "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize),
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

	return &dataitem.CompositeItemInfo{
		Fields:         tableInfo.Fields,
		Organization:   dataitem.OrganizationSingle,
		DataType:       dataitem.DataTypeTable,
		Format:         "parquet",
		EntryPath:      filePath,
		ComponentFiles: []string{filePath},
		SizeBytes:      &fileSize,
		Attributes:     lakeTableAttributes("parquet", "single", fieldsData, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize),
	}, nil
}

// buildBasicInfo 构建基础信息（无 Schema）
func buildBasicInfo(files []plugin.FileEntry, dirPath string) *dataitem.CompositeItemInfo {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	format := detectFormat(files)
	return &dataitem.CompositeItemInfo{
		Organization:   dataitem.OrganizationWhole,
		DataType:       dataitem.DataTypeTable,
		Format:         format,
		EntryPath:      dirPath,
		ComponentFiles: filePaths(files),
		SizeBytes:      &totalSize,
		Attributes:     lakeTableAttributes(format, "whole", nil, files, dirPath, totalSize),
	}
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

func filePaths(files []plugin.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

func lakeTableParquetFileCatalogPath(engineID uint, path string) plugin.CatalogPath {
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
