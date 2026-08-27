package metaenrich

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
)

func resolveTableFileDataItem(dirPath string, files []metaitem.StorageFileRef) (*dataitem.ResolvedItem, bool, error) {
	resolved, err := dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind:  dataitem.ScopeKindDirectory,
		ScopePath:  dirPath,
		Candidates: tableFileCandidates(files),
		Options: dataitem.ResolveOptions{
			AllowWholeScope: true,
		},
	})
	if err != nil {
		return nil, false, err
	}
	if resolved == nil || len(resolved.Items) != 1 {
		return nil, false, nil
	}
	item := resolved.Items[0]
	if item.DataType != datatype.Table {
		return nil, false, nil
	}
	return &item, item.Layout == format.LayoutWhole && resolved.Exclusive, nil
}

func tableFileCandidates(files []metaitem.StorageFileRef) []dataitem.Candidate {
	candidates := make([]dataitem.Candidate, 0, len(files))
	for _, file := range files {
		size := file.Size
		candidates = append(candidates, dataitem.Candidate{
			Path:        file.Path,
			Name:        file.Name,
			ContentType: file.ContentType,
			SizeBytes:   &size,
		})
	}
	return candidates
}

func inferScopePath(files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef) string {
	for _, subdir := range subdirs {
		if path := strings.Trim(subdir.Path, "/"); path != "" {
			parent := strings.Trim(filepath.ToSlash(filepath.Dir(path)), "/")
			if parent != "." {
				return parent
			}
		}
	}
	if len(files) == 0 {
		return ""
	}
	parent := strings.Trim(filepath.ToSlash(filepath.Dir(strings.Trim(files[0].Path, "/"))), "/")
	if parent == "." {
		return ""
	}
	for _, file := range files[1:] {
		current := strings.Trim(filepath.ToSlash(filepath.Dir(strings.Trim(file.Path, "/"))), "/")
		for parent != "" && current != parent && !strings.HasPrefix(current, parent+"/") {
			parent = strings.Trim(filepath.ToSlash(filepath.Dir(parent)), "/")
			if parent == "." {
				parent = ""
			}
		}
	}
	return parent
}

func tableFiles(files []metaitem.StorageFileRef) []metaitem.StorageFileRef {
	filtered := make([]metaitem.StorageFileRef, 0, len(files))
	for _, f := range files {
		if hasSingleTableProvider(fileFormatName(f.Name)) {
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

func directTableFiles(files []metaitem.StorageFileRef, dirPath string) []metaitem.StorageFileRef {
	trimmedDir := strings.Trim(dirPath, "/")
	filtered := make([]metaitem.StorageFileRef, 0, len(files))
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

func firstReadableTableFile(files []metaitem.StorageFileRef, dirPath string) *metaitem.StorageFileRef {
	candidates := directTableFiles(files, dirPath)
	for i := range candidates {
		if hasSingleTableProvider(fileFormatName(candidates[i].Name)) {
			return &candidates[i]
		}
	}
	for i := range files {
		if hasSingleTableProvider(fileFormatName(files[i].Name)) {
			return &files[i]
		}
	}
	return nil
}

func tableFilePrimaryContentPath(files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef, dirPath string) string {
	if item, _, err := resolveTableFileDataItem(dirPath, files); err == nil && item != nil && item.PrimaryContentPath != "" {
		return item.PrimaryContentPath
	}
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

func tableFileLayout(files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef, dirPath string) format.Layout {
	if item, ok, err := resolveTableFileDataItem(dirPath, files); err == nil && item != nil && ok {
		return item.Layout
	}
	if len(subdirs) > 0 {
		return format.LayoutWhole
	}
	if len(directTableFiles(files, dirPath)) > 1 || len(tableFiles(files)) > 1 {
		return format.LayoutWhole
	}
	return format.LayoutSingle
}

func validateTableFiles(files []metaitem.StorageFileRef, dirPath string) ([]metaitem.StorageFileRef, error) {
	filtered := tableFiles(files)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no table file files in directory: %s", dirPath)
	}
	if _, ok, err := resolveTableFileDataItem(dirPath, files); err != nil || !ok {
		return nil, fmt.Errorf("directory contains files outside supported scope table formats: %s", dirPath)
	}
	return filtered, nil
}

func tableFileSize(files []metaitem.StorageFileRef) int64 {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	return totalSize
}

func tableFieldsFromDescribeResult(tableInfo *format.TableDescribeResult) []datatype.FieldInfo {
	if tableInfo == nil || tableInfo.Table == nil {
		return nil
	}
	return append([]datatype.FieldInfo(nil), tableInfo.Table.Fields...)
}

func tableFileAttributes(formatName string, mode string, files []metaitem.StorageFileRef, dirPath string, totalSize int64, tableInfo *format.TableDescribeResult, includeAccessIndex bool) map[string]interface{} {
	input := metaattr.TableFileAttributesInput{
		FormatName:         formatName,
		Mode:               mode,
		FileCount:          len(files),
		PhysicalPath:       dirPath,
		TotalSize:          totalSize,
		IncludeAccessIndex: includeAccessIndex,
	}
	if tableInfo != nil {
		input.Table = tableInfo.Table
		input.FormatInfo = tableInfo.FormatInfo
		input.Spatial = tableInfo.Spatial
		input.AccessIndex = tableInfo.AccessIndex
	}
	return metaattr.TableFileAttributes(input)
}

func tableFileMode(files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef, dirPath string) string {
	if tableFileLayout(files, subdirs, dirPath) == format.LayoutWhole {
		return "whole"
	}
	return "single"
}

func tableDatasetInfoWithoutSchema(files []metaitem.StorageFileRef, subdirs []metaitem.StorageDirectoryRef, dirPath string) *metaitem.CompositeItemInfo {
	resolved, _, _ := resolveTableFileDataItem(dirPath, files)
	totalSize := tableFileSize(files)
	formatName := detectFormat(files)
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
	return &metaitem.CompositeItemInfo{
		Layout:             layout,
		DataType:           datatype.Table,
		Format:             formatName,
		PrimaryContentPath: primaryContentPath,
		ScopePath:          scopePath,
		RefFiles:           filePaths(files),
		SizeBytes:          &totalSize,
		Attributes:         tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), files, dirPath, totalSize, nil, false),
	}
}

func describeTableFileScope(ctx context.Context, formatName string, reader contentio.Reader, dirPath string) (*format.TableDescribeResult, error) {
	provider, err := format.GetScopeTableInfoProvider(format.NormalizeFormat(formatName))
	if err != nil {
		return nil, err
	}
	return provider.DescribeTableScope(ctx, reader, contentio.NewRef(dirPath, contentio.RoleScope), nil)
}

// detectFormat 根据文件列表检测主要格式
func detectFormat(files []metaitem.StorageFileRef) string {
	counts := map[string]int{}
	for _, f := range files {
		formatName := fileFormatName(f.Name)
		if formatName == "" || formatName == string(format.FormatUnknown) || !hasSingleTableProvider(formatName) {
			continue
		}
		counts[formatName]++
	}
	// 返回数量最多的格式
	best := string(format.FormatUnknown)
	bestCount := 0
	for fmt, cnt := range counts {
		if cnt > bestCount || (cnt == bestCount && fmt < best) {
			best = fmt
			bestCount = cnt
		}
	}
	return best
}

func fileFormatName(fileName string) string {
	return string(format.NormalizeFormat(fileName))
}

func hasSingleTableProvider(formatName string) bool {
	formatType := format.NormalizeFormat(formatName)
	if formatType == format.FormatUnknown {
		return false
	}
	_, err := format.GetTableInfoProvider(formatType)
	return err == nil
}

func hasScopeTableProvider(formatName string) bool {
	formatType := format.NormalizeFormat(formatName)
	if formatType == format.FormatUnknown {
		return false
	}
	_, err := format.GetScopeTableInfoProvider(formatType)
	return err == nil
}

func describeTableFile(ctx context.Context, formatName string, rc io.Reader) (*format.TableDescribeResult, error) {
	provider, err := format.GetTableInfoProvider(format.NormalizeFormat(formatName))
	if err != nil {
		return nil, err
	}
	return provider.DescribeTable(ctx, rc, nil)
}

func filePaths(files []metaitem.StorageFileRef) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

func resolveTableFileCatalogPath(engineID uint, path string, catalogPathFor func(path string) plugin.EngineCatalogPath) plugin.EngineCatalogPath {
	if catalogPathFor != nil {
		return catalogPathFor(path)
	}
	return plugin.FileItemPath(engineID, path)
}

func firstCatalogPathResolver(resolvers []func(path string) plugin.EngineCatalogPath) func(path string) plugin.EngineCatalogPath {
	if len(resolvers) == 0 {
		return nil
	}
	return resolvers[0]
}
