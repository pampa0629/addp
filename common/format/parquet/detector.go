package parquet

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/format/detector"
)

// lakeTableFormats 支持的湖表文件格式
var lakeTableFormats = map[string]bool{
	".parquet": true,
	".orc":     true,
	".avro":    true,
}

// LakeTableDetector 湖表检测器
// 检测条件：目录下的直接子文件全部为 .parquet/.orc/.avro
// 优先级：80
type LakeTableDetector struct{}

func init() {
	detector.Register(&LakeTableDetector{})
}

func (d *LakeTableDetector) Priority() int {
	return 80
}

func (d *LakeTableDetector) ItemType() string {
	return "lake_table"
}

// Detect 检测目录是否为湖表
// 条件：有文件 && 无子目录 && 所有文件都是支持的格式
func (d *LakeTableDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	if len(files) == 0 {
		return false
	}
	// 有子目录则不是湖表（第一阶段不处理嵌套分区）
	if len(subdirs) > 0 {
		return false
	}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if !lakeTableFormats[ext] {
			return false
		}
	}
	return true
}

// ExtractItemInfo 提取湖表元信息（读取第一个 Parquet 文件获取 Schema）
func (d *LakeTableDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*detector.CompositeItemInfo, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files in directory: %s", dirPath)
	}

	// 找第一个 parquet 文件读取 Schema
	var firstParquet *plugin.FileEntry
	for i := range files {
		ext := strings.ToLower(filepath.Ext(files[i].Name))
		if ext == ".parquet" {
			firstParquet = &files[i]
			break
		}
	}
	if firstParquet == nil {
		// 没有 parquet 文件，返回空 Schema（orc/avro 暂不解析）
		return buildBasicInfo(files, dirPath), nil
	}

	// 读取文件内容
	rc, err := contentReader.OpenContent(ctx, connInfo, parquetFileCatalogPath(engineID, firstParquet.Path), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read parquet file %s: %w", firstParquet.Path, err)
	}
	defer rc.Close()

	// 解析 Schema
	parser := &Parser{}
	tableInfo, err := parser.ParseTableInfo(ctx, rc, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse parquet schema from %s: %w", firstParquet.Path, err)
	}

	// 计算总文件大小
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	// 构建 FieldInfo 列表（用于 Attributes.fields）
	fieldsData := make([]map[string]interface{}, 0, len(tableInfo.Fields))
	for _, f := range tableInfo.Fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":          f.Name,
			"type":          string(f.Type),
			"original_type": f.OriginalType,
			"nullable":      f.Nullable,
		})
	}

	return &detector.CompositeItemInfo{
		Fields: tableInfo.Fields,
		Attributes: map[string]interface{}{
			"format":        detectFormat(files),
			"mode":          "directory",
			"fields":        fieldsData,
			"file_count":    len(files),
			"total_size":    totalSize,
			"physical_path": dirPath,
		},
	}, nil
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
) (*detector.CompositeItemInfo, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	format := ext[1:] // 去掉点号，如 "parquet"

	// 非 parquet 格式暂不解析 Schema
	if ext != ".parquet" {
		return &detector.CompositeItemInfo{
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
		return &detector.CompositeItemInfo{
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

	return &detector.CompositeItemInfo{
		Fields: tableInfo.Fields,
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
func buildBasicInfo(files []plugin.FileEntry, dirPath string) *detector.CompositeItemInfo {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	return &detector.CompositeItemInfo{
		Attributes: map[string]interface{}{
			"format":        detectFormat(files),
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
