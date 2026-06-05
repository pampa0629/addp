package metaenrich

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	"github.com/addp/meta/internal/metaattr"
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

type tableFileContentReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.CatalogPath
	files          []metaitem.StorageFileRef
	subdirs        []metaitem.StorageDirectoryRef
}

func (r tableFileContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r.contentReader == nil {
		return nil, contentio.ErrContentNotFound
	}
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveTableFileCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r tableFileContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	for _, file := range r.files {
		if strings.Trim(file.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Stat{
				Ref:    contentio.NewRef(file.Path, contentio.RoleMain),
				Size:   file.Size,
				Exists: true,
			}, nil
		}
	}
	for _, dir := range r.subdirs {
		if strings.Trim(dir.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Stat{
				Ref:    contentio.NewRef(dir.Path, contentio.RoleScope),
				Exists: true,
			}, nil
		}
	}
	return &contentio.Stat{Ref: ref, Exists: false}, nil
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

func describeTableFileScope(ctx context.Context, formatName string, reader contentio.Reader, dirPath string) (*format.TableDescribeResult, error) {
	provider, err := format.GetScopeTableInfoProvider(format.NormalizeFormat(formatName))
	if err != nil {
		return nil, err
	}
	return provider.DescribeTableScope(ctx, reader, contentio.NewRef(dirPath, contentio.RoleScope), nil)
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
