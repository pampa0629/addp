package preview

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/catalogutil"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
)

// fileCatalogPreviewProvider 文件系统类存储引擎预览插件（NFS/对象存储等）
// 使用 CatalogProvider / CatalogFactsProvider / ContentReadableProvider 读取，不依赖具体客户端。
type fileCatalogPreviewProvider struct {
	metadataRepo   *repository.MetadataRepository
	cadPreviewRepo *repository.CADPreviewRepository
	content        *objectcontent.ObjectContentRegistry
}

func (p *fileCatalogPreviewProvider) SetCADPreviewRepository(repo *repository.CADPreviewRepository) {
	p.cadPreviewRepo = repo
}

func NewFileCatalogPreviewProvider(metadataRepo *repository.MetadataRepository, content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &fileCatalogPreviewProvider{
		metadataRepo: metadataRepo,
		content:      content,
	}
}

func (p *fileCatalogPreviewProvider) Name() string { return "builtin:file-catalog" }

func (p *fileCatalogPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
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
	factsProvider, _ := pl.(plugin.CatalogFactsProvider)
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
			Bucket:     rootName,
			Path:       displayPath,
			NodeType:   "object",
			EngineID:   engine.ID,
			Attributes: models.JSONMap(req.Attributes),
		},
		GeometryColumns: []string{},
	}

	// 目录预览：路径以 / 结尾，或 NodeType 表明是目录类节点。
	// 根目录下的文件会被转换成 schema="" + table="file"，fullPath 为
	// "/file" 且 filePath 为空，不能因此误判为根目录。
	isDirNode := req.NodeType == "prefix" || req.NodeType == "directory" || req.NodeType == "bucket" || req.NodeType == "dir" || req.NodeType == "root"
	if isDirectoryPath(fullPath) || isDirNode {
		if applyS3MScenePreview(req.Attributes, preview.Object, engine.ID, rootName, fullPath) {
			return preview, nil
		}
		if applyOSGBScenePreviewPrompt(req.Attributes, preview.Object) {
			return preview, nil
		}
		return p.previewDirectory(ctx, catalogProvider, connInfo, engine, rootName, fullPath, preview)
	}
	if strings.TrimSpace(req.ScopePath) != "" && applyOSGBScenePreviewPrompt(req.Attributes, preview.Object) {
		preview.Object.Path = strings.Trim(req.ScopePath, "/")
		preview.Object.NodeType = "directory"
		preview.Object.ContentType = "application/x-directory"
		return preview, nil
	}

	// 文件预览
	return p.previewFile(ctx, factsProvider, contentReader, connInfo, engine, rootName, fullPath, preview, req)
}

func (p *fileCatalogPreviewProvider) previewDirectory(
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

	children, err := listFileCatalogPreviewChildren(ctxTimeout, catalogProvider, connInfo, engine, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory via catalog: %w", err)
	}
	preview.Object.Children = children
	preview.Object.ObjectCount = int64(len(children))
	return preview, nil
}

func (p *fileCatalogPreviewProvider) previewFile(
	ctx context.Context,
	factsProvider plugin.CatalogFactsProvider,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engine *commonModels.Engine,
	rootName, filePath string,
	preview *models.TablePreview,
	req *PreviewRequest,
) (*models.TablePreview, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	meta, err := getFileCatalogPreviewStorageFacts(ctxTimeout, factsProvider, connInfo, engine, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	preview.Object.SizeBytes = meta.Size
	preview.Object.StorageRef = filePath
	if !meta.ModifiedAt.IsZero() {
		mod := meta.ModifiedAt
		preview.Object.LastModified = &mod
	}

	rawContentType := meta.ContentType
	canonicalContentType := objectcontent.InferContentType(filePath, rawContentType)
	preview.Object.ContentType = canonicalContentType

	if p.content != nil {
		contentPath := filePath
		contentFormat := normalizeObjectContentRequestFormat(catalogutil.StringAttribute(preview.Object.Attributes, "format"))
		if format.NormalizeFormat(contentFormat) == format.Format3DTiles {
			contentPath = threeDTilesManifestObjectPath(rootName, filePath, preview.Object.Attributes)
			canonicalContentType = "application/vnd.ogc.3dtiles+json"
			preview.Object.ContentType = canonicalContentType
			preview.Object.StorageRef = contentPath
		}
		dir, name := splitFSPath(contentPath)
		contentReq := &objectcontent.ObjectContentRequest{
			Bucket:      rootName,
			Path:        dir,
			Name:        name,
			Format:      contentFormat,
			Extension:   defaultExtension(contentPath),
			ContentType: canonicalContentType,
			Size:        meta.Size,
			Attributes:  preview.Object.Attributes,
		}
		if isCADObjectContentRequest(contentReq) {
			url, err := resolveCADPreviewURL(ctx, p.cadPreviewRepo, req, contentReq)
			if err != nil {
				return nil, err
			}
			contentReq.PreviewURL = url
			preview.Object.URL = url
		} else if url := buildFileStorageStreamURL(engine.ID, contentPath); url != "" {
			contentReq.PreviewURL = url
			preview.Object.URL = url
		}
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if objectcontent.IsContainerFormat(contentReq.Format) {
				if content := containerPreviewContentFromMetaAttributes(preview.Object.Attributes, meta.Size, contentReq.Path, contentReq.Name); content != nil {
					preview.Object.Content = content
					return preview, nil
				}
			}
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return openFileCatalogContent(ctxTimeout, contentReader, connInfo, engine.ID, filePath)
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
					rc, err := openFileCatalogContent(ctxTimeout, contentReader, connInfo, engine.ID, filePath)
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

	return preview, nil
}

func buildFileStorageStreamURL(engineID uint, storageRef string) string {
	storageRef = strings.Trim(storageRef, "/")
	if engineID == 0 || storageRef == "" {
		return ""
	}
	values := url.Values{}
	values.Set("engine_id", strconv.FormatUint(uint64(engineID), 10))
	values.Set("storage_ref", storageRef)
	return "/api/v1/manager/storage-stream?" + values.Encode()
}

func buildStorageAssetURL(engineID uint, storageRef string) string {
	storageRef = strings.Trim(strings.ReplaceAll(storageRef, "\\", "/"), "/")
	if engineID == 0 || storageRef == "" {
		return ""
	}
	parts := strings.Split(storageRef, "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return "/api/v1/manager/storage-assets/" + strconv.FormatUint(uint64(engineID), 10) + "/" + strings.Join(parts, "/")
}

func openFileCatalogContent(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, path string) (io.ReadCloser, error) {
	if contentReader != nil {
		return contentReader.OpenContent(ctx, connInfo, plugin.FileItemPath(engineID, path), plugin.ReadOptions{})
	}
	return nil, fs.ErrNotExist
}

func listFileCatalogPreviewChildren(ctx context.Context, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engine *commonModels.Engine, dirPath string) ([]models.ObjectPreviewChild, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.FileDirectoryPath(engine.ID, dirPath), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	children := make([]models.ObjectPreviewChild, 0, len(nodes))
	for _, node := range nodes {
		childType := "object"
		contentType := catalogEntryContentType(node)
		if node.Role == plugin.CatalogRoleBranch {
			childType = "prefix"
			contentType = "application/x-directory"
		}
		children = append(children, models.ObjectPreviewChild{
			Name:        node.Name,
			Path:        catalogutil.NodePhysicalPath(node),
			Type:        childType,
			SizeBytes:   catalogEntrySizeBytes(node),
			ContentType: contentType,
		})
	}
	return children, nil
}

func getFileCatalogPreviewStorageFacts(ctx context.Context, factsProvider plugin.CatalogFactsProvider, connInfo plugin.ConnectionInfo, engine *commonModels.Engine, path string) (*plugin.StorageObjectFacts, error) {
	if factsProvider == nil {
		return nil, fs.ErrNotExist
	}
	item, err := factsProvider.DescribeCatalogFacts(ctx, connInfo, plugin.FileItemPath(engine.ID, path), plugin.CatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return catalogutil.CatalogFactsToStorageObjectFacts(item, path), nil
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
