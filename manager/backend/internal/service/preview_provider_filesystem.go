package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// fileSystemPreviewProvider 文件系统类存储引擎预览插件（NFS 等）
// 使用 FileSystemPlugin 接口读取文件，不依赖 MinIO 客户端
type fileSystemPreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	content      *ObjectContentRegistry
	priority     int
}

func NewFileSystemPreviewProvider(metadataRepo *repository.MetadataRepository, content *ObjectContentRegistry) PreviewProvider {
	return &fileSystemPreviewProvider{
		metadataRepo: metadataRepo,
		content:      content,
		priority:     93, // 高于 object-storage(95) 低一点，但高于数据库类
	}
}

func (p *fileSystemPreviewProvider) Name() string     { return "builtin:filesystem" }
func (p *fileSystemPreviewProvider) Priority() int    { return p.priority }

func (p *fileSystemPreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}
	// 支持所有实现了 FileSystemPlugin 的引擎（nfs 等）
	pl, err := plugin.Get(req.Engine.EngineType)
	if err != nil {
		return false
	}
	_, ok := pl.(plugin.FileSystemPlugin)
	return ok
}

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
	fsPlugin, ok := pl.(plugin.FileSystemPlugin)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement FileSystemPlugin", engine.EngineType)
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

	// 目录预览：路径以 / 结尾，或为空，或 NodeType 表明是目录类节点
	isDirNode := req.NodeType == "prefix" || req.NodeType == "directory" || req.NodeType == "bucket" || req.NodeType == "dir" || req.NodeType == "root"
	if isDirectoryPath(filePath) || filePath == "" || isDirNode {
		return p.previewDirectory(ctx, fsPlugin, connInfo, engine, rootName, fullPath, preview)
	}

	// 文件预览
	return p.previewFile(ctx, fsPlugin, connInfo, engine, rootName, fullPath, preview)
}

func (p *fileSystemPreviewProvider) previewDirectory(
	ctx context.Context,
	fsPlugin plugin.FileSystemPlugin,
	connInfo plugin.ConnectionInfo,
	engine *commonModels.Engine,
	rootName, dirPath string,
	preview *models.TablePreview,
) (*models.TablePreview, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	files, subdirs, err := fsPlugin.ListDirectory(ctxTimeout, connInfo, dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	preview.Mode = PreviewModeNode
	preview.Object.NodeType = "directory"
	preview.Object.ContentType = "application/x-directory"

	var children []models.ObjectPreviewChild
	for _, d := range subdirs {
		children = append(children, models.ObjectPreviewChild{
			Name:        d.Name,
			Path:        d.Path,
			Type:        "prefix",
			ContentType: "application/x-directory",
		})
	}
	for _, f := range files {
		children = append(children, models.ObjectPreviewChild{
			Name:        f.Name,
			Path:        f.Path,
			Type:        "object",
			SizeBytes:   f.Size,
			ContentType: f.ContentType,
		})
	}
	preview.Object.Children = children
	preview.Object.ObjectCount = int64(len(children))
	return preview, nil
}

func (p *fileSystemPreviewProvider) previewFile(
	ctx context.Context,
	fsPlugin plugin.FileSystemPlugin,
	connInfo plugin.ConnectionInfo,
	engine *commonModels.Engine,
	rootName, filePath string,
	preview *models.TablePreview,
) (*models.TablePreview, error) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	meta, err := fsPlugin.GetFileMetadata(ctxTimeout, connInfo, filePath)
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
			Extension:   defaultExtension(filePath),
			ContentType: canonicalContentType,
			Size:        meta.Size,
		}
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if streamHandler, ok := handler.(StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return fsPlugin.ReadFile(ctxTimeout, connInfo, filePath)
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
					rc, err := fsPlugin.ReadFile(ctxTimeout, connInfo, filePath)
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
