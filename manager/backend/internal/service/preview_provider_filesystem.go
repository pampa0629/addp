package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// fileSystemPreviewProvider 文件系统类存储引擎预览插件（NFS/对象存储等）
// 使用 CatalogProvider / ItemMetadataProvider / ContentReadableProvider 读取，不依赖具体客户端。
type fileSystemPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	content      *ObjectContentRegistry
}

func NewFileSystemPreviewProvider(metadataRepo *repository.MetadataRepository, content *ObjectContentRegistry) PreviewProvider {
	return &fileSystemPreviewProvider{
		metadataRepo: metadataRepo,
		content:      content,
	}
}

func (p *fileSystemPreviewProvider) Name() string { return "builtin:filesystem" }

func (p *fileSystemPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	engine := req.Engine
	// schema = locator path[0]，table = locator path[1:] 的 join
	// NFS 物理路径 = "/" + schema + "/" + table（schema 为空时返回 "/"）
	rootName := req.Schema
	filePath := req.Table

	fullPath := nfsPhysicalPath(rootName, filePath)

	pl, err := plugin.Get(engine.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", engine.EngineType)
	}
	catalogProvider, _ := pl.(plugin.CatalogProvider)
	metadataProvider, _ := pl.(plugin.ItemMetadataProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	if catalogProvider == nil {
		return nil, fmt.Errorf("engine %s does not implement CatalogProvider", engine.EngineType)
	}

	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)

	displayPath := fullPath
	if displayPath == "" {
		displayPath = rootName
	}

	preview := &models.TablePreview{
		Mode:     PreviewModeObject,
		Page:     1,
		PageSize: 1,
		Columns:  []string{},
		Rows:     []map[string]interface{}{},
		Object: &models.ObjectPreview{
			Bucket:   rootName,
			Path:     displayPath,
			NodeType: "object",
			EngineID: engine.ID,
		},
		GeometryColumns: []string{},
	}

	// 目录预览：路径以 / 结尾，或 NodeType 表明是目录类节点。
	// 根目录下的文件会被转换成 schema="" + table="file"，fullPath 为
	// "/file" 且 filePath 为空，不能因此误判为根目录。
	isDirNode := req.NodeType == "prefix" || req.NodeType == "directory" || req.NodeType == "bucket" || req.NodeType == "dir" || req.NodeType == "root"
	if isDirectoryPath(fullPath) || isDirNode {
		return p.previewDirectory(ctx, catalogProvider, connInfo, engine, rootName, fullPath, preview)
	}

	// 文件预览
	return p.previewFile(ctx, metadataProvider, contentReader, connInfo, engine, rootName, fullPath, preview)
}

func (p *fileSystemPreviewProvider) previewDirectory(
	ctx context.Context,
	catalogProvider plugin.CatalogProvider,
	connInfo plugin.ConnectionInfo,
	engine *commonModels.Engine,
	rootName, dirPath string,
	preview *models.TablePreview,
) (*models.TablePreview, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	preview.Mode = PreviewModeNode
	preview.Object.NodeType = "directory"
	preview.Object.ContentType = "application/x-directory"

	children, err := listFileSystemPreviewChildren(ctxTimeout, catalogProvider, connInfo, engine, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory via catalog: %w", err)
	}
	preview.Object.Children = children
	preview.Object.ObjectCount = int64(len(children))
	return preview, nil
}

func (p *fileSystemPreviewProvider) previewFile(
	ctx context.Context,
	metadataProvider plugin.ItemMetadataProvider,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engine *commonModels.Engine,
	rootName, filePath string,
	preview *models.TablePreview,
) (*models.TablePreview, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	meta, err := getFileSystemPreviewMetadata(ctxTimeout, metadataProvider, connInfo, engine, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	preview.Object.SizeBytes = meta.Size
	preview.Object.ObjectKey = filePath
	if !meta.ModifiedAt.IsZero() {
		mod := meta.ModifiedAt
		preview.Object.LastModified = &mod
	}

	rawContentType := meta.ContentType
	canonicalContentType := inferContentType(filePath, rawContentType)
	preview.Object.ContentType = canonicalContentType

	if p.content != nil {
		dir, name := splitFSPath(filePath)
		contentReq := &ObjectContentRequest{
			Bucket:      rootName,
			Path:        dir,
			Name:        name,
			Format:      stringAttribute(preview.Object.Attributes, "format"),
			Extension:   defaultExtension(filePath),
			ContentType: canonicalContentType,
			Size:        meta.Size,
		}
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if compositeHandler, ok := handler.(CompositeStreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return openFileSystemContent(ctxTimeout, contentReader, connInfo, engine.ID, filePath)
				}
				siblingProvider := func(path string) (io.ReadCloser, error) {
					return readFileSystemSibling(ctxTimeout, contentReader, connInfo, engine.ID, path)
				}
				content, truncated, err := compositeHandler.HandleCompositeStream(ctx, contentReq, streamer, siblingProvider)
				if err != nil {
					return nil, err
				}
				if content != nil {
					preview.Object.Content = content
					if truncated || content.Truncated {
						preview.Object.Truncated = true
						preview.Object.Content.Truncated = true
					}
				}
			} else if streamHandler, ok := handler.(StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return openFileSystemContent(ctxTimeout, contentReader, connInfo, engine.ID, filePath)
				}
				content, truncated, err := streamHandler.HandleStream(ctx, contentReq, streamer)
				if err != nil {
					return nil, err
				}
				if content != nil {
					preview.Object.Content = content
					if truncated || content.Truncated {
						preview.Object.Truncated = true
						preview.Object.Content.Truncated = true
					}
				}
			} else {
				fetcher := func(limit int64) ([]byte, bool, error) {
					if limit <= 0 {
						limit = maxTextPreviewBytes
					}
					rc, err := openFileSystemContent(ctxTimeout, contentReader, connInfo, engine.ID, filePath)
					if err != nil {
						return nil, false, err
					}
					defer rc.Close()
					return readObjectWithLimit(rc, limit)
				}
				content, truncated, err := handler.Handle(ctx, contentReq, fetcher)
				if err != nil {
					return nil, err
				}
				if content != nil {
					preview.Object.Content = content
					if truncated || content.Truncated {
						preview.Object.Truncated = true
						preview.Object.Content.Truncated = true
					}
				}
			}
		}
	}

	applyShapefileTablePreview(preview)

	return preview, nil
}

func openFileSystemContent(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, path string) (io.ReadCloser, error) {
	if contentReader != nil {
		return contentReader.OpenContent(ctx, connInfo, fileSystemCatalogPath(engineID, path), plugin.ReadOptions{})
	}
	return nil, fs.ErrNotExist
}

func readFileSystemSibling(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, path string) (io.ReadCloser, error) {
	if contentReader == nil {
		return nil, fs.ErrNotExist
	}
	var lastErr error
	for _, candidate := range candidateSiblingPathVariants(path) {
		reader, err := openFileSystemContent(ctx, contentReader, connInfo, engineID, candidate)
		if err == nil {
			return reader, nil
		}
		if isFileSystemNotFoundErr(err) {
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		lastErr = err
	}
	if lastErr == nil || isFileSystemNotFoundErr(lastErr) {
		return nil, fs.ErrNotExist
	}
	return nil, lastErr
}

func fileSystemCatalogPath(engineID uint, path string) plugin.CatalogPath {
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

func fileSystemDirectoryCatalogPath(engineID uint, path string) plugin.CatalogPath {
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

func listFileSystemPreviewChildren(ctx context.Context, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engine *commonModels.Engine, dirPath string) ([]models.ObjectPreviewChild, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, fileSystemDirectoryCatalogPath(engine.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	children := make([]models.ObjectPreviewChild, 0, len(nodes))
	for _, node := range nodes {
		childType := "object"
		contentType := stringAttribute(node.Attributes, "content_type")
		if node.IsContainer {
			childType = "prefix"
			contentType = "application/x-directory"
		}
		children = append(children, models.ObjectPreviewChild{
			Name:        node.Name,
			Path:        catalogNodePhysicalPath(node),
			Type:        childType,
			SizeBytes:   int64Stat(node.Stats, "size_bytes"),
			ContentType: contentType,
		})
	}
	return children, nil
}

func getFileSystemPreviewMetadata(ctx context.Context, metadataProvider plugin.ItemMetadataProvider, connInfo plugin.ConnectionInfo, engine *commonModels.Engine, path string) (*plugin.FileMetadata, error) {
	if metadataProvider == nil {
		return nil, fs.ErrNotExist
	}
	item, err := metadataProvider.DescribeItem(ctx, connInfo, fileSystemCatalogPath(engine.ID, path), plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return itemMetadataToFileMetadata(item, path), nil
}

func itemMetadataToFileMetadata(item *plugin.ItemMetadata, fallbackPath string) *plugin.FileMetadata {
	if item == nil {
		return &plugin.FileMetadata{Name: pathBase(fallbackPath), Path: fallbackPath}
	}
	name := stringAttribute(item.Attributes, "name")
	if name == "" {
		name = pathBase(fallbackPath)
	}
	path := stringAttribute(item.Attributes, "path")
	if path == "" {
		path = fallbackPath
	}
	updatedAt := time.Time{}
	if item.UpdatedAt != nil {
		updatedAt = *item.UpdatedAt
	}
	return &plugin.FileMetadata{
		Name:        name,
		Path:        path,
		Size:        int64Stat(item.Stats, "size_bytes"),
		ModifiedAt:  updatedAt,
		ContentType: stringAttribute(item.Attributes, "content_type"),
		ETag:        stringAttribute(item.Attributes, "etag"),
	}
}

func catalogNodePhysicalPath(node plugin.CatalogNode) string {
	if path := stringAttribute(node.Attributes, "path"); path != "" {
		return path
	}
	return node.Path.StringPath()
}

func mapAttribute(attrs map[string]interface{}, key string) map[string]interface{} {
	if attrs == nil {
		return nil
	}
	for _, section := range attributeSectionsForKey(key) {
		if sectionAttrs := sectionMapAttribute(attrs, section, key); len(sectionAttrs) > 0 {
			return sectionAttrs
		}
	}
	if value, ok := attrs[key].(map[string]interface{}); ok {
		return value
	}
	return nil
}

func stringSliceAttribute(attrs map[string]interface{}, key string) []string {
	if attrs == nil {
		return nil
	}
	for _, section := range attributeSectionsForKey(key) {
		if values := sectionStringSliceAttribute(attrs, section, key); len(values) > 0 {
			return values
		}
	}
	return interfaceToStringSlice(attrs[key])
}

func int64Attribute(attrs map[string]interface{}, key string) int64 {
	if attrs == nil {
		return 0
	}
	for _, section := range attributeSectionsForKey(key) {
		if value := sectionInt64Attribute(attrs, section, key); value != 0 {
			return value
		}
	}
	return interfaceToInt64(attrs[key])
}

func stringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	for _, section := range attributeSectionsForKey(key) {
		if value := sectionStringAttribute(attrs, section, key); value != "" {
			return value
		}
	}
	if value, ok := attrs[key].(string); ok {
		return value
	}
	return ""
}

func attributeSectionsForKey(key string) []string {
	switch key {
	case "composition_type", "data_family", "format", "entry_path", "component_files", "file_count", "mode":
		return []string{"item"}
	case "bucket", "path", "name", "physical_path", "size_bytes", "size", "total_size", "content_type", "last_modified_at", "etag":
		return []string{"storage"}
	case "fields", "primary_key", "indexes", "row_count", "document_count":
		return []string{"schema"}
	case "spatial_metadata":
		return []string{"extensions.spatial"}
	default:
		return nil
	}
}

func sectionStringAttribute(attrs map[string]interface{}, section, key string) string {
	raw, ok := attrs[section]
	if !ok {
		return ""
	}
	sectionAttrs, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	if value, ok := sectionAttrs[key].(string); ok {
		return value
	}
	return ""
}

func sectionMapAttribute(attrs map[string]interface{}, section, key string) map[string]interface{} {
	if sectionAttrs := sectionAttributes(attrs, section); len(sectionAttrs) > 0 {
		if value, ok := sectionAttrs[key].(map[string]interface{}); ok {
			return value
		}
	}
	return nil
}

func sectionStringSliceAttribute(attrs map[string]interface{}, section, key string) []string {
	if sectionAttrs := sectionAttributes(attrs, section); len(sectionAttrs) > 0 {
		return interfaceToStringSlice(sectionAttrs[key])
	}
	return nil
}

func sectionInt64Attribute(attrs map[string]interface{}, section, key string) int64 {
	if sectionAttrs := sectionAttributes(attrs, section); len(sectionAttrs) > 0 {
		return interfaceToInt64(sectionAttrs[key])
	}
	return 0
}

func sectionAttributes(attrs map[string]interface{}, section string) map[string]interface{} {
	current := attrs
	for _, part := range strings.Split(section, ".") {
		raw, ok := current[part]
		if !ok {
			return nil
		}
		next, ok := raw.(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func interfaceToStringSlice(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func interfaceToInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	default:
		return 0
	}
}

func int64Stat(stats map[string]interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	switch value := stats[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	default:
		return 0
	}
}

func pathBase(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return ""
	}
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}

// nfsPhysicalPath 将 locator 的 schema/table 转换为 NFS 绝对路径
// schema = locator path[0]，table = locator path[1:] 的 join
// 转换规则：NFS物理路径 = "/" + schema + "/" + table
func nfsPhysicalPath(schema, table string) string {
	if schema == "" && table == "" {
		return "/"
	}
	if schema == "" {
		// 根目录下的文件：table 就是文件名
		return "/" + table
	}
	if table == "" {
		return "/" + schema
	}
	return "/" + schema + "/" + table
}

// buildFSPath 保留供外部调用兼容，内部已改用 nfsPhysicalPath
func buildFSPath(rootName, filePath string) string {
	return nfsPhysicalPath(rootName, filePath)
}

// isDirectoryPath 判断路径是否为目录（以 / 结尾或为空）
func isDirectoryPath(path string) bool {
	return path == "" || strings.HasSuffix(path, "/")
}

// splitFSPath 将文件路径拆分为目录和文件名
func splitFSPath(path string) (dir, name string) {
	path = strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "/", path
	}
	return path[:idx+1], path[idx+1:]
}

func candidateSiblingPathVariants(path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	withoutLeading := strings.TrimLeft(trimmed, "/")
	withLeading := "/" + withoutLeading
	candidates := []string{trimmed, withoutLeading, withLeading}

	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func isFileSystemNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not exist") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such file") ||
		strings.Contains(msg, "no such object")
}
