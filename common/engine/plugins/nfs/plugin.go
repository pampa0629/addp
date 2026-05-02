package nfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
)

// NFSPlugin NFS 文件系统存储引擎插件
type NFSPlugin struct{}

func init() {
	plugin.Register(&NFSPlugin{})
}

func (p *NFSPlugin) Type() string           { return "nfs" }
func (p *NFSPlugin) DisplayName() string    { return "NFS 文件系统" }
func (p *NFSPlugin) EngineCategory() string { return "standard" }
func (p *NFSPlugin) DefaultPort() int       { return 2049 }

func (p *NFSPlugin) RequiredFields() []string {
	return []string{"server", "export_path"}
}

func (p *NFSPlugin) SensitiveFields() []string {
	return []string{} // NFS 基于 IP 访问控制，无密钥
}

func (p *NFSPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewFileCapabilities(p.Type())
}

func (p *NFSPlugin) StoreSemantics() plugin.StoreSemantics {
	capabilities := p.Capabilities()
	return plugin.StoreSemantics{
		Families:     capabilities.Storage.Families,
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *NFSPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.FileCatalogModel()
}

func (p *NFSPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListFileSystemCatalogChildren(ctx, p, connInfo, parent.EngineID, parent, plugin.CatalogTermRoot, opts)
}

func (p *NFSPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveFileSystemCatalogPath(ctx, p, connInfo, path.EngineID, path, plugin.CatalogTermRoot)
}

func (p *NFSPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeFileSystemItem(ctx, p, connInfo, path.EngineID, path)
}

func (p *NFSPlugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return p.ReadFile(ctx, connInfo, path.StringPath())
}

func (p *NFSPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *NFSPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal NFS connection info: %w", err)
	}
	return string(bytes), nil
}

// TestConnection 通过 ListRoots 验证连通性
func (p *NFSPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	_, err := p.ListRoots(ctx, connInfo)
	return err
}

// === FileSystemPlugin 接口实现 ===

// ListRoots 返回 NFS 唯一根节点，Name 为空（挂载点透明，不暴露 export_path）
func (p *NFSPlugin) ListRoots(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.RootEntry, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	_, _, err = getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	// NFS 根节点用 "." 表示挂载根（不暴露 export_path）
	return []plugin.RootEntry{{
		Name: ".",
		Path: "/",
	}}, nil
}

// ListDirectory 列出目录内容（非递归）
func (p *NFSPlugin) ListDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, path string) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, nil, err
	}

	_, target, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	// 规范化路径
	dirPath := normalizePath(path)

	entries, err := target.ReadDirPlus(dirPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, nil, fmt.Errorf("failed to list directory %s: %w", dirPath, err)
	}

	var files []plugin.FileEntry
	var dirs []plugin.DirEntry

	for _, entry := range entries {
		name := entry.FileName
		if name == "." || name == ".." {
			continue
		}
		fullPath := joinPath(dirPath, name)

		if entry.IsDir() {
			dirs = append(dirs, plugin.DirEntry{
				Name: name,
				Path: fullPath + "/",
			})
		} else {
			files = append(files, plugin.FileEntry{
				Name:        name,
				Path:        fullPath,
				Size:        entry.Size(),
				ModifiedAt:  entry.ModTime(),
				ContentType: inferContentType(name),
			})
		}
	}

	return files, dirs, nil
}

// ReadFile 流式读取文件内容
func (p *NFSPlugin) ReadFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.ReadCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	_, target, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	rc, err := target.Open(filePath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file %s: %w", filePath, err)
	}
	return rc, nil
}

// GetFileMetadata 获取文件元数据
func (p *NFSPlugin) GetFileMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.FileMetadata, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	_, target, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	info, _, err := target.Lookup(filePath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to stat NFS file %s: %w", filePath, err)
	}

	return &plugin.FileMetadata{
		Name:        filepath.Base(filePath),
		Path:        path,
		Size:        info.Size(),
		ModifiedAt:  info.ModTime(),
		ContentType: inferContentType(filePath),
	}, nil
}

// === 写入方法（不在 FileSystemPlugin 接口中，NFS 插件专有）===

// OpenFileForWrite 打开 NFS 文件用于写入（文件不存在则创建，存在则覆盖）
func (p *NFSPlugin) OpenFileForWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.WriteCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	_, target, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	// 先删除已有文件（模拟 truncate，OpenFile 不会自动截断）
	_ = target.Remove(filePath)

	f, err := target.OpenFile(filePath, 0644)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file for write %s: %w", filePath, err)
	}
	return f, nil
}

// MkdirAll 在 NFS 上递归创建目录
func (p *NFSPlugin) MkdirAll(ctx context.Context, connInfo plugin.ConnectionInfo, path string) error {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return err
	}

	_, target, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return fmt.Errorf("NFS connection failed: %w", err)
	}

	dirPath := normalizePath(path)
	parts := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = current + "/" + part
		if _, err := target.Mkdir(current, 0755); err != nil {
			// 目录已存在则忽略
			if _, _, lookupErr := target.Lookup(current); lookupErr == nil {
				continue
			}
			return fmt.Errorf("failed to create NFS directory %s: %w", current, err)
		}
	}
	return nil
}

// WriteFile 将 reader 内容写入 NFS 文件
func (p *NFSPlugin) WriteFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string, r io.Reader) error {
	f, err := p.OpenFileForWrite(ctx, connInfo, path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// === 辅助方法 ===

func (p *NFSPlugin) parseConnInfo(connInfo plugin.ConnectionInfo) (server, exportPath string, err error) {
	server = plugin.GetString(connInfo, "server")
	exportPath = plugin.GetString(connInfo, "export_path")
	if server == "" || exportPath == "" {
		return "", "", fmt.Errorf("missing required fields: server, export_path")
	}
	// 规范化 localhost
	server = plugin.NormalizeHost(server)
	return server, exportPath, nil
}

// normalizePath 确保路径以 / 开头
func normalizePath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return strings.TrimSuffix(path, "/")
}

// joinPath 拼接路径
func joinPath(dir, name string) string {
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

// inferContentType 根据文件名推断 MIME 类型
func inferContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	customTypes := map[string]string{
		".geojson": "application/geo+json",
		".shp":     "application/x-shapefile",
		".shx":     "application/x-shapefile",
		".dbf":     "application/x-dbf",
		".prj":     "application/x-shapefile-prj",
		".parquet": "application/x-parquet",
		".kml":     "application/vnd.google-earth.kml+xml",
		".gpx":     "application/gpx+xml",
		".tif":     "image/tiff",
		".tiff":    "image/tiff",
	}
	if mimeType, ok := customTypes[ext]; ok {
		return mimeType
	}
	return "application/octet-stream"
}
