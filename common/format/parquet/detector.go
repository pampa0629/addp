package parquet

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
)

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
		if path := commonJSON.String(node.Attributes, "storage", "path"); path != "" {
			return path
		}
	}
	return node.Path.StringPath()
}
