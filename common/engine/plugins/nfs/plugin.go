package nfs

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
)

// NFSPlugin NFS 文件系统存储引擎插件
type NFSPlugin struct{}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *limitedReadCloser) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}

type lockedReadCloser struct {
	io.ReadCloser
	mu *sync.Mutex
}

func (r *lockedReadCloser) Read(p []byte) (int, error) {
	if r == nil || r.ReadCloser == nil {
		return 0, io.ErrClosedPipe
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ReadCloser.Read(p)
}

func (r *lockedReadCloser) Close() error {
	if r == nil || r.ReadCloser == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ReadCloser.Close()
}

func (r *lockedReadCloser) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := r.ReadCloser.(io.Seeker)
	if !ok {
		return 0, fmt.Errorf("NFS file reader does not support seek")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return seeker.Seek(offset, whence)
}

type lockedWriteCloser struct {
	io.WriteCloser
	mu *sync.Mutex
}

func (w *lockedWriteCloser) Write(p []byte) (int, error) {
	if w == nil || w.WriteCloser == nil {
		return 0, io.ErrClosedPipe
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WriteCloser.Write(p)
}

func (w *lockedWriteCloser) Close() error {
	if w == nil || w.WriteCloser == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.WriteCloser.Close()
}

func init() {
	plugin.Register(&NFSPlugin{})
}

func (p *NFSPlugin) Type() string         { return "nfs" }
func (p *NFSPlugin) DisplayName() string  { return "NFS 文件系统" }
func (p *NFSPlugin) EngineOrigin() string { return "general" }
func (p *NFSPlugin) DefaultPort() int     { return 2049 }

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
		Semantics:    capabilities.Storage.Semantics,
		NotSupported: capabilities.Storage.NotSupported,
	}
}

func (p *NFSPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.FileCatalogModel()
}

func (p *NFSPlugin) fileCatalogCallbacks() plugin.FileCatalogCallbacks {
	return plugin.FileCatalogCallbacks{
		ListRootsFunc:       p.listRoots,
		ListDirectoryFunc:   p.listDirectory,
		GetFileMetadataFunc: p.getFileMetadata,
	}
}

func (p *NFSPlugin) ListChildren(ctx context.Context, connInfo plugin.ConnectionInfo, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogNode, error) {
	return plugin.ListFileCatalogChildren(ctx, p.fileCatalogCallbacks(), connInfo, parent.EngineID, parent, opts)
}

func (p *NFSPlugin) ResolvePath(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath) (*plugin.CatalogNode, error) {
	return plugin.ResolveFileCatalogPath(ctx, p.fileCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *NFSPlugin) DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error) {
	return plugin.DescribeFileItem(ctx, p.fileCatalogCallbacks(), connInfo, path.EngineID, path)
}

func (p *NFSPlugin) OpenContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	return p.readFile(ctx, connInfo, path.StringPath())
}

func (p *NFSPlugin) OpenRange(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if opts.Offset < 0 {
		return nil, fmt.Errorf("range read offset cannot be negative")
	}
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	return p.readFileRange(ctx, connInfo, path.StringPath(), opts)
}

func (p *NFSPlugin) CreateContent(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.WriteOptions) (io.WriteCloser, error) {
	return p.openFileForWrite(ctx, connInfo, path.StringPath(), opts.Overwrite)
}

func (p *NFSPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// TestConnection 通过 listRoots 验证连通性
func (p *NFSPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	_, err := p.listRoots(ctx, connInfo)
	return err
}

// === 文件系统底层 helper ===

// listRoots 返回 NFS 唯一根节点，Name 为空（挂载点透明，不暴露 export_path）
func (p *NFSPlugin) listRoots(ctx context.Context, connInfo plugin.ConnectionInfo) ([]plugin.RootEntry, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	_, err = getOrCreateMount(server, exportPath)
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

// listDirectory 列出目录内容（非递归）
func (p *NFSPlugin) listDirectory(ctx context.Context, connInfo plugin.ConnectionInfo, path string) ([]plugin.FileEntry, []plugin.DirEntry, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	// 规范化路径
	dirPath := normalizePath(path)

	entry.mu.Lock()
	entries, err := entry.target.ReadDirPlus(dirPath)
	entry.mu.Unlock()
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

// readFile 流式读取文件内容
func (p *NFSPlugin) readFile(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.ReadCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	rc, err := entry.target.Open(filePath)
	entry.mu.Unlock()
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file %s: %w", filePath, err)
	}
	return &lockedReadCloser{ReadCloser: rc, mu: &entry.mu}, nil
}

func (p *NFSPlugin) readFileRange(ctx context.Context, connInfo plugin.ConnectionInfo, path string, opts plugin.ReadOptions) (io.ReadCloser, error) {
	rc, err := p.readFile(ctx, connInfo, path)
	if err != nil {
		return nil, err
	}
	seeker, ok := rc.(io.Seeker)
	if !ok {
		_ = rc.Close()
		return nil, fmt.Errorf("NFS file reader does not support seek")
	}
	if _, err := seeker.Seek(opts.Offset, io.SeekStart); err != nil {
		_ = rc.Close()
		return nil, fmt.Errorf("failed to seek NFS file %s to offset %d: %w", normalizePath(path), opts.Offset, err)
	}
	return &limitedReadCloser{
		Reader: io.LimitReader(rc, opts.Length),
		closer: rc,
	}, nil
}

// getFileMetadata 获取文件元数据
func (p *NFSPlugin) getFileMetadata(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (*plugin.FileMetadata, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	info, _, err := entry.target.Lookup(filePath)
	entry.mu.Unlock()
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

// === 写入方法（NFS 插件专有）===

// OpenFileForWrite 打开 NFS 文件用于写入（文件不存在则创建，存在则覆盖）
func (p *NFSPlugin) OpenFileForWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path string) (io.WriteCloser, error) {
	return p.openFileForWrite(ctx, connInfo, path, true)
}

func (p *NFSPlugin) openFileForWrite(ctx context.Context, connInfo plugin.ConnectionInfo, path string, overwrite bool) (io.WriteCloser, error) {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return nil, err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("NFS connection failed: %w", err)
	}

	filePath := normalizePath(path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if overwrite {
		// go-nfs-client 的 OpenFile 不会自动截断，覆盖写必须先删除旧文件。
		_ = entry.target.Remove(filePath)
	} else if _, _, err := entry.target.Lookup(filePath); err == nil {
		return nil, fmt.Errorf("NFS file %s already exists", filePath)
	}

	f, err := entry.target.OpenFile(filePath, 0644)
	if err != nil {
		invalidatePool(server, exportPath)
		return nil, fmt.Errorf("failed to open NFS file for write %s: %w", filePath, err)
	}
	return &lockedWriteCloser{WriteCloser: f, mu: &entry.mu}, nil
}

// MkdirAll 在 NFS 上递归创建目录
func (p *NFSPlugin) MkdirAll(ctx context.Context, connInfo plugin.ConnectionInfo, path string) error {
	server, exportPath, err := p.parseConnInfo(connInfo)
	if err != nil {
		return err
	}

	entry, err := getOrCreateMount(server, exportPath)
	if err != nil {
		invalidatePool(server, exportPath)
		return fmt.Errorf("NFS connection failed: %w", err)
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	dirPath := normalizePath(path)
	parts := strings.Split(strings.TrimPrefix(dirPath, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = current + "/" + part
		if _, err := entry.target.Mkdir(current, 0755); err != nil {
			// 目录已存在则忽略
			if _, _, lookupErr := entry.target.Lookup(current); lookupErr == nil {
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
