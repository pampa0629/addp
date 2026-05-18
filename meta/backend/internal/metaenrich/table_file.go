package metaenrich

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
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

func init() {
	metaitem.RegisterResolver(&tableFileItemResolver{})
}

func tableFileItemRules() []dataitem.FormatRule {
	formats := tableFileFormatsByExtension()
	extensions := make([]string, 0, len(formats))
	for ext := range formats {
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	formatName := preferredTableFileFormat(formats)
	return []dataitem.FormatRule{
		{
			Format:       formatName,
			DataType:     dataitem.DataTypeTable,
			Organization: dataitem.OrganizationSingle,
			Priority:     40,
			Entry: dataitem.EntryRule{
				Extensions: extensions,
			},
		},
		{
			Format:       formatName,
			DataType:     dataitem.DataTypeTable,
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
}

func tableFileFormatsByExtension() map[string]string {
	formats := map[string]string{}
	for _, capability := range format.ListFormatCapabilities() {
		if capability.DataType != format.FormatDataTypeTable {
			continue
		}
		if !containsLayout(capability.Layouts, format.FormatLayoutWhole) {
			continue
		}
		for _, ext := range capability.Extensions {
			normalized := strings.ToLower(strings.TrimSpace(ext))
			if normalized == "" {
				continue
			}
			if !strings.HasPrefix(normalized, ".") {
				normalized = "." + normalized
			}
			formats[normalized] = string(capability.Format)
		}
	}
	return formats
}

func preferredTableFileFormat(formats map[string]string) string {
	if formatName := formats[".parquet"]; formatName != "" {
		return formatName
	}
	values := make([]string, 0, len(formats))
	seen := map[string]bool{}
	for _, formatName := range formats {
		if seen[formatName] {
			continue
		}
		seen[formatName] = true
		values = append(values, formatName)
	}
	sort.Strings(values)
	if len(values) == 0 {
		return string(format.FormatParquet)
	}
	return values[0]
}

func containsLayout(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (d *tableFileItemResolver) Priority() int {
	return 80
}

func (d *tableFileItemResolver) Rules() []dataitem.FormatRule {
	return tableFileItemRules()
}

func resolveTableFileDataItem(dirPath string, files []plugin.FileEntry) (*dataitem.ResolvedItem, bool, error) {
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
	if item.DataType != dataitem.DataTypeTable {
		return nil, false, nil
	}
	return &item, item.Organization == dataitem.OrganizationWhole && resolved.Exclusive, nil
}

func tableFileCandidates(files []plugin.FileEntry) []dataitem.Candidate {
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
	info, err := d.extractTableFileInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.DirPath, files, subdirs, input.CatalogPathFor)
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
			Organization: info.Organization,
			DataType:     info.DataType,
			Format:       info.Format,
			EntryPath:    info.EntryPath,
			RefList:      metaitem.ItemRefsFromPaths(info.RefFiles),
			SizeBytes:    &totalSize,
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
		Exclusive: item.Organization == dataitem.OrganizationWhole,
	}, nil
}

func (d *tableFileItemResolver) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	_, ok, err := resolveTableFileDataItem(inferScopePath(files, subdirs), files)
	return err == nil && ok
}

func inferScopePath(files []plugin.FileEntry, subdirs []plugin.DirEntry) string {
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

func tableFiles(files []plugin.FileEntry) []plugin.FileEntry {
	formats := tableFileFormatsByExtension()
	filtered := make([]plugin.FileEntry, 0, len(files))
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if formats[ext] != "" {
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

func firstReadableTableFile(files []plugin.FileEntry, dirPath string) *plugin.FileEntry {
	candidates := directTableFiles(files, dirPath)
	for i := range candidates {
		if hasTableProvider(fileFormatName(candidates[i].Name)) {
			return &candidates[i]
		}
	}
	for i := range files {
		if hasTableProvider(fileFormatName(files[i].Name)) {
			return &files[i]
		}
	}
	return nil
}

func tableFileEntryPath(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if item, _, err := resolveTableFileDataItem(dirPath, files); err == nil && item != nil && item.EntryPath != "" {
		return item.EntryPath
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

func tableFileOrganization(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) dataitem.Organization {
	if item, ok, err := resolveTableFileDataItem(dirPath, files); err == nil && item != nil && ok {
		return item.Organization
	}
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
	if _, ok, err := resolveTableFileDataItem(dirPath, files); err != nil || !ok {
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
			"name":     f.Name,
			"type":     string(f.Type),
			"nullable": f.Nullable,
		})
	}
	return fieldsData
}

func tableFileAttributes(formatName string, mode string, fieldsData []map[string]interface{}, files []plugin.FileEntry, dirPath string, totalSize int64, tableInfo *format.TableInfo, includeContentIndex bool) map[string]interface{} {
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
		if formatAttrs := formatAttributesFromTableInfo(formatName, tableInfo); len(formatAttrs) > 0 {
			attrs["format_info"].(map[string]interface{})[formatName] = mergeInterfaceMaps(
				attrs["format_info"].(map[string]interface{})[formatName],
				formatAttrs,
			)
		}
		if spatialInfo := tableInfo.GetSpatialInfo(); spatialInfo != nil {
			spatialAttrs := map[string]interface{}{
				"geometry_columns": []map[string]interface{}{{
					"name":          spatialInfo.GeometryColumn,
					"geometry_type": spatialInfo.GeometryType,
					"srid":          spatialInfo.SRID,
				}},
				"primary_geometry_column": spatialInfo.GeometryColumn,
				"has_spatial_index":       spatialInfo.HasSpatialIndex,
			}
			if spatialInfo.BoundingBox != nil {
				bbox := *spatialInfo.BoundingBox
				spatialAttrs["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
			}
			attrs["capabilities"] = map[string]interface{}{
				"spatial": spatialAttrs,
			}
		}
		if indexInfo := tableInfo.GetContentIndexInfo(); includeContentIndex && indexInfo != nil && indexInfo.Table != nil {
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

type formatAttributesProvider interface {
	FormatAttributes() map[string]interface{}
}

func formatAttributesFromTableInfo(formatName string, tableInfo *format.TableInfo) map[string]interface{} {
	if tableInfo == nil || len(tableInfo.FormatInfo) == 0 {
		return nil
	}
	if provider, ok := tableInfo.FormatInfo[formatName].(formatAttributesProvider); ok {
		return provider.FormatAttributes()
	}
	if attrs, ok := tableInfo.FormatInfo[formatName].(map[string]interface{}); ok {
		return attrs
	}
	return nil
}

func mergeInterfaceMaps(existing interface{}, additions map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	if current, ok := existing.(map[string]interface{}); ok {
		for k, v := range current {
			merged[k] = v
		}
	}
	for k, v := range additions {
		merged[k] = v
	}
	return merged
}

func tableFileMode(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) string {
	if tableFileOrganization(files, subdirs, dirPath) == dataitem.OrganizationWhole {
		return "whole"
	}
	return "single"
}

func tableFileInfoWithoutSchema(files []plugin.FileEntry, subdirs []plugin.DirEntry, dirPath string) *metaitem.CompositeItemInfo {
	resolved, _, _ := resolveTableFileDataItem(dirPath, files)
	totalSize := tableFileSize(files)
	formatName := detectFormat(files)
	organization := tableFileOrganization(files, subdirs, dirPath)
	entryPath := tableFileEntryPath(files, subdirs, dirPath)
	if resolved != nil {
		formatName = resolved.Format
		organization = resolved.Organization
		entryPath = resolved.EntryPath
		if resolved.SizeBytes != nil {
			totalSize = *resolved.SizeBytes
		}
	}
	return &metaitem.CompositeItemInfo{
		Organization: organization,
		DataType:     dataitem.DataTypeTable,
		Format:       formatName,
		EntryPath:    entryPath,
		RefFiles:     filePaths(files),
		SizeBytes:    &totalSize,
		Attributes:   tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), nil, files, dirPath, totalSize, nil, false),
	}
}

type tableFileContentReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.CatalogPath
	files          []plugin.FileEntry
	subdirs        []plugin.DirEntry
}

func (r tableFileContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r.contentReader == nil {
		return nil, contentio.ErrContentNotFound
	}
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveTableFileCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r tableFileContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Metadata, error) {
	for _, file := range r.files {
		if strings.Trim(file.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Metadata{
				Ref:    contentio.NewRef(file.Path, contentio.RoleMain),
				Size:   file.Size,
				Exists: true,
			}, nil
		}
	}
	for _, dir := range r.subdirs {
		if strings.Trim(dir.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Metadata{
				Ref:    contentio.NewRef(dir.Path, contentio.RoleScope),
				Exists: true,
			}, nil
		}
	}
	return &contentio.Metadata{Ref: ref, Exists: false}, nil
}

func (r tableFileContentReader) List(_ context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	scopePath := strings.Trim(scope.Path, "/")
	refs := make([]contentio.Ref, 0)
	for _, file := range r.files {
		path := strings.Trim(file.Path, "/")
		if !isImmediateChildPath(scopePath, path) {
			continue
		}
		refs = append(refs, contentio.NewRef(path, contentio.RoleMain))
	}
	for _, dir := range r.subdirs {
		path := strings.Trim(dir.Path, "/")
		if !isImmediateChildPath(scopePath, path) {
			continue
		}
		refs = append(refs, contentio.NewRef(path, contentio.RoleScope))
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	if len(refs) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	return refs, nil
}

func isImmediateChildPath(scopePath string, childPath string) bool {
	if scopePath == "" {
		return childPath != "" && !strings.Contains(childPath, "/")
	}
	if !strings.HasPrefix(childPath, scopePath+"/") {
		return false
	}
	rest := strings.TrimPrefix(childPath, scopePath+"/")
	return rest != "" && !strings.Contains(rest, "/")
}

func (d *tableFileItemResolver) extractTableFileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
	catalogPathFor func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	files, err := validateTableFiles(files, dirPath)
	if err != nil {
		return nil, err
	}

	firstReadableFile := firstReadableTableFile(files, dirPath)
	if firstReadableFile == nil {
		return tableFileInfoWithoutSchema(files, subdirs, dirPath), nil
	}
	if contentReader == nil {
		return tableFileInfoWithoutSchema(files, subdirs, dirPath), nil
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
			return nil, fmt.Errorf("failed to parse table schema from %s: %w", firstReadableFile.Path, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("failed to close table file %s: %w", firstReadableFile.Path, closeErr)
		}
	}

	resolved, _, _ := resolveTableFileDataItem(dirPath, files)
	totalSize := tableFileSize(files)
	organization := tableFileOrganization(files, subdirs, dirPath)
	entryPath := tableFileEntryPath(files, subdirs, dirPath)
	if resolved != nil {
		formatName = resolved.Format
		organization = resolved.Organization
		entryPath = resolved.EntryPath
		if resolved.SizeBytes != nil {
			totalSize = *resolved.SizeBytes
		}
	}
	fieldsData := tableFileFieldAttributes(tableInfo.Fields)
	return &metaitem.CompositeItemInfo{
		Fields:       tableInfo.Fields,
		Organization: organization,
		DataType:     dataitem.DataTypeTable,
		Format:       formatName,
		EntryPath:    entryPath,
		RefFiles:     filePaths(files),
		SizeBytes:    &totalSize,
		Attributes:   tableFileAttributes(formatName, tableFileMode(files, subdirs, dirPath), fieldsData, files, dirPath, totalSize, tableInfo, false),
	}, nil
}

func describeTableFileScope(ctx context.Context, formatName string, reader contentio.Reader, dirPath string) (*format.TableInfo, error) {
	provider, err := format.GetTableProvider(format.FormatType(formatName))
	if err != nil {
		return nil, err
	}
	scopeProvider, ok := provider.(format.ScopeTableProvider)
	if !ok {
		return nil, fmt.Errorf("%s provider does not implement scope table provider", formatName)
	}
	return scopeProvider.DescribeTableScope(ctx, reader, contentio.NewRef(dirPath, contentio.RoleScope), nil)
}

func extractTableFileWholeScopeInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
	subdirs []plugin.DirEntry,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	resolver := &tableFileItemResolver{}
	if !resolver.Detect(ctx, files, subdirs) {
		return nil, fmt.Errorf("directory is not a table file dataset: %s", dirPath)
	}
	return resolver.extractTableFileInfo(ctx, contentReader, connInfo, engineID, dirPath, files, subdirs, firstCatalogPathResolver(catalogPathFor))
}

// ExtractTableFileSingleFileInfo 提取单个表格文件的元信息（模式 B：文件即表）。
func ExtractTableFileSingleFileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
	includeContentIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	formatName := fileFormatName(filePath)

	provider, providerErr := format.GetTableProvider(format.FormatType(formatName))
	if providerErr != nil {
		return &metaitem.CompositeItemInfo{
			Organization: dataitem.OrganizationSingle,
			DataType:     dataitem.DataTypeTable,
			Format:       formatName,
			EntryPath:    filePath,
			RefFiles:     []string{filePath},
			SizeBytes:    &fileSize,
			Attributes:   tableFileAttributes(formatName, "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil, false),
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
			Organization: dataitem.OrganizationSingle,
			DataType:     dataitem.DataTypeTable,
			Format:       formatName,
			EntryPath:    filePath,
			RefFiles:     []string{filePath},
			SizeBytes:    &fileSize,
			Attributes:   tableFileAttributes(formatName, "single", nil, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, nil, false),
		}, nil
	}

	fieldsData := make([]map[string]interface{}, 0, len(tableInfo.Fields))
	for _, f := range tableInfo.Fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":     f.Name,
			"type":     string(f.Type),
			"nullable": f.Nullable,
		})
	}

	return &metaitem.CompositeItemInfo{
		Fields:       tableInfo.Fields,
		Organization: dataitem.OrganizationSingle,
		DataType:     dataitem.DataTypeTable,
		Format:       formatName,
		EntryPath:    filePath,
		RefFiles:     []string{filePath},
		SizeBytes:    &fileSize,
		Attributes:   tableFileAttributes(formatName, "single", fieldsData, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, tableInfo, includeContentIndex && format.SupportsContentIndex(format.FormatType(formatName))),
	}, nil
}

// ExtractTableFileSingleFileInfoStrict 仅在格式 provider 成功解析出表结构时返回结果。
// 适用于 JSON 这类默认是 document、只有特定内容结构才应升级为 table 的格式。
func ExtractTableFileSingleFileInfoStrict(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	filePath string,
	fileSize int64,
	includeContentIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.CompositeItemInfo, error) {
	formatName := fileFormatName(filePath)
	provider, providerErr := format.GetTableProvider(format.FormatType(formatName))
	if providerErr != nil {
		return nil, providerErr
	}

	rc, err := contentReader.OpenContent(ctx, connInfo, resolveTableFileCatalogPath(engineID, filePath, firstCatalogPathResolver(catalogPathFor)), plugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read table file %s: %w", filePath, err)
	}
	defer rc.Close()

	tableInfo, err := provider.DescribeTable(ctx, rc, nil)
	if err != nil {
		return nil, err
	}

	fieldsData := tableFileFieldAttributes(tableInfo.Fields)
	return &metaitem.CompositeItemInfo{
		Fields:       tableInfo.Fields,
		Organization: dataitem.OrganizationSingle,
		DataType:     dataitem.DataTypeTable,
		Format:       formatName,
		EntryPath:    filePath,
		RefFiles:     []string{filePath},
		SizeBytes:    &fileSize,
		Attributes:   tableFileAttributes(formatName, "single", fieldsData, []plugin.FileEntry{{Path: filePath, Size: fileSize}}, filePath, fileSize, tableInfo, includeContentIndex && format.SupportsContentIndex(format.FormatType(formatName))),
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
	includeContentIndex bool,
	catalogPathFor ...func(path string) plugin.CatalogPath,
) (*metaitem.DetectedItem, bool, error) {
	if item == nil || item.Organization != dataitem.OrganizationSingle || !hasTableProvider(item.Format) {
		return item, false, nil
	}
	extract := ExtractTableFileSingleFileInfo
	if item.DataType != dataitem.DataTypeTable {
		extract = ExtractTableFileSingleFileInfoStrict
	}
	info, err := extract(ctx, contentReader, connInfo, engineID, filePath, fileSize, includeContentIndex, firstCatalogPathResolver(catalogPathFor))
	if err != nil || info == nil {
		return item, false, err
	}
	return metaitem.DetectedItemFromCompositeInfo(info, filePath, fileSize), true, nil
}

// detectFormat 根据文件列表检测主要格式
func detectFormat(files []plugin.FileEntry) string {
	counts := map[string]int{}
	for _, f := range files {
		formatName := fileFormatName(f.Name)
		if formatName == "" || formatName == string(format.FormatUnknown) {
			continue
		}
		counts[formatName]++
	}
	// 返回数量最多的格式
	best := preferredTableFileFormat(tableFileFormatsByExtension())
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
	formatType := format.DetectFormat(fileName, nil)
	if formatType == format.FormatUnknown {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(fileName)), ".")
		if ext != "" {
			return ext
		}
	}
	return string(formatType)
}

func hasTableProvider(formatName string) bool {
	if strings.TrimSpace(formatName) == "" {
		return false
	}
	_, err := format.GetTableProvider(format.FormatType(strings.ToLower(strings.TrimSpace(formatName))))
	return err == nil
}

func describeTableFile(ctx context.Context, formatName string, rc io.Reader) (*format.TableInfo, error) {
	provider, err := format.GetTableProvider(format.FormatType(formatName))
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

func resolveTableFileCatalogPath(engineID uint, path string, catalogPathFor func(path string) plugin.CatalogPath) plugin.CatalogPath {
	if catalogPathFor != nil {
		return catalogPathFor(path)
	}
	return plugin.FileItemPath(engineID, path)
}

func firstCatalogPathResolver(resolvers []func(path string) plugin.CatalogPath) func(path string) plugin.CatalogPath {
	if len(resolvers) == 0 {
		return nil
	}
	return resolvers[0]
}
