package parquet

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// lakeTableFormats 支持的湖表文件格式
var lakeTableFormats = map[string]bool{
	".parquet": true,
	".orc":     true,
	".avro":    true,
}

// LakeTableDetector 湖表检测器。
// 检测条件：目录树内存在 .parquet/.orc/.avro 文件，且参与候选的文件全部为湖表格式。
type LakeTableDetector struct{}

func init() {
	dataitem.Register(&LakeTableDetector{})
}

func (d *LakeTableDetector) Priority() int {
	return 80
}

func (d *LakeTableDetector) ItemType() string {
	return "lake_table"
}

func (d *LakeTableDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	lakeFileCount := 0
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !lakeTableFormats[ext] {
			return false
		}
		lakeFileCount++
	}
	if lakeFileCount == 0 {
		return false
	}
	if len(subdirs) == 0 {
		return true
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
	if len(filtered) > 0 {
		return filtered
	}
	return lakeTableFiles(files)
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
	if directFiles := directLakeTableFiles(files, dirPath); len(directFiles) > 0 {
		return directFiles[0].Path
	}
	return dirPath
}

func lakeTableCompositionType(files []plugin.FileEntry, subdirs []plugin.DirEntry) dataitem.CompositionType {
	if len(subdirs) > 0 {
		return dataitem.CompositionTypeDirectoryTree
	}
	if len(files) > 1 {
		return dataitem.CompositionTypeMultiFile
	}
	return dataitem.CompositionTypeSingleFile
}

func validateLakeTableFiles(files []plugin.FileEntry, dirPath string) ([]plugin.FileEntry, error) {
	filtered := lakeTableFiles(files)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no lake table files in directory: %s", dirPath)
	}
	if len(filtered) != len(files) {
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
		"format":        formatName,
		"mode":          mode,
		"file_count":    len(files),
		"total_size":    totalSize,
		"physical_path": dirPath,
	}
	if len(fieldsData) > 0 {
		attrs["fields"] = fieldsData
	}
	return attrs
}

func lakeTableMode(subdirs []plugin.DirEntry) string {
	if len(subdirs) > 0 {
		return "directory_tree"
	}
	return "directory"
}

func lakeTableInfoWithoutSchema(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) *dataitem.CompositeItemInfo {
	totalSize := lakeTableSize(files)
	formatName := detectFormat(files)
	return &dataitem.CompositeItemInfo{
		CompositionType: lakeTableCompositionType(files, subdirs),
		DataFamily:      dataitem.DataFamilyTabular,
		Format:          formatName,
		EntryPath:       lakeTableEntryPath(files, subdirs, dirPath),
		ComponentFiles:  filePaths(files),
		SizeBytes:       &totalSize,
		Attributes:      lakeTableAttributes(formatName, lakeTableMode(subdirs), nil, files, dirPath, totalSize),
	}
}

func (d *LakeTableDetector) extractLakeTableInfo(
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

	rc, err := contentReader.OpenContent(ctx, connInfo, parquetFileCatalogPath(engineID, firstParquet.Path), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", firstParquet.Path, err)
	}
	defer rc.Close()

	parser := &Parser{}
	tableInfo, err := parser.ParseTableInfo(ctx, rc, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parquet schema from %s: %w", firstParquet.Path, err)
	}

	totalSize := lakeTableSize(files)
	formatName := detectFormat(files)
	fieldsData := lakeTableFieldAttributes(tableInfo.Fields)
	return &dataitem.CompositeItemInfo{
		Fields:          tableInfo.Fields,
		CompositionType: lakeTableCompositionType(files, subdirs),
		DataFamily:      dataitem.DataFamilyTabular,
		Format:          formatName,
		EntryPath:       lakeTableEntryPath(files, subdirs, dirPath),
		ComponentFiles:  filePaths(files),
		SizeBytes:       &totalSize,
		Attributes:      lakeTableAttributes(formatName, lakeTableMode(subdirs), fieldsData, files, dirPath, totalSize),
	}, nil
}

// ExtractItemInfo 提取湖表元信息（读取第一个 Parquet 文件获取 Schema）
func (d *LakeTableDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*dataitem.CompositeItemInfo, error) {
	return d.extractLakeTableInfo(ctx, contentReader, connInfo, engineID, dirPath, files, nil)
}

func ExtractDirectoryTreeInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
) (*dataitem.CompositeItemInfo, error) {
	detector := &LakeTableDetector{}
	if !detector.Detect(ctx, files, subdirs) {
		return nil, fmt.Errorf("directory is not a lake table dataset: %s", dirPath)
	}
	return detector.extractLakeTableInfo(ctx, contentReader, connInfo, engineID, dirPath, files, subdirs)
}

// ExtractSingleFileInfo 提取单个湖表文件的元信息（模式 B：文件即表）
// 目前只有 .parquet 支持 Schema 解析，.orc/.avro 返回基础信息
func ExtractSingleFileInfo(
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
			CompositionType: dataitem.CompositionTypeSingleFile,
			DataFamily:      dataitem.DataFamilyTabular,
			Format:          format,
			EntryPath:       filePath,
			ComponentFiles:  []string{filePath},
			SizeBytes:       &fileSize,
			Attributes: map[string]interface{}{
				"format":        format,
				"mode":          "file",
				"file_count":    1,
				"total_size":    fileSize,
				"physical_path": filePath,
			},
		}, nil
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, parquetFileCatalogPath(engineID, filePath), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", filePath, err)
	}
	defer rc.Close()

	parser := &Parser{}
	tableInfo, err := parser.ParseTableInfo(ctx, rc, nil)
	if err != nil {
		// Schema 解析失败时返回基础信息，不阻断扫描
		return &dataitem.CompositeItemInfo{
			CompositionType: dataitem.CompositionTypeSingleFile,
			DataFamily:      dataitem.DataFamilyTabular,
			Format:          "parquet",
			EntryPath:       filePath,
			ComponentFiles:  []string{filePath},
			SizeBytes:       &fileSize,
			Attributes: map[string]interface{}{
				"format":        "parquet",
				"mode":          "file",
				"file_count":    1,
				"total_size":    fileSize,
				"physical_path": filePath,
			},
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
		Fields:          tableInfo.Fields,
		CompositionType: dataitem.CompositionTypeSingleFile,
		DataFamily:      dataitem.DataFamilyTabular,
		Format:          "parquet",
		EntryPath:       filePath,
		ComponentFiles:  []string{filePath},
		SizeBytes:       &fileSize,
		Attributes: map[string]interface{}{
			"format":        "parquet",
			"mode":          "file",
			"fields":        fieldsData,
			"file_count":    1,
			"total_size":    fileSize,
			"physical_path": filePath,
		},
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
		CompositionType: dataitem.CompositionTypeDirectoryTree,
		DataFamily:      dataitem.DataFamilyTabular,
		Format:          format,
		EntryPath:       dirPath,
		ComponentFiles:  filePaths(files),
		SizeBytes:       &totalSize,
		Attributes: map[string]interface{}{
			"format":        format,
			"mode":          "directory",
			"file_count":    len(files),
			"total_size":    totalSize,
			"physical_path": dirPath,
		},
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

// ReadFirstParquetPreviewWithProviders 从 CatalogProvider 列目录，并通过 ContentReadableProvider 读取第一个 Parquet。
func ReadFirstParquetPreviewWithProviders(
	ctx context.Context,
	catalogProvider plugin.CatalogProvider,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	offset, limit int64,
) ([]format.FieldInfo, []map[string]interface{}, error) {
	if catalogProvider == nil {
		return nil, nil, fmt.Errorf("catalog provider cannot be nil")
	}
	if contentReader == nil {
		return nil, nil, fmt.Errorf("content readable provider cannot be nil")
	}

	nodes, err := catalogProvider.ListChildren(ctx, connInfo, parquetDirectoryCatalogPath(engineID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		path := catalogNodePath(node)
		if strings.EqualFold(filepath.Ext(path), ".parquet") || strings.EqualFold(filepath.Ext(node.Name), ".parquet") {
			return ReadParquetFilePreviewWithProvider(ctx, contentReader, connInfo, engineID, path, offset, limit)
		}
	}
	return nil, nil, fmt.Errorf("no parquet files found in %s", dirPath)
}

// ReadParquetFilePreviewWithProvider 通过 ContentReadableProvider 读取单个 Parquet 文件预览。
func ReadParquetFilePreviewWithProvider(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	offset, limit int64,
) ([]format.FieldInfo, []map[string]interface{}, error) {
	if contentReader == nil {
		return nil, nil, fmt.Errorf("content readable provider cannot be nil")
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, parquetFileCatalogPath(engineID, filePath), plugin.ReadOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read parquet file: %w", err)
	}
	defer rc.Close()

	return readParquetPreviewFromReader(ctx, rc, offset, limit)
}

func readParquetPreviewFromReader(ctx context.Context, rc io.Reader, offset, limit int64) ([]format.FieldInfo, []map[string]interface{}, error) {
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read parquet data: %w", err)
	}

	parser := &Parser{}

	tableInfo, err := parser.ParseTableInfo(ctx, strings.NewReader(string(data)), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse parquet schema: %w", err)
	}

	rows, err := parser.ReadPreview(ctx, strings.NewReader(string(data)), offset, limit, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read parquet preview: %w", err)
	}

	return tableInfo.Fields, rows, nil
}

func parquetDirectoryCatalogPath(engineID uint, path string) plugin.CatalogPath {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" || trimmed == "." {
		return plugin.CatalogPath{
			Version:  plugin.CatalogPathVersion,
			EngineID: engineID,
			Segments: []plugin.CatalogSegment{{
				Term: plugin.CatalogTermRoot,
				Kind: plugin.CatalogKindRoot,
				Name: "/",
			}},
		}
	}
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindPrefix,
			Name: trimmed,
		}},
	}
}

func parquetFileCatalogPath(engineID uint, path string) plugin.CatalogPath {
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

func catalogNodePath(node plugin.CatalogNode) string {
	if node.Attributes != nil {
		if path, ok := node.Attributes["path"].(string); ok && path != "" {
			return path
		}
	}
	return node.Path.StringPath()
}
